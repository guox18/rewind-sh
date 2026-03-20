package stream

import (
	"testing"
	"time"
)

func TestBufferHeadTail(t *testing.T) {
	b := NewBuffer(2, 2)
	now := time.Now()
	for i := 0; i < 5; i++ {
		b.Add(Event{
			Time:   now.Add(time.Duration(i) * time.Second),
			Stream: "stdout",
			Text:   "line",
		})
	}
	s := b.Snapshot()
	t.Logf("total=%d head=%d tail=%d", s.Total, len(s.Head), len(s.Tail))
	if s.Total != 5 {
		t.Fatalf("total want 5 got %d", s.Total)
	}
	if len(s.Head) != 2 {
		t.Fatalf("head size want 2 got %d", len(s.Head))
	}
	if len(s.Tail) != 2 {
		t.Fatalf("tail size want 2 got %d", len(s.Tail))
	}
}
