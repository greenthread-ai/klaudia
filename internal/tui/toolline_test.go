package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	m.flushReads()
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
	raw, _ := json.Marshal(in)
	m.update(permissionMsg{req: agent.ApprovalRequest{ToolName: "Edit", Input: raw}, reply: make(chan permission.Decision, 1)})
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "⚙ Edit a.go") {
		t.Fatalf("the card came without its call:\n%s", out)
	} else if strings.Contains(out, "replace \"") {
		t.Errorf("the change was printed twice, once above the card and once in it:\n%s", out)
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

// A batch of reads is one line, written before whatever follows it, and
// visible in the live region while it is held.
func TestReadRunIsOneLine(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	m.sess.CWD = "/proj"
	m.setState(stateRunning)
	for i, f := range []string{"main.go", "main_test.go", "go.mod"} {
		call(m, string(rune('a'+i)), "Read", map[string]any{"file_path": "/proj/" + f}, "1\tx", false)
	}
	if !strings.Contains(stripANSI(m.View()), "✓ Read 3 files · main.go, main_test.go, go.mod") {
		t.Errorf("held reads not shown live:\n%s", stripANSI(m.View()))
	}
	call(m, "d", "Bash", map[string]any{"command": "go test ./..."}, "ok", false)
	out := stripANSI(m.transcript.String())
	want := "✓ Read 3 files · main.go, main_test.go, go.mod\n✓ Bash go test ./..."
	if !strings.HasPrefix(out, want) {
		t.Errorf("got:\n%s\nwant prefix:\n%s", out, want)
	}
}

// The live line names the running call with a space between tool and input.
func TestActiveToolLabelIsSpaced(t *testing.T) {
	m := newTestModel()
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: "1", ToolName: "BashOutput", Input: map[string]any{"bash_id": "bash_1"}})
	if m.activeToolName != "BashOutput bash_1" {
		t.Errorf("activeToolName = %q", m.activeToolName)
	}
}

// Starting a background job is one line; the how-to text is for the model.
func TestBackgroundStartIsOneLine(t *testing.T) {
	m := newTestModel()
	in := map[string]any{"command": "./provision.sh", "run_in_background": true}
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: "1", ToolName: "Bash", Input: in})
	m.renderEvent(agent.Event{Type: "tool_result", ToolUseID: "1", ToolName: "Bash",
		Content: "Started job provision.sh (bash_1). Read its output with BashOutput…",
		Data:    map[string]any{"job": "provision.sh", "job_id": "bash_1"}})
	if out := strings.TrimSpace(stripANSI(m.transcript.String())); out != "✓ Bash ./provision.sh · started as provision.sh" {
		t.Errorf("got %q", out)
	}
}

// tok/s is the model's rate, not the turn's: tool time is taken out, once
// even for overlapping calls.
func TestToolTimeIsCountedOnce(t *testing.T) {
	m := newTestModel()
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: "a", ToolName: "Read", Input: map[string]any{"file_path": "x"}})
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: "b", ToolName: "Read", Input: map[string]any{"file_path": "y"}})
	m.toolsSince = time.Now().Add(-2 * time.Second) // both in flight for 2s
	m.renderEvent(agent.Event{Type: "tool_result", ToolUseID: "a", ToolName: "Read", Content: "1\tx"})
	m.renderEvent(agent.Event{Type: "tool_result", ToolUseID: "b", ToolName: "Read", Content: "1\ty"})
	if d := m.takeToolTime(); d < 2*time.Second || d > 3*time.Second {
		t.Errorf("tool time = %s, want ~2s", d)
	}
	if m.takeToolTime() != 0 {
		t.Error("tool time was not reset for the next turn")
	}
	if got := throughput(300, 2*time.Second); !strings.Contains(got, "150 tok/s") {
		t.Errorf("throughput = %q", got)
	}
	if got := throughput(300, 500*time.Millisecond); strings.Contains(got, "tok/s") {
		t.Errorf("a sub-second rate was shown: %q", got)
	}
}
