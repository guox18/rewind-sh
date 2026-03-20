package checkpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type EnvValue struct {
	Present bool   `json:"present"`
	Value   string `json:"value"`
}

type FileEntry struct {
	Source       string `json:"source"`
	SnapshotFile string `json:"snapshot_file"`
}

type Snapshot struct {
	Name      string              `json:"name"`
	CreatedAt time.Time           `json:"created_at"`
	Files     []FileEntry         `json:"files"`
	Env       map[string]EnvValue `json:"env"`
}

type Store struct {
	Root string
}

func NewStore(root string) *Store {
	return &Store{Root: root}
}

func (s *Store) Create(name string, files []string, envKeys []string) (Snapshot, error) {
	if strings.TrimSpace(name) == "" {
		return Snapshot{}, errors.New("checkpoint 名称不能为空")
	}
	dir := filepath.Join(s.Root, name)
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		return Snapshot{}, err
	}
	entries := make([]FileEntry, 0, len(files))
	for i, f := range files {
		src := filepath.Clean(f)
		b, err := os.ReadFile(src)
		if err != nil {
			return Snapshot{}, err
		}
		sn := filepath.Join("files", fileName(i, src))
		snAbs := filepath.Join(dir, sn)
		if err = os.WriteFile(snAbs, b, 0o644); err != nil {
			return Snapshot{}, err
		}
		entries = append(entries, FileEntry{
			Source:       src,
			SnapshotFile: sn,
		})
	}
	env := make(map[string]EnvValue, len(envKeys))
	for _, k := range envKeys {
		v, ok := os.LookupEnv(strings.TrimSpace(k))
		env[k] = EnvValue{Present: ok, Value: v}
	}
	snap := Snapshot{
		Name:      name,
		CreatedAt: time.Now(),
		Files:     entries,
		Env:       env,
	}
	if err := s.writeMeta(dir, snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func (s *Store) Restore(name string) (Snapshot, error) {
	dir := filepath.Join(s.Root, name)
	snap, err := s.readMeta(dir)
	if err != nil {
		return Snapshot{}, err
	}
	for _, entry := range snap.Files {
		b, err := os.ReadFile(filepath.Join(dir, entry.SnapshotFile))
		if err != nil {
			return Snapshot{}, err
		}
		if err = os.MkdirAll(filepath.Dir(entry.Source), 0o755); err != nil {
			return Snapshot{}, err
		}
		if err = os.WriteFile(entry.Source, b, 0o644); err != nil {
			return Snapshot{}, err
		}
	}
	return snap, nil
}

func (s *Store) EnvScript(name string) (string, error) {
	dir := filepath.Join(s.Root, name)
	snap, err := s.readMeta(dir)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(snap.Env))
	for k, v := range snap.Env {
		if v.Present {
			lines = append(lines, "export "+k+"="+shellQuote(v.Value))
			continue
		}
		lines = append(lines, "unset "+k)
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Store) List() ([]Snapshot, error) {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		snap, err := s.readMeta(filepath.Join(s.Root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *Store) writeMeta(dir string, snap Snapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
}

func (s *Store) readMeta(dir string) (Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err = json.Unmarshal(b, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func fileName(index int, src string) string {
	base := strings.ReplaceAll(src, string(filepath.Separator), "_")
	base = strings.ReplaceAll(base, ":", "_")
	return fmt.Sprintf("%03d_%s", index, strings.Trim(base, "_"))
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
