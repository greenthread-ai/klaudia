package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Plan mode is read-only: a Write the model asks for is refused and nothing
// reaches the disk.
func TestPlanModeBlocksWrite(t *testing.T) {
	m := NewFakeModel(t,
		Use("Write", map[string]any{"file_path": "nope.txt", "content": "x"}),
		Say("could not write"),
	)
	e := NewEnv(t, m)
	r := e.Headless("create nope.txt", "--permission-mode", "plan")

	if _, err := os.Stat(e.Path("nope.txt")); err == nil {
		t.Fatalf("plan mode allowed a write\n%s", r.dump())
	}
	results := toolResults(r.Events())
	if len(results) != 1 || results[0]["is_error"] != true {
		t.Errorf("want the Write refused with an error result, got %v\n%s", results, r.dump())
	}
}

// Autonomous mode does project work without asking and without treating it as
// a host change.
func TestAutonomousProjectWork(t *testing.T) {
	m := NewFakeModel(t,
		Use("Write", map[string]any{"file_path": "auto.txt", "content": "banana"}),
		Use("Bash", map[string]any{"command": "cat auto.txt"}),
		Say("done."),
	)
	e := NewEnv(t, m)
	r := e.Headless("write auto.txt", "--permission-mode", "autonomous")

	if r.ExitCode != 0 {
		t.Fatalf("exit %d, want 0\n%s", r.ExitCode, r.dump())
	}
	if got, _ := os.ReadFile(e.Path("auto.txt")); string(got) != "banana" {
		t.Errorf("auto.txt = %q, want banana", got)
	}
	for _, res := range toolResults(r.Events()) {
		if res["is_error"] == true {
			t.Errorf("project work was refused: %s", resultText(res))
		}
	}
	if strings.Contains(strings.Join(toolUses(r.Events()), ","), "RequestHostChange") {
		t.Errorf("project work triggered a host-change request")
	}
}

// Without --allow-host-changes a change to this machine is refused, the
// refusal explains itself, and the run exits 4 so automation can tell "blocked"
// from "failed".
//
// The probe is a write to ~/.bashrc — host state per docs/trust.md — under the
// run's own temporary HOME. smoke.sh uses `sudo tee /etc/...`; a regression in
// the gate there would modify the machine running the tests, here it modifies a
// temp dir.
func TestHostChangeBlockedExits4(t *testing.T) {
	m := NewFakeModel(t) // filled in once HOME is known
	e := NewEnv(t, m)
	rc := filepath.Join(e.Home, ".bashrc")
	m.Script(
		Use("Bash", map[string]any{"command": "echo e2e-marker >> " + rc}),
		Say("I was not allowed to change the machine."),
	)
	r := e.Headless("append to bashrc", "--permission-mode", "autonomous")

	if b, err := os.ReadFile(rc); err == nil && strings.Contains(string(b), "e2e-marker") {
		t.Fatalf("the host write got through\n%s", r.dump())
	}
	if r.ExitCode != 4 {
		t.Errorf("exit %d, want 4 (host change blocked)\n%s", r.ExitCode, r.dump())
	}
	results := toolResults(r.Events())
	if len(results) == 0 || results[0]["is_error"] != true {
		t.Fatalf("want the Bash call refused, got %v", results)
	}
	msg := strings.ToLower(resultText(results[0]))
	if !strings.Contains(msg, "machine") && !strings.Contains(msg, "host") {
		t.Errorf("the refusal does not explain itself: %q", msg)
	}
}

// With --allow-host-changes a declared change goes through: the model asks via
// RequestHostChange, the flag grants it, and the same write then succeeds.
// Pairs with the test above so a gate that refuses everything cannot pass both.
func TestHostChangeAllowedWithFlag(t *testing.T) {
	m := NewFakeModel(t)
	e := NewEnv(t, m)
	rc := filepath.Join(e.Home, ".bashrc")
	m.Script(
		Use("RequestHostChange", map[string]any{
			"summary": "Add a marker line to the shell profile",
			"reason":  "the e2e test needs a declared host change",
			"paths":   []string{rc},
		}),
		Use("Bash", map[string]any{"command": "echo e2e-marker >> " + rc}),
		Say("done."),
	)
	r := e.Headless("append to bashrc", "--permission-mode", "autonomous", "--allow-host-changes")

	if r.ExitCode != 0 {
		t.Fatalf("exit %d, want 0\n%s", r.ExitCode, r.dump())
	}
	if b, _ := os.ReadFile(rc); !strings.Contains(string(b), "e2e-marker") {
		t.Errorf("--allow-host-changes did not permit the write\n%s", r.dump())
	}
}

// Remote work is governed by the task: ssh to another machine must fail at ssh,
// not at the local host-change guardrail.
func TestRemoteWorkIsNotGatedLocally(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not installed")
	}
	m := NewFakeModel(t,
		Use("Bash", map[string]any{"command": "ssh -o BatchMode=yes -o ConnectTimeout=2 klaudia-e2e.invalid sudo systemctl restart nginx"}),
		Say("ssh failed."),
	)
	e := NewEnv(t, m)
	r := e.Headless("restart nginx on the box", "--permission-mode", "autonomous")

	if r.ExitCode == 4 {
		t.Errorf("remote work exited 4 as if it were a local host change\n%s", r.dump())
	}
	if strings.Contains(r.Stdout, "RequestHostChange to describe") {
		t.Errorf("remote work was gated as a local host change\n%s", r.dump())
	}
}

// --allowedTools on the command line is not a legacy config to migrate, so it
// leaves the guardrail enforcing: autonomous mode is still allowed, and a host
// change is still refused.
func TestAllowedToolsFlagKeepsTheGuardrail(t *testing.T) {
	m := NewFakeModel(t)
	e := NewEnv(t, m)
	rc := filepath.Join(e.Home, ".bashrc")
	m.Script(Use("Bash", map[string]any{"command": "echo e2e-marker >> " + rc}), Say("blocked."))
	r := e.Headless("append to bashrc", "--permission-mode", "autonomous", "--allowedTools", "Read")

	if r.ExitCode != 4 {
		t.Errorf("exit %d, want 4: the guardrail should still be enforcing\n%s", r.ExitCode, r.dump())
	}
	if b, err := os.ReadFile(rc); err == nil && strings.Contains(string(b), "e2e-marker") {
		t.Errorf("the host write got through with --allowedTools")
	}
}

// Rules in config are the migration case: the guardrail starts in observe, and
// autonomous mode — which needs it enforcing — is refused as a usage error.
func TestConfigRulesStartTheGuardrailObserving(t *testing.T) {
	m := NewFakeModel(t)
	e := NewEnv(t, m)
	if err := os.MkdirAll(e.Path(".klaudia"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[permissions]\nallow = [\"Read\"]\n"
	if err := os.WriteFile(e.Path(".klaudia", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	r := e.Headless("x", "--permission-mode", "autonomous")
	if r.ExitCode != 2 || !strings.Contains(r.Stderr, "observe") {
		t.Errorf("exit %d, want 2 naming the observe guardrail\n%s", r.ExitCode, r.dump())
	}
}
