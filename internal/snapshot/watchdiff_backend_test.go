package snapshot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWatchDiffCreateRestore(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("watch-diff test requires linux")
	}
	workdir := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	base := filepath.Join(workdir, "a.txt")
	if err := os.WriteFile(base, []byte("origin"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := NewWatchDiffBackend(nil)
	if err := backend.Initialize(workdir, stateDir); err != nil {
		t.Fatal(err)
	}
	info := backend.ScopeInfo()
	t.Logf("step=scope roots=%v watchUsed=%d watchLimit=%d log=%s", info.Roots, info.WatchUsed, info.WatchLimit, info.LogFile)
	if err := backend.Create(workdir, stateDir, "cmd-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(workdir, "child.txt")
	if err := os.WriteFile(extra, []byte("temp"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("step=after_change base=%s extra=%s", base, extra)
	if err := backend.Restore(workdir, stateDir, "cmd-1"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=after_restore content=%s", string(got))
	if string(got) != "origin" {
		t.Fatalf("restore失败 got=%s", string(got))
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Fatal("child.txt 应该被回滚移除")
	}
}

func TestAddProtectedPathWithinRoot(t *testing.T) {
	root := t.TempDir()
	out := map[string]struct{}{}
	addProtectedPath(root, filepath.Join(root, "bin", "rewind-sh"), out)
	addProtectedPath(root, filepath.Join(os.TempDir(), "other"), out)
	if _, ok := out["bin/rewind-sh"]; !ok {
		t.Fatal("expected protected rel path to be recorded")
	}
	if len(out) != 1 {
		t.Fatalf("unexpected protected entries count=%d", len(out))
	}
}
