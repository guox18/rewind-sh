package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"rewindsh/internal/stream"
)

type RunOptions struct {
	Command      string
	WorkDir      string
	LogDir       string
	HeadLines    int
	TailLines    int
	Timeout      time.Duration
	SliceSeconds int
}

type RunResult struct {
	ExitCode       int            `json:"exit_code"`
	LogFile        string         `json:"log_file"`
	Summary        stream.Summary `json:"summary"`
	RecentSlice    []stream.Event `json:"recent_slice"`
	RootPID        int            `json:"root_pid"`
	ProcessGroupID int            `json:"process_group_id"`
}

var errInteractivePrompt = errors.New("检测到命令正在等待交互输入，请改用非交互参数")

func Run(opts RunOptions) (RunResult, error) {
	if strings.TrimSpace(opts.Command) == "" {
		return RunResult{}, errors.New("run 命令不能为空")
	}
	if opts.HeadLines < 0 || opts.TailLines < 0 {
		return RunResult{}, errors.New("head/tail 不能为负数")
	}
	if looksInteractiveCommand(opts.Command) {
		return RunResult{}, errors.New("命令需要交互式TTY，当前模式不支持（如 vim/top/less/man）")
	}
	if err := ensureDir(opts.LogDir); err != nil {
		return RunResult{}, err
	}
	logFile := filepath.Join(opts.LogDir, time.Now().Format("20060102_150405")+".jsonl")
	w, err := stream.NewLogWriter(logFile)
	if err != nil {
		return RunResult{}, err
	}
	defer w.Close()

	baseCtx, stop := context.WithCancel(context.Background())
	defer stop()
	ctx := baseCtx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(baseCtx, opts.Timeout)
		defer cancel()
	}

	shellPath, shellArgs := resolveShellInvocation(opts.Command)
	cmd := exec.CommandContext(ctx, shellPath, shellArgs...)
	cmd.Env = append(os.Environ(), nonInteractiveEnv()...)
	if strings.TrimSpace(opts.WorkDir) != "" {
		cmd.Dir = opts.WorkDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	applyDropPrivileges(cmd.SysProcAttr)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return RunResult{}, err
	}
	if err = cmd.Start(); err != nil {
		return RunResult{}, err
	}
	rootPID := cmd.Process.Pid
	pgid := 0
	if g, ge := syscall.Getpgid(rootPID); ge == nil {
		pgid = g
	}

	buf := stream.NewBuffer(opts.HeadLines, opts.TailLines)
	var mu sync.Mutex
	sliceEvents := make([]stream.Event, 0, 256)
	pushEvent := func(e stream.Event) error {
		mu.Lock()
		defer mu.Unlock()
		buf.Add(e)
		if looksPromptForInput(e.Text) {
			stop()
			return errInteractivePrompt
		}
		if opts.SliceSeconds > 0 {
			cutoff := time.Now().Add(-time.Duration(opts.SliceSeconds) * time.Second)
			sliceEvents = append(sliceEvents, e)
			idx := 0
			for idx < len(sliceEvents) && sliceEvents[idx].Time.Before(cutoff) {
				idx++
			}
			if idx > 0 {
				sliceEvents = append([]stream.Event(nil), sliceEvents[idx:]...)
			}
		}
		return w.WriteEvent(e)
	}
	readPipe := func(name string, r *bufio.Scanner) error {
		for r.Scan() {
			e := stream.Event{
				Time:   time.Now(),
				Stream: name,
				Text:   r.Text(),
			}
			if err := pushEvent(e); err != nil {
				return err
			}
		}
		err := r.Err()
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "file already closed") {
			return nil
		}
		return err
	}
	scOut := bufio.NewScanner(stdout)
	scOut.Buffer(make([]byte, 0, 1024), 8*1024*1024)
	scErr := bufio.NewScanner(stderr)
	scErr.Buffer(make([]byte, 0, 1024), 8*1024*1024)

	var wg sync.WaitGroup
	var outErr, errErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		outErr = readPipe("stdout", scOut)
	}()
	go func() {
		defer wg.Done()
		errErr = readPipe("stderr", scErr)
	}()
	wg.Wait()
	waitErr := cmd.Wait()
	if outErr != nil {
		return RunResult{}, outErr
	}
	if errErr != nil {
		return RunResult{}, errErr
	}
	res := RunResult{
		LogFile:        logFile,
		Summary:        buf.Snapshot(),
		RecentSlice:    sliceEvents,
		RootPID:        rootPID,
		ProcessGroupID: pgid,
	}
	if waitErr == nil {
		if errors.Is(outErr, errInteractivePrompt) || errors.Is(errErr, errInteractivePrompt) {
			return res, errInteractivePrompt
		}
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		if errors.Is(outErr, errInteractivePrompt) || errors.Is(errErr, errInteractivePrompt) {
			return res, errInteractivePrompt
		}
		return res, nil
	}
	return RunResult{}, waitErr
}

func resolveShellInvocation(command string) (string, []string) {
	if path := strings.TrimSpace(os.Getenv("REWIND_SHELL")); isExecutableFile(path) {
		return path, shellArgs(path, command)
	}
	if path := strings.TrimSpace(os.Getenv("SHELL")); isExecutableFile(path) && filepath.Base(path) != "sh" {
		return path, shellArgs(path, command)
	}
	if isExecutableFile("/bin/bash") {
		return "/bin/bash", shellArgs("/bin/bash", command)
	}
	if isExecutableFile("/usr/bin/bash") {
		return "/usr/bin/bash", shellArgs("/usr/bin/bash", command)
	}
	if isExecutableFile("/bin/zsh") {
		return "/bin/zsh", shellArgs("/bin/zsh", command)
	}
	if path := strings.TrimSpace(os.Getenv("SHELL")); isExecutableFile(path) {
		return path, shellArgs(path, command)
	}
	return "/bin/sh", shellArgs("/bin/sh", command)
}

func nonInteractiveEnv() []string {
	return []string{
		"CI=1",
		"TERM=dumb",
		"PAGER=cat",
		"GIT_PAGER=cat",
		"MANPAGER=cat",
		"LESS=FRX",
	}
}

func shellArgs(shellPath, command string) []string {
	if filepath.Base(shellPath) == "bash" && wantsLoginShell() {
		return []string{"-lc", command}
	}
	return []string{"-c", command}
}

func wantsLoginShell() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REWIND_SHELL_LOGIN"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isExecutableFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func looksInteractiveCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	name := filepath.Base(fields[0])
	switch name {
	case "vim", "vi", "nvim", "top", "htop", "less", "more", "man", "nano":
		return true
	case "python", "python3", "node", "irb", "lua", "sqlite3", "mysql":
		return len(fields) == 1
	default:
		return false
	}
}

func looksPromptForInput(text string) bool {
	low := strings.ToLower(strings.TrimSpace(text))
	if low == "" {
		return false
	}
	markers := []string{
		"[y/n]", "[y/n]:", "[y/n]?", "[y/n/q]",
		"[y/N]", "(y/n)", "(yes/no)", "yes/no",
		"continue?", "proceed?", "accept?",
		"press enter", "hit enter",
		"enter choice", "select an option",
		"password:", "passphrase:",
	}
	for _, m := range markers {
		if strings.Contains(low, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func applyDropPrivileges(attr *syscall.SysProcAttr) {
	uidStr := os.Getenv("SUDO_UID")
	gidStr := os.Getenv("SUDO_GID")
	if uidStr == "" || gidStr == "" {
		return
	}
	var uid, gid uint32
	if _, err := fmt.Sscanf(uidStr, "%d", &uid); err != nil {
		return
	}
	if _, err := fmt.Sscanf(gidStr, "%d", &gid); err != nil {
		return
	}
	attr.Credential = &syscall.Credential{
		Uid: uid,
		Gid: gid,
	}
}
