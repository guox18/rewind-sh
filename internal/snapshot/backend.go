package snapshot

import "fmt"

type Backend interface {
	Name() string
	Available() (bool, string)
	Create(workdir, stateDir, snapshotID string) error
	Restore(workdir, stateDir, snapshotID string) error
	Delete(stateDir, snapshotID string) error
}

type BackendStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	Selected  bool   `json:"selected"`
}

type DiagnoseResult struct {
	Requested string          `json:"requested"`
	Resolved  string          `json:"resolved"`
	Statuses  []BackendStatus `json:"statuses"`
}

type BackendOptions struct {
	MonitorPaths []string
}

type ScopeInfo struct {
	Roots      []string `json:"roots"`
	WatchLimit int      `json:"watch_limit"`
	WatchUsed  int      `json:"watch_used"`
	LogFile    string   `json:"log_file"`
}

type ScopeProvider interface {
	ScopeInfo() ScopeInfo
}

type SessionInitializer interface {
	Initialize(workdir, stateDir string) error
}

type ScopeExtender interface {
	ExtendRoots(paths []string) (ScopeInfo, error)
}

func ResolveBackend(name string, options BackendOptions) (Backend, error) {
	switch name {
	case "", "auto":
		b := AutoBackend(options)
		if b == nil {
			return nil, fmt.Errorf("没有找到可用的快照后端")
		}
		return b, nil
	case "watch-diff":
		return NewWatchDiffBackend(options.MonitorPaths), nil
	default:
		return nil, fmt.Errorf("未知后端: %s", name)
	}
}

func AutoBackend(options BackendOptions) Backend {
	candidates := backendCandidates(options)
	for _, b := range candidates {
		ok, _ := b.Available()
		if ok {
			return b
		}
	}
	// 如果都不支持，返回 nil，在 Engine 层会报错
	return nil
}

func Diagnose(requested string, options BackendOptions) (DiagnoseResult, error) {
	if requested == "" {
		requested = "auto"
	}
	resolvedBackend, err := ResolveBackend(requested, options)
	if err != nil {
		return DiagnoseResult{}, err
	}
	resolvedName := ""
	if resolvedBackend != nil {
		resolvedName = resolvedBackend.Name()
	}
	statuses := make([]BackendStatus, 0, len(backendCandidates(options)))
	for _, b := range backendCandidates(options) {
		ok, reason := b.Available()
		statuses = append(statuses, BackendStatus{
			Name:      b.Name(),
			Available: ok,
			Reason:    reason,
			Selected:  b.Name() == resolvedName,
		})
	}
	return DiagnoseResult{
		Requested: requested,
		Resolved:  resolvedName,
		Statuses:  statuses,
	}, nil
}

func backendCandidates(options BackendOptions) []Backend {
	return []Backend{
		NewWatchDiffBackend(options.MonitorPaths),
	}
}
