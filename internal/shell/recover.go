package shell

import (
	"os"
	"path/filepath"
	"time"
)

func backupCorruptFile(path string, content []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	name := filepath.Base(path) + ".corrupt." + time.Now().Format("20060102_150405")
	_ = os.WriteFile(filepath.Join(filepath.Dir(path), name), content, 0o644)
}
