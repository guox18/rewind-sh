package stream

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestViewWithCursorMove(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "a.jsonl")
	w, err := NewLogWriter(logFile)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err = w.WriteEvent(Event{
			Time:   time.Unix(int64(i), 0),
			Stream: "stdout",
			Text:   "line",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	cursor := filepath.Join(dir, "cursor.txt")
	r1, err := View(ViewOptions{
		File:       logFile,
		Limit:      2,
		CursorFile: cursor,
		Move:       0,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("r1 offset=%d lines=%d", r1.Offset, len(r1.Lines))
	r2, err := View(ViewOptions{
		File:       logFile,
		Limit:      2,
		CursorFile: cursor,
		Move:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("r2 offset=%d lines=%d", r2.Offset, len(r2.Lines))
	if r2.Offset != 2 {
		t.Fatalf("want offset 2 got %d", r2.Offset)
	}
	b, err := os.ReadFile(cursor)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cursor=%s", string(b))
}
