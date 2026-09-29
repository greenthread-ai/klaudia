package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// paramsProvider replays turns and keeps every request it was sent.
type paramsProvider struct {
	turns []anthropic.BetaMessage
	sent  []anthropic.BetaMessageNewParams
}

func (p *paramsProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.sent = append(p.sent, params)
	if len(p.sent) > len(p.turns) {
		return anthropic.BetaMessage{StopReason: "end_turn"}, nil
	}
	return p.turns[len(p.sent)-1], nil
}

func turnFromJSON(t *testing.T, raw string) anthropic.BetaMessage {
	t.Helper()
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A general-purpose sub-agent gets "*": before, that meant the parent's
// TodoWrite (and its store), AskUserQuestion with nobody to answer it, and a
// system prompt holding only the type's own paragraph.
func TestSpawnGivesTheChildContextItsOwnTodosAndNoQuestions(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep a real ~/.claude/CLAUDE.md out of it
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Always run gofmt."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".klaudia", "KNOWLEDGE.md"), []byte("The API is v2."), 0o644); err != nil {
		t.Fatal(err)
	}

	parentTodos := &tools.TodoStore{}
	todo, err := tools.NewTodoWrite(parentTodos)
	if err != nil {
		t.Fatal(err)
	}
	ask, err := tools.NewAskUserQuestion()
	if err != nil {
		t.Fatal(err)
	}
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	// The parent's own list, which the child must leave alone.
	if _, err := todo.Execute(context.Background(), tools.Context{},
		json.RawMessage(`{"todos":[{"content":"parent task","status":"in_progress"}]}`)); err != nil {
		t.Fatal(err)
	}

	provider := &paramsProvider{turns: []anthropic.BetaMessage{
		turnFromJSON(t, `{"role":"assistant","stop_reason":"tool_use",
			"usage":{"input_tokens":100,"cache_read_input_tokens":50,"output_tokens":20},
			"content":[{"type":"tool_use","id":"tu1","name":"TodoWrite",
				"input":{"todos":[{"content":"child task","status":"pending"}]}}]}`),
		turnFromJSON(t, `{"role":"assistant","stop_reason":"end_turn",
			"usage":{"input_tokens":200,"output_tokens":30},
			"content":[{"type":"text","text":"done"}]}`),
	}}
	sp := NewSpawner(provider, tools.NewRegistry(todo, ask, read), "claude-opus-4-8",
		bypassPerm(), nil, 0).WithWorkingDir(dir)

	out, err := sp.Spawn(context.Background(), "general-purpose", "do it", nil)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(provider.sent) == 0 {
		t.Fatal("no request sent")
	}
	first := provider.sent[0]

	var system strings.Builder
	for _, b := range first.System {
		system.WriteString(b.Text)
	}
	for _, want := range []string{
		"You are an agent for Klaudia", // the type's prompt still leads
		"<env>",
		"Working directory: " + dir,
		"Always run gofmt.",
		"The API is v2.",
	} {
		if !strings.Contains(system.String(), want) {
			t.Errorf("sub-agent system prompt missing %q:\n%s", want, system.String())
		}
	}

	names := namesOf(first.Tools)
	if slices.Contains(names, "AskUserQuestion") {
		t.Errorf("sub-agent offered AskUserQuestion: %v", names)
	}
	if !slices.Contains(names, "TodoWrite") || !slices.Contains(names, "Read") {
		t.Errorf("sub-agent lost tools it should keep: %v", names)
	}

	if got := parentTodos.Items(); len(got) != 1 || got[0].Content != "parent task" {
		t.Errorf("child's TodoWrite changed the parent's list: %+v", got)
	}

	if !strings.HasPrefix(out, "done") {
		t.Errorf("result should lead with the child's answer: %q", out)
	}
	for _, want := range []string{"<usage>", "turns: 2", "input_tokens: 350", "output_tokens: 50", "duration_ms: "} {
		if !strings.Contains(out, want) {
			t.Errorf("result missing %q: %q", want, out)
		}
	}
}

// Read-only types never had TodoWrite; the adjustment must not add it.
func TestSubagentToolsAddsNothing(t *testing.T) {
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	got := subagentTools(tools.NewRegistry(read)).Names()
	if !slices.Equal(got, []string{"Read"}) {
		t.Errorf("tools = %v, want [Read]", got)
	}
}
