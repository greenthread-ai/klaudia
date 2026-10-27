package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// TestBashPlanModeGatesMutationNotExecution is the fix for the defect
// docs/plan-mode.md opens with: plan mode refused every Bash call, so the
// research it exists for was impossible.
//
// The three verdicts are each represented. The Ask row is the load-bearing
// one — the classifier behind this is an allowlist, and an allowlist that
// refuses what it has not got round to classifying is the original bug with a
// longer fuse.
func TestBashPlanModeGatesMutationNotExecution(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    permission.Behavior
		message string // substring of the deny/ask explanation
	}{
		{name: "git log is research", command: "git log --oneline -13", want: permission.Allow},
		{name: "git diff is research", command: "git diff HEAD~13 --stat", want: permission.Allow},
		{name: "go doc has no tool equivalent", command: "go doc permission.Check", want: permission.Allow},
		{name: "sed as a ranged read", command: "sed -n '1,40p' go.mod", want: permission.Allow},
		{
			name: "a prefix that hides a mutation in the chain", command: "git status && rm -rf build",
			want: permission.Deny, message: "rm writes",
		},
		{
			name: "a write disguised as a read-only program", command: "echo x > main.go",
			want: permission.Deny, message: "main.go",
		},
		{
			name: "committing", command: "git commit -m x",
			want: permission.Deny, message: "writes a commit",
		},
		{
			name: "building, which can leave a binary behind", command: "go build ./cmd/klaudia",
			want: permission.Deny, message: "binary",
		},
		{
			name: "a program nothing classifies", command: "cargo check",
			want: permission.Ask, message: "classifies cargo",
		},
	}

	b, err := NewBash(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(BashInput{Command: tc.command})
			req := b.PermissionRequest(raw)
			got := permission.Check(permission.Context{Mode: permission.StaticMode(permission.ModePlan)}, b, req)
			if got.Behavior != tc.want {
				t.Fatalf("%q in plan mode: behavior = %q (%q), want %q", tc.command, got.Behavior, got.Message, tc.want)
			}
			if tc.message != "" && !strings.Contains(got.Message, tc.message) {
				t.Errorf("%q: message = %q, want it to mention %q", tc.command, got.Message, tc.message)
			}
			if tc.want != permission.Allow && strings.TrimSpace(got.Message) == "" {
				t.Errorf("%q: refused with no explanation", tc.command)
			}
		})
	}
}

// TestBashPermissionRequestCarriesTheWholeLine guards the plumbing the gate
// depends on. Specifier is the prefix, and the prefix of
// `git status && rm -rf build` is `git status` — a gate given only that would
// allow the line.
func TestBashPermissionRequestCarriesTheWholeLine(t *testing.T) {
	b, err := NewBash(nil)
	if err != nil {
		t.Fatal(err)
	}
	const cmd = "git status && rm -rf build"
	raw, _ := json.Marshal(BashInput{Command: cmd})
	req := b.PermissionRequest(raw)
	if req.Specifier != "git status" {
		t.Errorf("specifier = %q, want the command prefix", req.Specifier)
	}
	if req.Raw != cmd {
		t.Errorf("raw = %q, want the whole command line", req.Raw)
	}
}

// TestBashOutsidePlanModeIsUnchanged: the mutation axis applies to plan mode
// only. Everywhere else, what a command may reach is the host gate's job and
// it has already run by the time this is consulted.
func TestBashOutsidePlanModeIsUnchanged(t *testing.T) {
	b, err := NewBash(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []permission.Mode{permission.ModeAutonomous, permission.ModeBypassPermissions} {
		for _, cmd := range []string{"git log -1", "rm -rf build", "cargo check"} {
			raw, _ := json.Marshal(BashInput{Command: cmd})
			got := permission.Check(
				permission.Context{Mode: permission.StaticMode(mode)}, b, b.PermissionRequest(raw))
			if got.Behavior != permission.Allow {
				t.Errorf("%s / %q: behavior = %q, want %q", mode, cmd, got.Behavior, permission.Allow)
			}
		}
	}
}
