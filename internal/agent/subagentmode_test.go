package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/schema"
)

// modeProbe stands in for Bash in a child's registry and records the mode its
// permission check ran under. It is named "Bash" so the research agents' Tools
// list admits it.
type modeProbe struct {
	schema *schema.Schema
	seen   []permission.Mode
}

func newModeProbe(t *testing.T) *modeProbe {
	t.Helper()
	s, err := schema.For[tools.BashInput]()
	if err != nil {
		t.Fatal(err)
	}
	return &modeProbe{schema: s}
}

func (p *modeProbe) Name() string                                { return "Bash" }
func (p *modeProbe) Description(context.Context) (string, error) { return "probe", nil }
func (p *modeProbe) InputSchema() json.RawMessage                { return p.schema.Raw }
func (p *modeProbe) ValidateInput(json.RawMessage) error         { return nil }
func (p *modeProbe) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (p *modeProbe) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	p.seen = append(p.seen, permission.CurrentMode(pctx))
	return permission.Decision{Behavior: permission.Allow}
}

func (p *modeProbe) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	return []tools.Result{{Content: "ok"}}, nil
}

// TestResearchSubagentsRunUnderPlanMode is the sub-agent half of the plan-mode
// defect: the two research agents were held read-only by being denied Bash,
// which also denied them `git log`, `git diff` and `go doc` — the commands
// research is actually made of. Delegating was no way round it.
//
// They now carry Bash and are held to read-only by *mode*, which is a stronger
// guarantee than a short tool list and a far less crippling one. What plan mode
// then permits is tools.TestBashPlanModeGatesMutationNotExecution's business;
// this is only that the mode arrives.
func TestResearchSubagentsRunUnderPlanMode(t *testing.T) {
	tests := []struct {
		name   string
		agent  string
		parent permission.Mode
		want   permission.Mode
	}{
		{
			name:  "Plan is read-only inside an autonomous session",
			agent: "Plan", parent: permission.ModeAutonomous, want: permission.ModePlan,
		},
		{
			name:  "Explore is read-only inside an autonomous session",
			agent: "Explore", parent: permission.ModeAutonomous, want: permission.ModePlan,
		},
		{
			name:  "general-purpose still inherits the session",
			agent: "general-purpose", parent: permission.ModeAutonomous, want: permission.ModeAutonomous,
		},
		// Bypass cannot be observed here: permission.Check short-circuits it
		// before the tool's own check runs, so the probe is never consulted.
		// TestResearchSubagentCannotWriteThroughTheShell covers it from the
		// only side that can see it — whether the file is still there.
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			probe := newModeProbe(t)
			provider := &scriptedProvider{turns: []anthropic.BetaMessage{
				toolUseTurn(t, "tu1", "Bash", map[string]any{"command": "git log -1"}),
			}}
			s := newTestSpawner(provider, tools.NewRegistry(probe), "claude-opus-4-8",
				permission.Context{Mode: permission.StaticMode(tc.parent)}, nil, 2).
				WithWorkingDir(dir)

			if _, err := s.Spawn(context.Background(), tc.agent, "look", nil); err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			if len(probe.seen) == 0 {
				t.Fatal("the child never reached the permission check — it was not given a shell")
			}
			if probe.seen[0] != tc.want {
				t.Errorf("child checked under mode %q, want %q", probe.seen[0], tc.want)
			}
		})
	}
}

// TestResearchSubagentCannotWriteThroughTheShell is the same guarantee end to
// end, through the real Bash tool and the real gate: the parent is autonomous,
// and the child still cannot delete a file.
func TestResearchSubagentCannotWriteThroughTheShell(t *testing.T) {
	for _, tc := range []struct {
		name    string
		parent  permission.Mode
		command string
		survive bool
	}{
		{name: "rm is refused", parent: permission.ModeAutonomous, command: "rm notes.txt", survive: true},
		{name: "a redirection is refused", parent: permission.ModeAutonomous, command: "echo x > notes.txt", survive: true},
		{name: "bypass still deletes", parent: permission.ModeBypassPermissions, command: "rm notes.txt", survive: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notes.txt")
			if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
				t.Fatal(err)
			}
			read, err := tools.NewRead()
			if err != nil {
				t.Fatal(err)
			}
			bash, err := tools.NewBash(sandbox.NewLocal())
			if err != nil {
				t.Fatal(err)
			}
			provider := &scriptedProvider{turns: []anthropic.BetaMessage{
				toolUseTurn(t, "tu1", "Bash", map[string]any{"command": tc.command}),
			}}
			s := newTestSpawner(provider, tools.NewRegistry(read, bash), "claude-opus-4-8",
				permission.Context{Mode: permission.StaticMode(tc.parent)}, nil, 2).
				WithWorkingDir(dir)

			if _, err := s.Spawn(context.Background(), "Plan", "delete it", nil); err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			got, readErr := os.ReadFile(path)
			if tc.survive {
				if readErr != nil {
					t.Fatalf("the Plan sub-agent deleted a file: %v", readErr)
				}
				if string(got) != "hello" {
					t.Errorf("notes.txt = %q, want it untouched", got)
				}
				return
			}
			if readErr == nil {
				t.Error("the forced plan mode overrode bypass, which the user had explicitly asked for")
			}
		})
	}
}

var _ api.Provider = (*scriptedProvider)(nil)
