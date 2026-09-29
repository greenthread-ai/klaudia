package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

// pressPermissionKey answers a pending permission ask with one key and returns
// what the agent received.
func pressPermissionKey(t *testing.T, m *Model, key string) permission.Decision {
	t.Helper()
	reply := make(chan permission.Decision, 1)
	m.pending = reply
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	select {
	case d := <-reply:
		return d
	default:
		t.Fatalf("key %q did not answer the ask", key)
		return permission.Decision{}
	}
}

// "Something else" is offered on every permission ask, not only host changes:
// declining an ordinary tool call is as often "not like that" as declining a
// change to the machine. The model is told to wait for the instruction rather
// than route around the refusal, and the echoed line invites that instruction.
func TestSomethingElseRedirectsAnOrdinaryPermissionAsk(t *testing.T) {
	m := newTestModel()
	m.resize(200, 40)
	m.setState(stateAwaitingPermission)
	m.pendingReq = agent.ApprovalRequest{ToolName: "Bash", Specifier: "rm -rf build"}

	view := visibleText(m.View())
	for _, want := range []string{"(s)omething else", "esc cancels turn"} {
		if !strings.Contains(view, want) {
			t.Errorf("prompt does not offer %q:\n%s", want, view)
		}
	}

	d := pressPermissionKey(t, m, "s")
	if d.Behavior != permission.Deny {
		t.Fatalf("something else should deny the call, got %v", d.Behavior)
	}
	if !strings.Contains(d.Message, "redirecting") || !strings.Contains(d.Message, "wait for that instruction") {
		t.Errorf("model is not told an instruction is coming: %q", d.Message)
	}
	if strings.Contains(d.Message, "machine") {
		t.Errorf("an ordinary ask is described as a host change: %q", d.Message)
	}
	if m.state != stateRunning {
		t.Errorf("the turn should stay alive for the redirect, state = %v", m.state)
	}
	if m.redirect {
		t.Error("the redirect flag should be cleared once the answer is echoed")
	}
	if out := visibleText(m.transcript.String()); !strings.Contains(out, "instead") {
		t.Errorf("echo does not invite an instruction:\n%s", out)
	}
}

// A plain "no" on an ordinary ask stays a plain refusal.
func TestNoOnAPermissionAskStaysAPlainRefusal(t *testing.T) {
	m := newTestModel()
	m.setState(stateAwaitingPermission)
	m.pendingReq = agent.ApprovalRequest{ToolName: "Bash", Specifier: "rm -rf build"}
	d := pressPermissionKey(t, m, "n")
	if d.Behavior != permission.Deny || d.Message != "denied by user" {
		t.Errorf("n = %+v, want a plain denial", d)
	}
	if out := visibleText(m.transcript.String()); strings.Contains(out, "instead") {
		t.Errorf("a plain no reads as a redirect:\n%s", out)
	}
}

// The host-change redirect keeps its own wording, and its prompt names Esc too.
func TestSomethingElseOnAHostChange(t *testing.T) {
	m := newTestModel()
	m.resize(200, 40)
	m.setState(stateAwaitingPermission)
	m.pendingReq = agent.ApprovalRequest{
		ToolName:   "RequestHostChange",
		HostChange: &agent.HostChange{Summary: "install nginx"},
	}
	if view := visibleText(m.View()); !strings.Contains(view, "esc cancels turn") {
		t.Errorf("host prompt does not name esc:\n%s", view)
	}
	d := pressPermissionKey(t, m, "s")
	if d.Behavior != permission.Deny ||
		!strings.Contains(d.Message, "change to their machine") ||
		!strings.Contains(d.Message, "redirecting") {
		t.Errorf("host redirect = %+v", d)
	}
}
