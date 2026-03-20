package shell

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteAndRestoreWithSubprocessWrites(t *testing.T) {
	workdir := t.TempDir()
	base := filepath.Join(workdir, "app.txt")
	if err := os.WriteFile(base, []byte("origin"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := New(Config{
		WorkDir:     workdir,
		HistorySize: 5,
		Backend:     "auto",
		HeadLines:   10,
		TailLines:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := "echo parent > app.txt; /bin/sh -lc 'echo child > child.txt'"
	t.Logf("step=execute command=%s", cmd)
	rec, res, err := e.ExecuteCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=record id=%d snapshot=%s exit=%d pgid=%d", rec.ID, rec.SnapshotID, res.ExitCode, res.ProcessGroupID)
	changed, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=after_exec app.txt=%s", string(changed))
	if string(changed) == "origin" {
		t.Fatal("app.txt 应该被命令修改")
	}
	if _, err = os.Stat(filepath.Join(workdir, "child.txt")); err != nil {
		t.Fatal(err)
	}
	t.Log("step=restore start")
	if _, err = e.Restore(rec.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=after_restore app.txt=%s", string(restored))
	if string(restored) != "origin" {
		t.Fatalf("restore失败: got=%s", string(restored))
	}
	if _, err = os.Stat(filepath.Join(workdir, "child.txt")); !os.IsNotExist(err) {
		t.Fatal("child.txt 应该被回滚移除")
	}
}

func TestHistoryRingTrim(t *testing.T) {
	workdir := t.TempDir()
	e, err := New(Config{
		WorkDir:     workdir,
		HistorySize: 2,
		Backend:     "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		t.Logf("step=exec idx=%d", i)
		if _, _, err = e.ExecuteCommand("echo run > run.txt"); err != nil {
			t.Fatal(err)
		}
	}
	items, err := e.List()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=list size=%d firstID=%d", len(items), items[0].ID)
	if len(items) != 2 {
		t.Fatalf("want 2 got %d", len(items))
	}
	if items[0].ID != 2 {
		t.Fatalf("oldest should be id=2 got %d", items[0].ID)
	}
}
