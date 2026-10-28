package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// pressPermissionKey answers a pending permission ask with one key and returns
// what the agent received.
func pressPermissionKey(t *testing.T, m *Model, key rune) permission.Decision {
	t.Helper()
	reply := make(chan permission.Decision, 1)
	m.pending = reply
	m.onKey(tea.KeyPressMsg{Code: key, Text: string(key)})
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
// change to the machine. (PR #85.)
func TestSomethingElseRedirectsAnOrdinaryPermissionAsk(t *testing.T) {
	m := newTestModel()
	m.setState(stateAwaitingPermission)
	m.pendingReq = agent.ApprovalRequest{ToolName: "Bash", Specifier: "rm -rf build"}

	prompt := m.permissionPrompt()
	for _, want := range []string{"(s)omething else", "esc cancels turn"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not offer %q: %s", want, prompt)
		}
	}
	d := pressPermissionKey(t, m, 's')
	if d.Behavior != permission.Deny || !strings.Contains(d.Message, "redirecting") || !strings.Contains(d.Message, "wait for that instruction") {
		t.Errorf("something else = %+v, want a deny that says an instruction is coming", d)
	}
	if strings.Contains(d.Message, "machine") {
		t.Errorf("an ordinary ask is described as a host change: %q", d.Message)
	}
	if m.state != stateRunning || m.redirect {
		t.Errorf("state %v, redirect %v; want running and the flag cleared", m.state, m.redirect)
	}
	if out := visibleText(m.transcript.String()); !strings.Contains(out, "instead") {
		t.Errorf("echo does not invite an instruction:\n%s", out)
	}
}

// A plain "no" stays a plain refusal.
func TestNoOnAPermissionAskStaysAPlainRefusal(t *testing.T) {
	m := newTestModel()
	m.setState(stateAwaitingPermission)
	m.pendingReq = agent.ApprovalRequest{ToolName: "Bash", Specifier: "rm -rf build"}
	if d := pressPermissionKey(t, m, 'n'); d.Behavior != permission.Deny || d.Message != "denied by user" {
		t.Errorf("n = %+v, want a plain denial", d)
	}
	if out := visibleText(m.transcript.String()); strings.Contains(out, "instead") {
		t.Errorf("a plain no reads as a redirect:\n%s", out)
	}
}

// A host change keeps its own wording; a repository-config card, which has no
// task to redirect, ignores "s".
func TestSomethingElseOnHostCards(t *testing.T) {
	m := newTestModel()
	m.setState(stateAwaitingPermission)
	m.pendingReq = agent.ApprovalRequest{ToolName: "RequestHostChange", Detail: &hostgate.HostChange{}}
	if p := m.permissionPrompt(); !strings.Contains(p, "esc cancels turn") {
		t.Errorf("host prompt does not name esc: %s", p)
	}
	if d := pressPermissionKey(t, m, 's'); d.Behavior != permission.Deny || !strings.Contains(d.Message, "change to their machine") {
		t.Errorf("host redirect = %+v", d)
	}

	m = newTestModel()
	m.setState(stateAwaitingPermission)
	m.pendingReq = agent.ApprovalRequest{ToolName: "hooks", Detail: &hostgate.HostChange{RepoConfig: "hooks"}}
	reply := make(chan permission.Decision, 1)
	m.pending = reply
	m.onKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if len(reply) != 0 || m.state != stateAwaitingPermission {
		t.Error(`"s" answered a repository-config card, which does not offer it`)
	}
}
