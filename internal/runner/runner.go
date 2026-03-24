package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type RunOptions struct {
	Command string
	WorkDir string
	Timeout time.Duration
}

type RunResult struct {
	ExitCode       int `json:"exit_code"`
	RootPID        int `json:"root_pid"`
	ProcessGroupID int `json:"process_group_id"`
}

func Run(opts RunOptions) (RunResult, error) {
	if strings.TrimSpace(opts.Command) == "" {
		return RunResult{}, errors.New("run 命令不能为空")
	}
	if looksInteractiveCommand(opts.Command) {
		return RunResult{}, errors.New("命令需要交互式TTY，当前模式不支持（如 vim/top/less/man）")
	}

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

	// Pipe directly to console
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return RunResult{}, err
	}
	rootPID := cmd.Process.Pid
	pgid := 0
	if g, ge := syscall.Getpgid(rootPID); ge == nil {
		pgid = g
	}

	waitErr := cmd.Wait()
	res := RunResult{
		RootPID:        rootPID,
		ProcessGroupID: pgid,
	}
	if waitErr == nil {
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
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
