package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsWithinAnyRoot(t *testing.T) {
	root := filepath.Clean("/tmp/a")
	in := filepath.Clean("/tmp/a/b")
	out := filepath.Clean("/tmp/x")
	if !isWithinAnyRoot(in, []string{root}) {
		t.Fatal("expected in-scope path")
	}
	if isWithinAnyRoot(out, []string{root}) {
		t.Fatal("expected out-of-scope path")
	}
}

func TestIsInteractiveCommand(t *testing.T) {
	if !isInteractiveCommand("vim a.txt") {
		t.Fatal("vim should be interactive")
	}
	if !isInteractiveCommand("python") {
		t.Fatal("python without args should be interactive")
	}
	if isInteractiveCommand("python script.py") {
		t.Fatal("python with script should not be interactive")
	}
	if isInteractiveCommand("ls -la") {
		t.Fatal("ls should not be interactive")
	}
}

func TestFormatPromptPath(t *testing.T) {
	out := formatPromptPath(filepath.Clean("."))
	if strings.TrimSpace(out) == "" {
		t.Fatal("prompt path should not be empty")
	}
}

func TestResolveProtectedSpecKeepsWorkdir(t *testing.T) {
	workdir := t.TempDir()
	protected := filepath.Join(t.TempDir(), "data")
	base, extras, err := resolveProtectedSpec(workdir, nil, []string{protected})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(base) != filepath.Clean(workdir) {
		t.Fatalf("base should keep workdir: %s", base)
	}
	if len(extras) != 1 || filepath.Clean(extras[0]) != filepath.Clean(protected) {
		t.Fatalf("extras mismatch: %v", extras)
	}
}
