package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// relWriteTool stands in for Write. It writes to a fixed name under whatever
// working directory it is handed, which is the only way a scripted test can
// write into a checkout whose path is chosen at spawn time. It is named "Write"
// because the spawner decides on tool names: that is what marks a sub-agent as
// one that can change the tree.
type relWriteTool struct {
	rel  string
	body string
	dirs []string // the working directories it was actually handed
}

func (relWriteTool) Name() string                                { return "Write" }
func (relWriteTool) Description(context.Context) (string, error) { return "", nil }
func (relWriteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (relWriteTool) ValidateInput(json.RawMessage) error { return nil }
func (relWriteTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (relWriteTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (w *relWriteTool) Execute(_ context.Context, tctx tools.Context, _ json.RawMessage) ([]tools.Result, error) {
	w.dirs = append(w.dirs, tctx.WorkingDir)
	path := filepath.Join(tctx.WorkingDir, w.rel)
	if err := os.WriteFile(path, []byte(w.body), 0o644); err != nil {
		return nil, err
	}
	return []tools.Result{{Content: "wrote " + path}}, nil
}

// gitRepo builds a one-commit repository and points worktree storage at a
// scratch config dir.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
		{"add", "-A"},
		{"commit", "--quiet", "--allow-empty", "-m", "first"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return root
}

// errorProvider models a child whose turn fails outright.
type errorProvider struct{}

func (errorProvider) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	return anthropic.BetaMessage{}, errors.New("provider exploded")
}

// namedStub is a tool that exists only to carry a name past writesFiles.
type namedStub string

func (n namedStub) Name() string                              { return string(n) }
func (namedStub) Description(context.Context) (string, error) { return "", nil }
func (namedStub) InputSchema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (namedStub) ValidateInput(json.RawMessage) error         { return nil }
func (namedStub) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (namedStub) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (namedStub) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	return []tools.Result{{Content: "ok"}}, nil
}

func writingSpawner(t *testing.T, w *relWriteTool, dir string) *Spawner {
	t.Helper()
	return newTestSpawner(&scriptedProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu1", "Write", map[string]any{}),
	}}, tools.NewRegistry(w), "claude-opus-4-8", bypassPerm(), nil, 2).
		WithWorkingDir(dir)
}

// The whole point of the feature: two of these can run at once, so neither may
// write into the tree the other is reading. The child's tools must therefore be
// handed a checkout of its own — and the work must still arrive in the user's
// tree when it finishes, or isolation would just be a way to lose it.
func TestWritingSubAgentWorksInItsOwnCheckoutAndTheWorkComesBack(t *testing.T) {
	root := gitRepo(t)
	w := &relWriteTool{rel: "made.txt", body: "the child's work\n"}

	text, err := writingSpawner(t, w, root).WithIsolator(testIsolator(true)).
		Spawn(context.Background(), "general-purpose", "write it", nil)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if len(w.dirs) != 1 {
		t.Fatalf("tool ran %d times, want 1", len(w.dirs))
	}
	if w.dirs[0] == root {
		t.Fatal("the child wrote straight into the user's tree; nothing was isolated")
	}
	if _, err := os.Stat(w.dirs[0]); err == nil {
		t.Errorf("the checkout at %s outlived the sub-agent", w.dirs[0])
	}
	body, err := os.ReadFile(filepath.Join(root, "made.txt"))
	if err != nil {
		t.Fatalf("the child's work never reached the working tree: %v", err)
	}
	if string(body) != "the child's work\n" {
		t.Errorf("made.txt = %q", body)
	}
	// The model asked for files to change; it has to be told that they did.
	if !strings.Contains(text, "1 file applied to the working tree") {
		t.Errorf("result does not report what landed:\n%s", text)
	}
}

// reportProvider models the normal shape of a writing child: it calls the tool,
// then reports the path it wrote. The path it has to hand is the checkout's.
type reportProvider struct {
	t *testing.T
	w *relWriteTool
	n int
}

func (p *reportProvider) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, sink api.StreamSink) (anthropic.BetaMessage, error) {
	p.n++
	if p.n == 1 {
		return toolUseTurn(p.t, "tu1", "Write", map[string]any{}), nil
	}
	text := "wrote " + filepath.Join(p.w.dirs[0], p.w.rel)
	if sink.OnText != nil {
		sink.OnText(text)
	}
	return textTurn(p.t, text), nil
}

// textTurn builds a finished assistant turn. Via JSON because the SDK resolves
// a content block from its raw fields, which a struct literal leaves empty —
// the loop would read no text at all.
func textTurn(t *testing.T, text string) anthropic.BetaMessage {
	t.Helper()
	body, _ := json.Marshal(text)
	raw := fmt.Sprintf(`{"role":"assistant","stop_reason":"end_turn",
		"content":[{"type":"text","text":%s}]}`, body)
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal text turn: %v", err)
	}
	return m
}

// A path inside the checkout is a path nobody else can open: the parent model
// would Read it and find nothing, and the user would be told to look somewhere
// that no longer exists once the checkout is removed. The child's report is
// translated back to the project on the way out.
func TestSubAgentResultNamesPathsInTheUsersTree(t *testing.T) {
	root := gitRepo(t)
	w := &relWriteTool{rel: "made.txt", body: "x\n"}
	s := newTestSpawner(&reportProvider{t: t, w: w}, tools.NewRegistry(w), "claude-opus-4-8",
		bypassPerm(), nil, 3).WithWorkingDir(root).WithIsolator(testIsolator(true))

	text, err := s.Spawn(context.Background(), "general-purpose", "write it", nil)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if checkout := w.dirs[0]; strings.Contains(text, checkout) {
		t.Errorf("the child's report cites the checkout, which is gone:\n%s", text)
	}
	if want := filepath.Join(root, "made.txt"); !strings.Contains(text, want) {
		t.Errorf("report does not name %s:\n%s", want, text)
	}
}

func TestSubAgentSharesTheTreeWhenItShould(t *testing.T) {
	tests := []struct {
		name      string
		worktrees bool
		repo      bool
		canWrite  bool
	}{
		{
			// Explore and Plan read and report paths; a checkout would make
			// every path they return wrong, and there is nothing to isolate.
			name:      "a read-only toolset has nothing to isolate",
			worktrees: true, repo: true, canWrite: false,
		},
		{name: "turned off in config", worktrees: false, repo: true, canWrite: true},
		{
			// Not every project is a git repository, and that is not a reason
			// to refuse the work.
			name: "not a git repository", worktrees: true, repo: false, canWrite: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.repo {
				root = gitRepo(t)
			} else if _, err := exec.LookPath("git"); err != nil {
				t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
			}

			w := &relWriteTool{rel: "made.txt", body: "x\n"}
			capture := &captureTool{}
			reg, toolName := tools.NewRegistry(capture), "Capture"
			if tt.canWrite {
				reg, toolName = tools.NewRegistry(w), "Write"
			}
			s := newTestSpawner(&scriptedProvider{turns: []anthropic.BetaMessage{
				toolUseTurn(t, "tu1", toolName, map[string]any{}),
			}}, reg, "claude-opus-4-8", bypassPerm(), nil, 2).
				WithWorkingDir(root).WithIsolator(testIsolator(tt.worktrees))

			if _, err := s.Spawn(context.Background(), "general-purpose", "go", nil); err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			got := capture.got.WorkingDir
			if tt.canWrite {
				if len(w.dirs) != 1 {
					t.Fatalf("tool ran %d times, want 1", len(w.dirs))
				}
				got = w.dirs[0]
			}
			if got != root {
				t.Errorf("child ran in %q, want the project root %q", got, root)
			}
		})
	}
}

// A sub-agent that failed left a checkout in a state nobody has looked at.
// Applying half of it to the user's tree is the outcome isolation exists to
// prevent, so the work is kept and the error says where.
func TestFailedSubAgentKeepsItsCheckoutAndSaysWhere(t *testing.T) {
	root := gitRepo(t)
	w := &relWriteTool{rel: "made.txt", body: "half-finished\n"}
	s := newTestSpawner(&errorProvider{}, tools.NewRegistry(w), "claude-opus-4-8",
		bypassPerm(), nil, 2).WithWorkingDir(root).WithIsolator(testIsolator(true))

	_, err := s.Spawn(context.Background(), "general-purpose", "go", nil)
	if err == nil {
		t.Fatal("expected the provider's error")
	}
	if !strings.Contains(err.Error(), "left in") {
		t.Errorf("error does not say where the changes are: %v", err)
	}
	dir := strings.TrimSuffix(err.Error()[strings.LastIndex(err.Error(), "left in ")+len("left in "):], ")")
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Errorf("checkout %s was removed with the child's work in it", dir)
	}
	if _, statErr := os.Stat(filepath.Join(root, "made.txt")); statErr == nil {
		t.Error("a failed child's work was applied to the user's tree")
	}
}

func TestWritesFiles(t *testing.T) {
	tests := []struct {
		name  string
		tools []string
		want  bool
	}{
		{name: "the read-only three", tools: []string{"Read", "Glob", "Grep"}, want: false},
		{name: "Write", tools: []string{"Read", "Write"}, want: true},
		{name: "Edit", tools: []string{"Edit"}, want: true},
		{name: "NotebookEdit", tools: []string{"NotebookEdit"}, want: true},
		{name: "Bash can write anything", tools: []string{"Read", "Bash"}, want: true},
		{name: "nothing at all", tools: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts []tools.Tool
			for _, n := range tt.tools {
				ts = append(ts, namedStub(n))
			}
			if got := writesFiles(tools.NewRegistry(ts...)); got != tt.want {
				t.Errorf("writesFiles(%v) = %v, want %v", tt.tools, got, tt.want)
			}
		})
	}
}
