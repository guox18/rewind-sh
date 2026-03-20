package stream

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ViewResult struct {
	Total  int      `json:"total"`
	Offset int      `json:"offset"`
	Limit  int      `json:"limit"`
	Lines  []string `json:"lines"`
}

type ViewOptions struct {
	File       string
	Offset     int
	Limit      int
	CursorFile string
	Move       int
}

func View(opts ViewOptions) (ViewResult, error) {
	if opts.Limit <= 0 {
		return ViewResult{}, errors.New("limit 必须大于 0")
	}
	lines, err := readLogLines(opts.File)
	if err != nil {
		return ViewResult{}, err
	}
	offset := opts.Offset
	if opts.CursorFile != "" {
		offset, err = loadCursor(opts.CursorFile)
		if err != nil {
			return ViewResult{}, err
		}
		offset += opts.Move
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := offset + opts.Limit
	if end > len(lines) {
		end = len(lines)
	}
	if opts.CursorFile != "" {
		if err = saveCursor(opts.CursorFile, offset); err != nil {
			return ViewResult{}, err
		}
	}
	return ViewResult{
		Total:  len(lines),
		Offset: offset,
		Limit:  opts.Limit,
		Lines:  lines[offset:end],
	}, nil
}

func readLogLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 1024), 8*1024*1024)
	out := make([]string, 0, 256)
	for s.Scan() {
		raw := s.Text()
		var e Event
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			out = append(out, raw)
			continue
		}
		out = append(out, e.Time.Format("2006-01-02 15:04:05")+" ["+e.Stream+"] "+e.Text)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func loadCursor(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, nil
	}
	return n, nil
}

func saveCursor(path string, n int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(n)), 0o644)
}
