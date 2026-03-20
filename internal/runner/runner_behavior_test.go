package runner

import (
	"path/filepath"
	"testing"

	"rewindsh/internal/stream"
)

func TestRunBehaviorWithPrintedSteps(t *testing.T) {
	dir := t.TempDir()
	opts := RunOptions{
		Command:   "printf 'out-1\\n'; printf 'err-1\\n' 1>&2; printf 'out-2\\n'",
		LogDir:    filepath.Join(dir, "logs"),
		HeadLines: 2,
		TailLines: 2,
	}
	t.Logf("step=run start command=%s", opts.Command)
	res, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=run done log=%s exit=%d total=%d", res.LogFile, res.ExitCode, res.Summary.Total)
	if res.Summary.Total != 3 {
		t.Fatalf("total want 3 got %d", res.Summary.Total)
	}

	t.Log("step=view start")
	view, err := stream.View(stream.ViewOptions{
		File:   res.LogFile,
		Offset: 0,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=view done total=%d lines=%d", view.Total, len(view.Lines))
	for i, line := range view.Lines {
		t.Logf("line[%d]=%s", i, line)
	}
	if view.Total != 3 {
		t.Fatalf("view total want 3 got %d", view.Total)
	}
}
