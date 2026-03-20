package snapshot

import (
	"runtime"
	"testing"
)

func TestDiagnoseAuto(t *testing.T) {
	res, err := Diagnose("auto", BackendOptions{})
	if runtime.GOOS != "linux" {
		t.Logf("non-linux err=%v", err)
		if err == nil {
			t.Fatal("non-linux should return unavailable error")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("requested=%s resolved=%s count=%d", res.Requested, res.Resolved, len(res.Statuses))
	if res.Requested != "auto" {
		t.Fatalf("requested want auto got %s", res.Requested)
	}
	if res.Resolved != "watch-diff" {
		t.Fatalf("resolved want watch-diff got %s", res.Resolved)
	}
	if len(res.Statuses) == 0 {
		t.Fatal("statuses should not be empty")
	}
}

func TestDiagnoseUnknown(t *testing.T) {
	_, err := Diagnose("unknown", BackendOptions{})
	t.Logf("err=%v", err)
	if err == nil {
		t.Fatal("want error for unknown backend")
	}
}
