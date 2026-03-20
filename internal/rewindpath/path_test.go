package rewindpath

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionDir(t *testing.T) {
	workdir := t.TempDir()
	session := "1700000000-aabbcc"
	dir := SessionDir(workdir, session)
	t.Logf("step=session_dir path=%s", dir)
	if !strings.Contains(dir, filepath.Join("sessions", session)) {
		t.Fatalf("session path mismatch: %s", dir)
	}
	if SessionDir(workdir, "") != WorkspaceDir(workdir) {
		t.Fatal("empty session should fallback workspace dir")
	}
	pathWithTraversal := SessionDir(workdir, "../../x")
	t.Logf("step=session_sanitize path=%s", pathWithTraversal)
	if !strings.Contains(pathWithTraversal, filepath.Join("sessions", "..-..-x")) {
		t.Fatalf("session sanitize mismatch: %s", pathWithTraversal)
	}
}
