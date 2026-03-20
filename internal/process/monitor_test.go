package process

import (
	"errors"
	"testing"
)

func TestParsePS(t *testing.T) {
	in := []byte("  PID STARTED COMMAND\n  101 Mon Jan  2 15:04:05 2006 /bin/sleep 10\n  202 Tue Jan  3 16:05:06 2006 /usr/bin/python app.py\n")
	items := parsePS(in, "python")
	t.Logf("items=%d", len(items))
	if len(items) != 1 {
		t.Fatalf("want 1 got %d", len(items))
	}
	if items[0].PID != 202 {
		t.Fatalf("pid mismatch got %d", items[0].PID)
	}
}

func TestIsPermissionDenied(t *testing.T) {
	e1 := errors.New("fork/exec /bin/ps: operation not permitted")
	e2 := errors.New("permission denied")
	e3 := errors.New("binary not found")
	t.Logf("e1=%v e2=%v e3=%v", IsPermissionDenied(e1), IsPermissionDenied(e2), IsPermissionDenied(e3))
	if !IsPermissionDenied(e1) {
		t.Fatal("e1 should be permission denied")
	}
	if !IsPermissionDenied(e2) {
		t.Fatal("e2 should be permission denied")
	}
	if IsPermissionDenied(e3) {
		t.Fatal("e3 should not be permission denied")
	}
}
