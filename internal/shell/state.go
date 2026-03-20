package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Record struct {
	ID             int       `json:"id"`
	Command        string    `json:"command"`
	SnapshotID     string    `json:"snapshot_id"`
	Backend        string    `json:"backend"`
	StartedAt      time.Time `json:"started_at"`
	EndedAt        time.Time `json:"ended_at"`
	ExitCode       int       `json:"exit_code"`
	RootPID        int       `json:"root_pid"`
	ProcessGroupID int       `json:"process_group_id"`
}

type History struct {
	Max    int      `json:"max"`
	NextID int      `json:"next_id"`
	Items  []Record `json:"items"`
}

func loadHistory(path string, max int) (History, error) {
	if max <= 0 {
		max = 100
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return History{Max: max, NextID: 1, Items: []Record{}}, nil
		}
		return History{}, err
	}
	var h History
	if err = json.Unmarshal(b, &h); err != nil {
		backupCorruptFile(path, b)
		return History{Max: max, NextID: 1, Items: []Record{}}, nil
	}
	if h.Max <= 0 {
		h.Max = max
	}
	if h.NextID <= 0 {
		h.NextID = 1
	}
	if h.Items == nil {
		h.Items = []Record{}
	}
	sort.Slice(h.Items, func(i, j int) bool {
		return h.Items[i].ID < h.Items[j].ID
	})
	return h, nil
}

func saveHistory(path string, h History) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func envPath(stateDir, snapshotID string) string {
	return filepath.Join(stateDir, "env", snapshotID+".json")
}

func snapshotID(id int) string {
	return "cmd-" + fmt.Sprintf("%06d", id)
}
