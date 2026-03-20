package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func saveEnv(stateDir, snapshotID string) error {
	env := make(map[string]string, 128)
	for _, item := range os.Environ() {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		env[k] = v
	}
	p := envPath(stateDir, snapshotID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

func restoreEnv(stateDir, snapshotID string) error {
	p := envPath(stateDir, snapshotID)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	target := map[string]string{}
	if err = json.Unmarshal(b, &target); err != nil {
		backupCorruptFile(p, b)
		return nil
	}
	current := map[string]struct{}{}
	for _, item := range os.Environ() {
		k, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		current[k] = struct{}{}
	}
	for k := range current {
		if _, ok := target[k]; !ok {
			if err = os.Unsetenv(k); err != nil {
				return err
			}
		}
	}
	for k, v := range target {
		if err = os.Setenv(k, v); err != nil {
			return err
		}
	}
	return nil
}
