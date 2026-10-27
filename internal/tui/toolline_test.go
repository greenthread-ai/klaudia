package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

func call(m *Model, id, name string, input map[string]any, result string, failed bool) {
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: id, ToolName: name, Input: input})
	m.renderEvent(agent.Event{Type: "tool_result", ToolUseID: id, ToolName: name, Input: input, Content: result, IsError: failed})
}

// A Read used to cost a header line, a result line, and eight lines of the
// file. It is one line now, and the file is a /last away.
func TestReadIsOneLine(t *testing.T) {
	m := newTestModel()
	m.sess.CWD = "/proj"
	call(m, "1", "Read", map[string]any{"file_path": "/proj/main.go"}, "1\tpackage main\n2\t\n3\tfunc main() {}", false)
	out := strings.TrimSpace(stripANSI(m.transcript.String()))
	if out != "✓ Read main.go · 3 lines" {
		t.Errorf("got %q", out)
	}
}

func TestEditShowsItsHunkUnderOneLine(t *testing.T) {
	m := newTestModel()
	call(m, "1", "Edit", map[string]any{"file_path": "a.go", "old_string": "old()", "new_string": "fresh()"}, "ok", false)
	out := stripANSI(m.transcript.String())
	if !strings.HasPrefix(out, "✓ Edit a.go") || !strings.Contains(out, "old()") || !strings.Contains(out, "fresh()") {
		t.Errorf("got:\n%s", out)
	}
	if strings.Contains(out, "⚙") {
		t.Errorf("a separate start line was written:\n%s", out)
	}
}

func TestFailedCallShowsItsError(t *testing.T) {
	m := newTestModel()
	call(m, "1", "Read", map[string]any{"file_path": "ghost.go"}, "File does not exist: ghost.go", true)
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "✗ Read ghost.go") || !strings.Contains(out, "File does not exist") {
		t.Errorf("got:\n%s", out)
	}
}

// A call that stops for approval has its line (and diff) written before the
// card, so the question has its context; the result then goes under it.
func TestApprovalShowsTheCallFirst(t *testing.T) {
	m := newTestModel()
	in := map[string]any{"file_path": "a.go", "old_string": "x", "new_string": "y"}
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: "1", ToolName: "Edit", Input: in})
	m.update(permissionMsg{req: agent.ApprovalRequest{ToolName: "Edit"}, reply: make(chan permission.Decision, 1)})
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "⚙ Edit a.go") {
		t.Fatalf("the card came without its call:\n%s", out)
	}
	m.renderEvent(agent.Event{Type: "tool_result", ToolUseID: "1", ToolName: "Edit", Input: in, Content: "ok"})
	out := stripANSI(m.transcript.String())
	if strings.Count(out, "Edit a.go") != 1 || !strings.Contains(out, "  ✓ Edit") {
		t.Errorf("the result should sit under the early line, not repeat it:\n%s", out)
	}
}

// Calls in flight when the turn stops still get a line.
func TestInterruptedCallsAreNotLost(t *testing.T) {
	m := newTestModel()
	m.setState(stateRunning)
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: "1", ToolName: "Bash", Input: map[string]any{"command": "sleep 60"}})
	m.update(doneMsg{err: context.Canceled})
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "⊘ Bash sleep 60 · stopped") {
		t.Errorf("got:\n%s", out)
	}
}
