package cli

import (
	"os/exec"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

func TestSandboxFailIfUnavailable(t *testing.T) {
	var warned []string
	warn := func(m string) { warned = append(warned, m) }

	// Deterministic: container mode with no image cannot be used.
	ex, err := buildExecutor(config.Sandbox{Mode: config.SandboxContainer}, warn)
	if err != nil || ex == nil || len(warned) != 1 || !strings.Contains(warned[0], "falling back") {
		t.Fatalf("default: ex %T err %v warned %v; want a local fallback with a warning", ex, err, warned)
	}
	if _, ok := ex.(*sandbox.Local); !ok {
		t.Errorf("fallback executor = %T, want *sandbox.Local", ex)
	}
	ex, err = buildExecutor(config.Sandbox{Mode: config.SandboxContainer, FailIfUnavailable: true}, warn)
	if err == nil || ex != nil || !strings.Contains(err.Error(), "failIfUnavailable") {
		t.Errorf("failIfUnavailable: ex %T err %v; want an error naming the setting", ex, err)
	}
}

// bwrap can be installed and still unable to run (no unprivileged user
// namespaces). That used to pass the PATH check and fail every command.
func TestBwrapProbe(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	works := exec.Command("bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "true").Run() == nil
	var warned []string
	ex, err := buildExecutor(config.Sandbox{Mode: config.SandboxOS}, func(m string) { warned = append(warned, m) })
	if err != nil {
		t.Fatal(err)
	}
	_, isBwrap := ex.(*sandbox.Bwrap)
	if works != isBwrap {
		t.Errorf("bwrap usable=%v but executor is %T (warnings %v)", works, ex, warned)
	}
	if !works && (len(warned) != 1 || !strings.Contains(warned[0], "cannot run")) {
		t.Errorf("warnings = %v, want one saying bwrap cannot run here", warned)
	}
	t.Logf("bwrap usable here: %v; executor %T", works, ex)
}
