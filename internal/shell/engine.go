package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"rewindsh/internal/rewindpath"
	"rewindsh/internal/runner"
	"rewindsh/internal/snapshot"
)

type Config struct {
	WorkDir      string
	StateDir     string
	LogDir       string
	SessionID    string
	HistorySize  int
	Backend      string
	MonitorPaths []string
	HeadLines    int
	TailLines    int
}

type Engine struct {
	cfg       Config
	backend   snapshot.Backend
	scopeInfo snapshot.ScopeInfo
}

func New(cfg Config) (*Engine, error) {
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}
	absWork, err := filepath.Abs(cfg.WorkDir)
	if err != nil {
		return nil, err
	}
	cfg.WorkDir = absWork
	if cfg.StateDir == "" {
		cfg.StateDir = rewindpath.StateDir(cfg.WorkDir)
	}
	if cfg.LogDir == "" {
		cfg.LogDir = rewindpath.LogsDir(cfg.WorkDir)
	}
	if cfg.HistorySize <= 0 {
		cfg.HistorySize = 100
	}
	if cfg.HeadLines <= 0 {
		cfg.HeadLines = 30
	}
	if cfg.TailLines <= 0 {
		cfg.TailLines = 30
	}
	b, err := snapshot.ResolveBackend(cfg.Backend, snapshot.BackendOptions{
		MonitorPaths: cfg.MonitorPaths,
	})
	if err != nil {
		return nil, err
	}
	ok, reason := b.Available()
	if cfg.Backend != "" && cfg.Backend != "auto" && !ok {
		return nil, fmt.Errorf("后端不可用: %s", reason)
	}
	if !ok {
		return nil, fmt.Errorf("后端不可用: %s", reason)
	}
	if err = os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return nil, err
	}
	if initializer, ok := b.(snapshot.SessionInitializer); ok {
		if err = initializer.Initialize(cfg.WorkDir, cfg.StateDir); err != nil {
			return nil, err
		}
	}
	scope := snapshot.ScopeInfo{}
	if provider, ok := b.(snapshot.ScopeProvider); ok {
		scope = provider.ScopeInfo()
	}
	return &Engine{cfg: cfg, backend: b, scopeInfo: scope}, nil
}

func (e *Engine) BackendName() string {
	return e.backend.Name()
}

func (e *Engine) ScopeInfo() snapshot.ScopeInfo {
	if provider, ok := e.backend.(snapshot.ScopeProvider); ok {
		return provider.ScopeInfo()
	}
	return e.scopeInfo
}

func (e *Engine) ExtendMonitorRoots(paths []string) (snapshot.ScopeInfo, error) {
	ext, ok := e.backend.(snapshot.ScopeExtender)
	if !ok {
		return e.ScopeInfo(), nil
	}
	info, err := ext.ExtendRoots(paths)
	if err != nil {
		return snapshot.ScopeInfo{}, err
	}
	e.scopeInfo = info
	return info, nil
}

func (e *Engine) ExecuteCommand(cmdText string) (Record, runner.RunResult, error) {
	return e.ExecuteCommandIn(cmdText, e.cfg.WorkDir)
}

func (e *Engine) ExecuteCommandIn(cmdText, runDir string) (Record, runner.RunResult, error) {
	if strings.TrimSpace(cmdText) == "" {
		return Record{}, runner.RunResult{}, errors.New("命令不能为空")
	}
	if strings.TrimSpace(runDir) == "" {
		runDir = e.cfg.WorkDir
	}
	absRunDir, err := filepath.Abs(runDir)
	if err != nil {
		return Record{}, runner.RunResult{}, err
	}
	info, err := os.Stat(absRunDir)
	if err != nil {
		return Record{}, runner.RunResult{}, err
	}
	if !info.IsDir() {
		return Record{}, runner.RunResult{}, fmt.Errorf("执行目录不是文件夹: %s", absRunDir)
	}
	histPath := filepath.Join(e.cfg.StateDir, "history.json")
	h, err := loadHistory(histPath, e.cfg.HistorySize)
	if err != nil {
		return Record{}, runner.RunResult{}, err
	}
	id := h.NextID
	sid := snapshotID(id)
	if err = e.backend.Create(e.cfg.WorkDir, e.cfg.StateDir, sid); err != nil {
		return Record{}, runner.RunResult{}, err
	}
	if err = saveEnv(e.cfg.StateDir, sid); err != nil {
		return Record{}, runner.RunResult{}, err
	}
	rec := Record{
		ID:         id,
		Command:    cmdText,
		SnapshotID: sid,
		Backend:    e.backend.Name(),
		StartedAt:  time.Now(),
	}
	res, runErr := runner.Run(runner.RunOptions{
		Command:   cmdText,
		LogDir:    e.cfg.LogDir,
		HeadLines: e.cfg.HeadLines,
		TailLines: e.cfg.TailLines,
		WorkDir:   absRunDir,
	})
	rec.EndedAt = time.Now()
	rec.ExitCode = res.ExitCode
	rec.RootPID = res.RootPID
	rec.ProcessGroupID = res.ProcessGroupID
	h.Items = append(h.Items, rec)
	h.NextID++
	evicted := e.trimHistory(&h)
	if err = saveHistory(histPath, h); err != nil {
		return rec, res, err
	}
	for _, r := range evicted {
		_ = e.backend.Delete(e.cfg.StateDir, r.SnapshotID)
		_ = os.Remove(envPath(e.cfg.StateDir, r.SnapshotID))
	}
	if runErr != nil {
		return rec, res, runErr
	}
	return rec, res, nil
}

func (e *Engine) List() ([]Record, error) {
	h, err := loadHistory(filepath.Join(e.cfg.StateDir, "history.json"), e.cfg.HistorySize)
	if err != nil {
		return nil, err
	}
	return h.Items, nil
}

func (e *Engine) Restore(id int) (Record, error) {
	h, err := loadHistory(filepath.Join(e.cfg.StateDir, "history.json"), e.cfg.HistorySize)
	if err != nil {
		return Record{}, err
	}
	var found *Record
	for i := range h.Items {
		if h.Items[i].ID == id {
			found = &h.Items[i]
			break
		}
	}
	if found == nil {
		return Record{}, errors.New("未找到记录")
	}
	if found.ProcessGroupID > 0 {
		_ = syscall.Kill(-found.ProcessGroupID, syscall.SIGTERM)
	}
	if err = e.backend.Restore(e.cfg.WorkDir, e.cfg.StateDir, found.SnapshotID); err != nil {
		return Record{}, err
	}
	if err = restoreEnv(e.cfg.StateDir, found.SnapshotID); err != nil {
		return Record{}, err
	}
	return *found, nil
}

func (e *Engine) trimHistory(h *History) []Record {
	if h.Max <= 0 {
		h.Max = e.cfg.HistorySize
	}
	if len(h.Items) <= h.Max {
		return nil
	}
	n := len(h.Items) - h.Max
	evicted := make([]Record, n)
	copy(evicted, h.Items[:n])
	h.Items = append([]Record{}, h.Items[n:]...)
	return evicted
}
