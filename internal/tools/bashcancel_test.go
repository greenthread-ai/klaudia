package tools

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// A user interrupt and a timeout look identical at the shell — a killed process
// with no useful exit status. A result that doesn't say which invites the reader
// to guess, and one did: it told the user their rebuild had "hit my 15-minute
// tool timeout" when they had interrupted it themselves four minutes in.
func TestInterruptedCommandSaysSoAndIsNotATimeout(t *testing.T) {
	out, _ := formatBashOutput(sandbox.Response{
		Stdout: "==> Creating kind cluster\n", Canceled: true, ExitCode: 130,
	}, "./scripts/up.sh")

	if !strings.Contains(out, "interrupted by the user") {
		t.Errorf("interruption not stated:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "timed out") {
		t.Errorf("an interrupt was described as a timeout:\n%s", out)
	}
	// The bare exit code was the whole problem — it said nothing.
	if strings.Contains(out, "[exit code 130]") {
		t.Errorf("fell through to a bare exit code:\n%s", out)
	}
	// Output produced before the interrupt is still worth keeping.
	if !strings.Contains(out, "Creating kind cluster") {
		t.Errorf("partial output was dropped:\n%s", out)
	}
}

// A real timeout must still read as one.
func TestTimeoutStillReportsATimeout(t *testing.T) {
	out, _ := formatBashOutput(sandbox.Response{TimedOut: true, ExitCode: 124}, "sleep 999")
	if !strings.Contains(out, "timed out") {
		t.Errorf("timeout no longer reported:\n%s", out)
	}
}

// An ordinary failure is unchanged.
func TestPlainFailureKeepsItsExitCode(t *testing.T) {
	out, _ := formatBashOutput(sandbox.Response{Stderr: "boom", ExitCode: 2}, "false")
	if !strings.Contains(out, "[exit code 2]") {
		t.Errorf("exit code lost:\n%s", out)
	}
}
