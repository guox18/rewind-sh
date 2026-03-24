package runner

import (
	"testing"
)

func TestRunBehaviorWithPrintedSteps(t *testing.T) {
	opts := RunOptions{
		Command: "printf 'out-1\\n'; printf 'err-1\\n' 1>&2; printf 'out-2\\n'",
	}
	t.Logf("step=run start command=%s", opts.Command)
	res, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("step=run done exit=%d pid=%d pgid=%d", res.ExitCode, res.RootPID, res.ProcessGroupID)
	if res.ExitCode != 0 {
		t.Fatalf("exit code want 0 got %d", res.ExitCode)
	}
}
