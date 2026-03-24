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
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=engine created")

	// First command: modify the file
	rec1, res1, err := e.ExecuteCommand("echo modified > app.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=exec1 done id=%d snapshot=%s exit=%d", rec1.ID, rec1.SnapshotID, res1.ExitCode)

	// Verify file changed
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=after_exec1 content=%s", string(data))
	if string(data) == "origin" {
		t.Fatal("file should have been modified")
	}

	// Restore to first snapshot
	recRestored, err := e.Restore(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=restore done id=%d", recRestored.ID)

	// Verify file restored
	data, err = os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=after_restore content=%s", string(data))
	if string(data) != "origin" {
		t.Fatalf("file should be restored to origin, got: %s", string(data))
	}
}
