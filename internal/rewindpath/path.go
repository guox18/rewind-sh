package rewindpath

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func BaseDir() string {
	if v := strings.TrimSpace(os.Getenv("REWIND_HOME")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".rewind"
	}
	return filepath.Join(home, ".rewind-sh")
}

func WorkspaceDir(workdir string) string {
	abs := workdir
	if p, err := filepath.Abs(workdir); err == nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	name := strings.ToLower(filepath.Base(abs))
	name = invalidNameChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "workspace"
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(abs))
	return filepath.Join(BaseDir(), "workspaces", fmt.Sprintf("%s-%016x", name, h.Sum64()))
}

func StateDir(workdir string) string {
	return filepath.Join(WorkspaceDir(workdir), "state")
}

func CheckpointDir(workdir string) string {
	return filepath.Join(WorkspaceDir(workdir), "checkpoints")
}

func SessionDir(workdir, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return WorkspaceDir(workdir)
	}
	sid := invalidNameChars.ReplaceAllString(strings.TrimSpace(sessionID), "-")
	sid = strings.Trim(sid, "-")
	if sid == "" {
		sid = "session"
	}
	return filepath.Join(WorkspaceDir(workdir), "sessions", sid)
}

func SessionStateDir(workdir, sessionID string) string {
	return filepath.Join(SessionDir(workdir, sessionID), "state")
}
