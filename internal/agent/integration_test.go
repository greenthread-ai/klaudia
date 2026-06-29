package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// bypassPerm is a permission context that allows everything, matching
// --dangerously-skip-permissions. Used so the loop dispatches mutating tools
// without an interactive Approver.
func bypassPerm() permission.Context {
	return permission.Context{Mode: func() permission.Mode { return permission.ModeBypassPermissions }}
}

// toolUseTurn builds an assistant message with a single tool_use block, via JSON
// so the SDK populates the internal raw fields the dispatcher reads.
func toolUseTurn(t *testing.T, id, name string, input map[string]any) anthropic.BetaMessage {
	t.Helper()
	inB, _ := json.Marshal(input)
	raw := fmt.Sprintf(`{"role":"assistant","stop_reason":"tool_use",
		"content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}]}`, id, name, inB)
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal tool_use turn: %v", err)
	}
	return m
}

// End-to-end through the real agent loop with the real Write and Read tools: the
// model writes a file, then reads it back. Proves tool_use → permission check →
// real Execute → tool_result round-trips for genuine client-side tools, not
// mocks.
func TestLoopExecutesRealWriteThenRead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	const body = "hello from the agent loop"

	write, err := tools.NewWrite()
	if err != nil {
		t.Fatal(err)
	}
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry(write, read)

	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu_write", "Write", map[string]any{"file_path": target, "content": body}),
		toolUseTurn(t, "tu_read", "Read", map[string]any{"file_path": target}),
		// third turn: scriptedProvider returns end_turn automatically
	}}

	loop := New(provider, reg)
	res, err := loop.Run(context.Background(), Options{
		Model:      "test",
		Permission: bypassPerm(),
		MaxTurns:   5,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The Write tool actually created the file on disk.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("Write tool did not create the file: %v", err)
	}
	if string(got) != body {
		t.Errorf("file content = %q, want %q", got, body)
	}

	// The Read tool's result (a user/tool_result message) must contain the body,
	// proving the read flowed back through the loop.
	if !messagesContain(res.Messages, body) {
		t.Errorf("Read tool_result did not surface the file body in the conversation")
	}
}

// A mutating tool is denied in plan mode — the loop must not execute it and the
// file must not appear. Guards the read-only guarantee of plan mode end to end.
func TestLoopPlanModeDeniesWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "should-not-exist.txt")

	write, _ := tools.NewWrite()
	reg := tools.NewRegistry(write)
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu_write", "Write", map[string]any{"file_path": target, "content": "nope"}),
	}}

	loop := New(provider, reg)
	if _, err := loop.Run(context.Background(), Options{
		Model:      "test",
		Permission: permission.Context{Mode: func() permission.Mode { return permission.ModePlan }},
		MaxTurns:   5,
	}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("plan mode must not write files; stat err = %v", err)
	}
}

// Persist a conversation to a transcript, then reconstruct it the way resume
// does (MostRecent → Read → MessagesFromEntries). Guards the resume path end to
// end: a recorded session round-trips back into loadable messages, and a
// session that recorded real turns is found by auto-resume.
func TestTranscriptResumeRoundTrip(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/proj"
	const sid = "round-trip"

	tr, err := session.NewTranscript(session.Meta{SessionID: sid, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	user := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("what is 2+2?"))
	asst := anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("4")},
	}
	if err := tr.Record("user", mustJSON(t, user)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Record("assistant", mustJSON(t, asst)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}

	// Auto-resume must select this content-bearing session.
	if id, ok := session.MostRecent(cwd); !ok || id != sid {
		t.Fatalf("MostRecent = %q,%v; want %s,true", id, ok, sid)
	}

	entries, err := session.Read(session.ExistingPath(cwd, sid))
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := MessagesFromEntries(entries)
	if err != nil {
		t.Fatalf("MessagesFromEntries: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("reconstructed %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != anthropic.BetaMessageParamRoleUser || msgs[1].Role != anthropic.BetaMessageParamRoleAssistant {
		t.Errorf("roles = %v,%v; want user,assistant", msgs[0].Role, msgs[1].Role)
	}
}

// mustJSON marshals a message param to the raw JSON the recorder stores.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// messagesContain reports whether any message's serialized content includes sub.
func messagesContain(msgs []anthropic.BetaMessageParam, sub string) bool {
	for _, m := range msgs {
		b, _ := json.Marshal(m)
		if strings.Contains(string(b), sub) {
			return true
		}
	}
	return false
}
