package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"rewindsh/internal/checkpoint"
	"rewindsh/internal/process"
	"rewindsh/internal/rewindpath"
	"rewindsh/internal/shell"
	"rewindsh/internal/snapshot"
)

func Execute(args []string) error {
	if len(args) == 0 {
		return shellCommand(nil)
	}
	switch args[0] {
	case "shell":
		return shellCommand(args[1:])
	case "exec":
		return execCommand(args[1:])
	case "rewind-list":
		return rewindListCommand(args[1:])
	case "rewind-restore":
		return rewindRestoreCommand(args[1:])
	case "checkpoint-create":
		return checkpointCreate(args[1:])
	case "checkpoint-restore":
		return checkpointRestore(args[1:])
	case "checkpoint-env":
		return checkpointEnv(args[1:])
	case "checkpoint-list":
		return checkpointList(args[1:])
	case "process-list":
		return processList(args[1:])
	case "process-kill":
		return processKill(args[1:])
	case "backend-check":
		return backendCheckCommand(args[1:])
	case "help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("未知命令: %s", args[0])
	}
}

func shellCommand(args []string) error {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	workdir := fs.String("workdir", ".", "工作目录")
	historySize := fs.Int("history-size", 100, "保留命令状态数")
	backend := fs.String("backend", "auto", "快照后端: auto|watch-diff")
	pathsInput := fs.String("paths", "", "受保护路径，逗号分隔")
	monitorPaths := fs.String("monitor-paths", "", "额外监控路径，多个用逗号分隔（兼容参数）")
	sessionIDInput := fs.String("session-id", "", "复用会话ID；为空则创建新会话")
	if err := fs.Parse(args); err != nil {
		return err
	}

	actualWorkdir, actualMonitorPaths, err := resolveProtectedSpec(*workdir, splitCSV(*monitorPaths), splitCSV(*pathsInput))
	if err != nil {
		return err
	}
	sessionID := strings.TrimSpace(*sessionIDInput)
	if sessionID == "" {
		sessionID = newSessionID()
	}
	cfg := shell.Config{
		WorkDir:      actualWorkdir,
		HistorySize:  *historySize,
		Backend:      *backend,
		MonitorPaths: actualMonitorPaths,
	}
	cfg = applySessionConfig(cfg, actualWorkdir, sessionID)
	e, err := shell.New(cfg)
	if err != nil {
		return err
	}

	// 忽略 SIGINT 信号，防止 Ctrl+C 退出主程序
	// 当执行子命令时，前台进程组会收到 SIGINT，子命令自行中断
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT)
	go func() {
		for range sigCh {
			// Do nothing on Ctrl+C in shell prompt
			// 子进程在前台时也会收到相同的信号，从而被中断
		}
	}()

	fmt.Printf("rewind-sh shell started, backend=%s\n", e.BackendName())
	fmt.Printf("session id: %s\n", sessionID)
	scope := e.ScopeInfo()
	if len(scope.Roots) > 0 {
		fmt.Printf("monitor scope: roots=%s watch-used=%d watch-limit=%d\n", strings.Join(scope.Roots, ","), scope.WatchUsed, scope.WatchLimit)
		if scope.LogFile != "" {
			fmt.Printf("monitor scope log: %s\n", scope.LogFile)
		}
	}
	fmt.Println("Agent 提示: 当前运行在一个受限的可回滚 Shell 环境中。此环境无法通过 exit 退出，且无法切换到其他不受控的 shell，请在当前环境内完成所有操作。")
	fmt.Println("输入命令执行；内建命令: rewind list | rewind restore <id> | rewind scope | cd <dir> | pwd")
	currentDir := currentStartupDir(actualWorkdir)
	reader := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("rewind-sh:%s> ", formatPromptPath(currentDir))
		if !reader.Scan() {
			// 如果由于 EOF 导致退出，仍然只能接受环境关闭
			return nil
		}
		line := strings.TrimSpace(reader.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			fmt.Fprintln(os.Stderr, "权限拒绝: 当前是受限容器，不允许使用 exit 或 quit 退出当前 Shell。请继续执行任务。")
			continue
		}
		if line == "bash" || line == "zsh" || line == "sh" {
			fmt.Fprintln(os.Stderr, "权限拒绝: 为保证操作可追溯，禁止嵌套启动其他交互式 Shell，请直接在当前环境输入具体指令。")
			continue
		}
		if line == "rewind list" {
			items, e2 := e.List()
			if e2 != nil {
				fmt.Fprintln(os.Stderr, e2.Error())
				continue
			}
			for _, item := range items {
				fmt.Printf("%d\t%s\texit=%d\t%s\n", item.ID, item.StartedAt.Format(time.RFC3339), item.ExitCode, item.Command)
			}
			continue
		}
		if line == "rewind scope" {
			scope = e.ScopeInfo()
			fmt.Println(formatScopeOutput(scope))
			continue
		}
		if line == "pwd" {
			fmt.Println(currentDir)
			continue
		}
		if line == "cd" || strings.HasPrefix(line, "cd ") {
			dst, e2 := resolveCDTarget(currentDir, line)
			if e2 != nil {
				fmt.Fprintln(os.Stderr, e2.Error())
				continue
			}
			if !isWithinAnyRoot(dst, scope.Roots) {
				fmt.Fprintln(os.Stderr, "提示: cd 目标超出当前监控范围，正在尝试动态扩展监控范围...")
				updated, e3 := e.ExtendMonitorRoots([]string{dst})
				if e3 != nil {
					fmt.Fprintln(os.Stderr, e3.Error())
					continue
				}
				scope = updated
				fmt.Println(formatScopeOutput(scope))
			}
			currentDir = dst
			continue
		}
		if strings.HasPrefix(line, "rewind restore ") {
			idText := strings.TrimSpace(strings.TrimPrefix(line, "rewind restore "))
			id, e2 := parseRecordID(idText)
			if e2 != nil {
				fmt.Fprintln(os.Stderr, e2.Error())
				continue
			}
			rec, e2 := e.Restore(id)
			if e2 != nil {
				fmt.Fprintln(os.Stderr, e2.Error())
				continue
			}
			fmt.Printf("restored id=%d command=%s\n", rec.ID, rec.Command)
			continue
		}
		if isInteractiveCommand(line) {
			fmt.Fprintln(os.Stderr, "该命令需要交互式TTY（如 vim/top/less/man），当前模式不支持，请改用非交互命令或在外部终端执行。")
			continue
		}
		if !isWithinAnyRoot(currentDir, scope.Roots) {
			fmt.Fprintln(os.Stderr, "提示: 当前执行目录超出监控范围，正在尝试动态扩展监控范围...")
			updated, e3 := e.ExtendMonitorRoots([]string{currentDir})
			if e3 != nil {
				fmt.Fprintln(os.Stderr, e3.Error())
				continue
			}
			scope = updated
			fmt.Println(formatScopeOutput(scope))
		}
		rec, res, e2 := e.ExecuteCommandIn(line, currentDir)
		if e2 != nil {
			fmt.Fprintln(os.Stderr, e2.Error())
		}
		fmt.Printf("record=%d snapshot=%s exit=%d pgid=%d\n", rec.ID, rec.SnapshotID, res.ExitCode, res.ProcessGroupID)
	}
}

func execCommand(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	workdir := fs.String("workdir", ".", "工作目录")
	historySize := fs.Int("history-size", 100, "保留命令状态数")
	backend := fs.String("backend", "auto", "快照后端")
	pathsInput := fs.String("paths", "", "受保护路径，逗号分隔")
	monitorPaths := fs.String("monitor-paths", "", "额外监控路径，多个用逗号分隔（兼容参数）")
	sessionIDInput := fs.String("session-id", "", "复用会话ID")
	if err := fs.Parse(args); err != nil {
		return err
	}

	actualWorkdir, actualMonitorPaths, err := resolveProtectedSpec(*workdir, splitCSV(*monitorPaths), splitCSV(*pathsInput))
	if err != nil {
		return err
	}

	cmdText := strings.Join(fs.Args(), " ")
	cfg := shell.Config{
		WorkDir:      actualWorkdir,
		HistorySize:  *historySize,
		Backend:      *backend,
		MonitorPaths: actualMonitorPaths,
	}
	cfg = applySessionConfig(cfg, actualWorkdir, strings.TrimSpace(*sessionIDInput))
	e, err := shell.New(cfg)
	if err != nil {
		return err
	}
	rec, res, err := e.ExecuteCommand(cmdText)
	fmt.Printf("record=%d snapshot=%s backend=%s exit=%d\n", rec.ID, rec.SnapshotID, rec.Backend, res.ExitCode)
	return err
}

func rewindListCommand(args []string) error {
	fs := flag.NewFlagSet("rewind-list", flag.ContinueOnError)
	workdir := fs.String("workdir", ".", "工作目录")
	historySize := fs.Int("history-size", 100, "保留命令状态数")
	backend := fs.String("backend", "auto", "快照后端")
	pathsInput := fs.String("paths", "", "受保护路径，逗号分隔")
	monitorPaths := fs.String("monitor-paths", "", "额外监控路径，多个用逗号分隔（兼容参数）")
	sessionIDInput := fs.String("session-id", "", "复用会话ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	actualWorkdir, actualMonitorPaths, err := resolveProtectedSpec(*workdir, splitCSV(*monitorPaths), splitCSV(*pathsInput))
	if err != nil {
		return err
	}
	cfg := shell.Config{
		WorkDir:      actualWorkdir,
		HistorySize:  *historySize,
		Backend:      *backend,
		MonitorPaths: actualMonitorPaths,
	}
	cfg = applySessionConfig(cfg, actualWorkdir, strings.TrimSpace(*sessionIDInput))
	e, err := shell.New(cfg)
	if err != nil {
		return err
	}
	items, err := e.List()
	if err != nil {
		return err
	}
	return printJSON(items)
}

func rewindRestoreCommand(args []string) error {
	fs := flag.NewFlagSet("rewind-restore", flag.ContinueOnError)
	workdir := fs.String("workdir", ".", "工作目录")
	historySize := fs.Int("history-size", 100, "保留命令状态数")
	backend := fs.String("backend", "auto", "快照后端")
	pathsInput := fs.String("paths", "", "受保护路径，逗号分隔")
	monitorPaths := fs.String("monitor-paths", "", "额外监控路径，多个用逗号分隔（兼容参数）")
	sessionIDInput := fs.String("session-id", "", "复用会话ID")
	id := fs.Int("id", 0, "记录ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id <= 0 {
		return errors.New("id 必须大于 0")
	}
	actualWorkdir, actualMonitorPaths, err := resolveProtectedSpec(*workdir, splitCSV(*monitorPaths), splitCSV(*pathsInput))
	if err != nil {
		return err
	}
	cfg := shell.Config{
		WorkDir:      actualWorkdir,
		HistorySize:  *historySize,
		Backend:      *backend,
		MonitorPaths: actualMonitorPaths,
	}
	cfg = applySessionConfig(cfg, actualWorkdir, strings.TrimSpace(*sessionIDInput))
	e, err := shell.New(cfg)
	if err != nil {
		return err
	}
	rec, err := e.Restore(*id)
	if err != nil {
		return err
	}
	fmt.Printf("restored id=%d cmd=%s\n", rec.ID, rec.Command)
	return nil
}

func checkpointCreate(args []string) error {
	fs := flag.NewFlagSet("checkpoint-create", flag.ContinueOnError)
	root := fs.String("root", rewindpath.CheckpointDir("."), "checkpoint根目录")
	name := fs.String("name", "", "checkpoint名称")
	files := fs.String("files", "", "逗号分隔文件列表")
	envKeys := fs.String("env", "", "逗号分隔环境变量")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store := checkpoint.NewStore(*root)
	snap, err := store.Create(*name, splitCSV(*files), splitCSV(*envKeys))
	if err != nil {
		return err
	}
	return printJSON(snap)
}

func checkpointRestore(args []string) error {
	fs := flag.NewFlagSet("checkpoint-restore", flag.ContinueOnError)
	root := fs.String("root", rewindpath.CheckpointDir("."), "checkpoint根目录")
	name := fs.String("name", "", "checkpoint名称")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store := checkpoint.NewStore(*root)
	snap, err := store.Restore(*name)
	if err != nil {
		return err
	}
	fmt.Printf("restored=%s files=%d\n", snap.Name, len(snap.Files))
	fmt.Println("环境变量请执行 checkpoint-env 输出脚本进行恢复")
	return nil
}

func checkpointEnv(args []string) error {
	fs := flag.NewFlagSet("checkpoint-env", flag.ContinueOnError)
	root := fs.String("root", rewindpath.CheckpointDir("."), "checkpoint根目录")
	name := fs.String("name", "", "checkpoint名称")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store := checkpoint.NewStore(*root)
	script, err := store.EnvScript(*name)
	if err != nil {
		return err
	}
	fmt.Println(script)
	return nil
}

func checkpointList(args []string) error {
	fs := flag.NewFlagSet("checkpoint-list", flag.ContinueOnError)
	root := fs.String("root", rewindpath.CheckpointDir("."), "checkpoint根目录")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store := checkpoint.NewStore(*root)
	items, err := store.List()
	if err != nil {
		return err
	}
	return printJSON(items)
}

func processList(args []string) error {
	fs := flag.NewFlagSet("process-list", flag.ContinueOnError)
	match := fs.String("match", "", "命令关键字过滤")
	if err := fs.Parse(args); err != nil {
		return err
	}
	items, err := process.List(*match)
	if err != nil {
		if process.IsPermissionDenied(err) {
			fmt.Fprintln(os.Stderr, "warning: 当前环境限制了ps权限，process-list返回空结果")
			return printJSON([]any{})
		}
		return err
	}
	return printJSON(items)
}

func processKill(args []string) error {
	fs := flag.NewFlagSet("process-kill", flag.ContinueOnError)
	pid := fs.Int("pid", 0, "进程PID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pid <= 0 {
		return errors.New("pid 必须大于 0")
	}
	if err := process.Kill(*pid); err != nil {
		return err
	}
	fmt.Printf("killed pid=%d\n", *pid)
	return nil
}

func backendCheckCommand(args []string) error {
	fs := flag.NewFlagSet("backend-check", flag.ContinueOnError)
	backend := fs.String("backend", "auto", "后端名: auto|watch-diff")
	monitorPaths := fs.String("monitor-paths", "", "额外监控路径，多个用逗号分隔")
	asJSON := fs.Bool("json", false, "以JSON输出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	diag, err := snapshot.Diagnose(*backend, snapshot.BackendOptions{
		MonitorPaths: splitCSV(*monitorPaths),
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(diag)
	}
	fmt.Printf("requested=%s resolved=%s\n", diag.Requested, diag.Resolved)
	for _, s := range diag.Statuses {
		chosen := ""
		if s.Selected {
			chosen = " [selected]"
		}
		status := "unavailable"
		if s.Available {
			status = "available"
		}
		if s.Reason == "" {
			fmt.Printf("- %s: %s%s\n", s.Name, status, chosen)
			continue
		}
		fmt.Printf("- %s: %s%s (%s)\n", s.Name, status, chosen, s.Reason)
	}
	return nil
}

func splitCSV(in string) []string {
	items := strings.Split(in, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func newSessionID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("s-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%x", time.Now().Unix(), buf)
}

func applySessionConfig(cfg shell.Config, workdir, sessionID string) shell.Config {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return cfg
	}
	cfg.SessionID = sid
	cfg.StateDir = rewindpath.SessionStateDir(workdir, sid)
	return cfg
}

func currentStartupDir(fallback string) string {
	cwd, err := os.Getwd()
	if err != nil || strings.TrimSpace(cwd) == "" {
		return fallback
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return fallback
	}
	return filepath.Clean(abs)
}

func resolveProtectedSpec(workdir string, monitorPaths []string, protectedPaths []string) (string, []string, error) {
	if len(protectedPaths) == 0 {
		base, err := expandPath(workdir, "")
		if err != nil {
			return "", nil, err
		}
		extras := make([]string, 0, len(monitorPaths))
		for _, item := range monitorPaths {
			p, e := expandPath(item, base)
			if e != nil {
				return "", nil, e
			}
			extras = append(extras, p)
		}
		return base, extras, nil
	}
	roots := make([]string, 0, len(protectedPaths))
	for _, item := range protectedPaths {
		p, err := expandPath(item, "")
		if err != nil {
			return "", nil, err
		}
		roots = append(roots, p)
	}
	base, err := expandPath(workdir, "")
	if err != nil {
		return "", nil, err
	}
	extras := make([]string, 0, len(roots)+len(monitorPaths))
	extras = append(extras, roots...)
	for _, item := range monitorPaths {
		p, e := expandPath(item, base)
		if e != nil {
			return "", nil, e
		}
		extras = append(extras, p)
	}
	return base, extras, nil
}

func expandPath(in string, relativeBase string) (string, error) {
	path := strings.TrimSpace(in)
	if path == "" {
		path = "."
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", errors.New("无法解析HOME目录")
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) && strings.TrimSpace(relativeBase) != "" {
		path = filepath.Join(relativeBase, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func resolveCDTarget(currentDir, line string) (string, error) {
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "cd"))
	if arg == "" || arg == "~" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", errors.New("无法解析HOME目录")
		}
		return home, nil
	}
	if strings.HasPrefix(arg, "~"+string(filepath.Separator)) || strings.HasPrefix(arg, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", errors.New("无法解析HOME目录")
		}
		arg = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(arg, "~/"), "~"+string(filepath.Separator)))
	}
	if !filepath.IsAbs(arg) {
		arg = filepath.Join(currentDir, arg)
	}
	dst := filepath.Clean(arg)
	info, err := os.Stat(dst)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("cd 目标不是目录")
	}
	return dst, nil
}

func isWithinAnyRoot(path string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	target := filepath.Clean(path)
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if target == cleanRoot {
			return true
		}
		prefix := cleanRoot + string(filepath.Separator)
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

func isInteractiveCommand(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
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

func formatPromptPath(path string) string {
	clean := filepath.Clean(path)
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		home = filepath.Clean(home)
		if clean == home {
			return "~"
		}
		prefix := home + string(filepath.Separator)
		if strings.HasPrefix(clean, prefix) {
			return "~" + string(filepath.Separator) + strings.TrimPrefix(clean, prefix)
		}
	}
	return clean
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func formatScopeOutput(scope snapshot.ScopeInfo) string {
	return fmt.Sprintf("roots=%s watch_used=%d watch_limit=%d scope_log=%s", strings.Join(scope.Roots, ","), scope.WatchUsed, scope.WatchLimit, scope.LogFile)
}

func parseRecordID(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("记录ID不能为空")
	}
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, errors.New("记录ID必须是数字")
	}
	if id <= 0 {
		return 0, errors.New("记录ID必须大于0")
	}
	return id, nil
}

func printUsage() {
	exe := filepath.Base(os.Args[0])
	fmt.Println(exe + " commands:")
	fmt.Println("  shell --paths <path1[,path2...]> --history-size 100 --backend auto [--session-id <id>]")
	fmt.Println("  exec --paths <path1[,path2...]> --history-size 100 --backend auto [--session-id <id>] <command...>")
	fmt.Println("  rewind-list --paths <path1[,path2...]> [--session-id <id>]")
	fmt.Println("  rewind-restore --paths <path1[,path2...]> --id <id> [--session-id <id>]")
	fmt.Println("  shell 内建: rewind scope")
	fmt.Println("  checkpoint-create --name <name> --files a.txt,b.txt --env PATH,HOME")
	fmt.Println("  checkpoint-restore --name <name>")
	fmt.Println("  checkpoint-env --name <name>")
	fmt.Println("  checkpoint-list")
	fmt.Println("  process-list [--match keyword]")
	fmt.Println("  process-kill --pid <pid>")
	fmt.Println("  backend-check [--backend auto|watch-diff] [--json]")
	fmt.Println("示例:")
	fmt.Println("  " + exe + " shell")
	fmt.Println("  " + exe + " exec \"echo hello > a.txt\"")
	fmt.Println("  " + exe + " rewind-list")
	fmt.Println("  " + exe + " rewind-restore --id 12")
	fmt.Println("  " + exe + " backend-check --backend auto")
}
