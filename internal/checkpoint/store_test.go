package checkpoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRestoreAndEnvScript(t *testing.T) {
	root := filepath.Join(t.TempDir(), "checkpoints")
	work := t.TempDir()
	target := filepath.Join(work, "demo.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("RW_TEST_ENV", "v1"); err != nil {
		t.Fatal(err)
	}
	store := NewStore(root)
	snap, err := store.Create("s1", []string{target}, []string{"RW_TEST_ENV", "RW_TEST_MISSING"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created snapshot=%s files=%d env=%d", snap.Name, len(snap.Files), len(snap.Env))
	if err = os.WriteFile(target, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Restore("s1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "before" {
		t.Fatalf("restore failed: %s", string(b))
	}
	script, err := store.EnvScript("s1")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("env script:\n%s", script)
	if !strings.Contains(script, "export RW_TEST_ENV='v1'") {
		t.Fatalf("script missing export: %s", script)
	}
	if !strings.Contains(script, "unset RW_TEST_MISSING") {
		t.Fatalf("script missing unset: %s", script)
	}
}
