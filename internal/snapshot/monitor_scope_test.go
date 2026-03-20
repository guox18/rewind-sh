package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMonitorRootsNoExpand(t *testing.T) {
	workdir := t.TempDir()
	roots, limit, used, err := resolveMonitorRootsWithLimit(workdir, nil, 1000000)
	t.Logf("step=roots roots=%v limit=%d used=%d err=%v", roots, limit, used, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] != filepath.Clean(workdir) {
		t.Fatalf("roots mismatch: %v", roots)
	}
}

func TestResolveMonitorRootsLimitExceeded(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, limit, used, err := resolveMonitorRootsWithLimit(workdir, nil, 0)
	t.Logf("step=no_limit limit=%d used=%d err=%v", limit, used, err)
	_, _, _, err = resolveMonitorRootsWithLimit(workdir, nil, 1)
	t.Logf("step=limit_1 err=%v", err)
	if err == nil {
		t.Fatal("expect limit exceeded error")
	}
}
