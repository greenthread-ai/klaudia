package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// answerAsk hands the answer back to the waiting tool and resumes the turn. The
// nil check matters: a turn interrupted mid-question clears askReply, and a send
// on a nil channel blocks forever — the UI would simply stop.
func (m *Model) answerAsk(answer string) {
	if m.askReply != nil {
		m.askReply <- answer
		m.askReply = nil
	}
	m.appendLine(toolStyle.Render("  → " + answer))
	m.setState(stateRunning)
}

// beginOtherAnswer switches from the numbered list to free-text entry, seeding
// the box with whatever the user already typed so the first keystroke isn't
// swallowed.
func (m *Model) beginOtherAnswer(seed string) tea.Cmd {
	m.setState(stateAnsweringOther)
	m.input.Reset()
	if seed != "" {
		m.input.InsertString(seed)
	}
	m.input.Focus()
	m.syncInputHeight()
	return textarea.Blink
}

// permissionSummary returns a one-line description of the pending action.
func (m *Model) permissionSummary(req agent.ApprovalRequest) string {
	label := req.ToolName
	if req.Specifier != "" {
		label += " (" + req.Specifier + ")"
	}
	switch req.ToolName {
	case "Edit":
		if target := firstNonEmpty(stringField(req.Input, "file_path"), req.Specifier); target != "" {
			return "edit " + target
		}
	case "Write":
		if target := firstNonEmpty(stringField(req.Input, "file_path"), req.Specifier); target != "" {
			return "write " + target
		}
	case "NotebookEdit":
		if target := firstNonEmpty(stringField(req.Input, "notebook_path"), req.Specifier); target != "" {
			return "edit notebook " + target
		}
	case "Bash":
		if desc := stringField(req.Input, "description"); desc != "" {
			return "run command — " + desc
		}
		if target := firstNonEmpty(req.Specifier, stringField(req.Input, "command")); target != "" {
			return "run command " + target
		}
	}
	return label
}

func (m *Model) permissionPrompt() string {
	if hc := m.pendingReq.HostChange; hc != nil {
		return hostPrompt(hc)
	}
	// No "always". Standing rules are gone: one in a config demoted the next
	// session to a mode that asked about everything, so the answer offered to
	// stop the prompting was the thing that caused more of it. Plan mode
	// reaches this branch for a command its classifier cannot place (see
	// trust.InspectCommand); so can an MCP server or an embedding frontend.
	return fmt.Sprintf("Allow %s? (y)es / (n)o", m.permissionSummary(m.pendingReq))
}

func permissionDetail(req agent.ApprovalRequest) string {
	switch req.ToolName {
	case "Edit":
		return editPermissionDetail(req.Input)
	case "Write":
		if path := stringField(req.Input, "file_path"); path != "" {
			return "file: " + path
		}
	case "NotebookEdit":
		if path := stringField(req.Input, "notebook_path"); path != "" {
			return "notebook: " + path
		}
	case "Bash":
		if cmd := stringField(req.Input, "command"); cmd != "" {
			// The reason goes with the command. Plan mode asks about a command
			// it cannot classify, and returning early here hid the one line
			// saying so — the prompt read as an arbitrary refusal-in-waiting
			// rather than "nothing here classifies cd".
			detail := "command: " + oneline(cmd, 220)
			if req.Suggestion != "" {
				detail += "\n  why: " + oneline(req.Suggestion, 220)
			}
			return detail
		}
	}
	if req.Suggestion != "" {
		return req.Suggestion
	}
	return ""
}

func editPermissionDetail(raw json.RawMessage) string {
	path := stringField(raw, "file_path")
	oldText := stringField(raw, "old_string")
	newText := stringField(raw, "new_string")
	var parts []string
	if path != "" {
		parts = append(parts, "file: "+path)
	}
	if oldText != "" || newText != "" {
		parts = append(parts, fmt.Sprintf("replace %q → %q", oneline(oldText, 80), oneline(newText, 80)))
	}
	return strings.Join(parts, "\n  ")
}

func stringField(raw json.RawMessage, name string) string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	v, ok := fields[name].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// answer resolves the pending permission ask.
func (m *Model) answer(d permission.Decision) {
	if m.pending != nil {
		m.pending <- d
		m.pending = nil
	}
	verb := "allowed"
	if d.Behavior != permission.Allow {
		verb = "denied"
	}
	// For a host change, say what the answer reached rather than just that one
	// was given. "allowed" tells the user nothing about how far it went.
	if hc := m.pendingReq.HostChange; hc != nil {
		verb = hostAnswerLine(hc, d.Behavior == permission.Allow, m.hostRedirect)
	}
	m.hostRedirect = false
	m.appendLine(toolStyle.Render("  → " + verb))
	m.setState(stateRunning)
}

// uiApprover implements agent.Approver by asking the UI and blocking for the
// user's decision.
type uiApprover struct {
	events chan tea.Msg
}

func (a *uiApprover) Approve(ctx context.Context, req agent.ApprovalRequest) permission.Decision {
	reply := make(chan permission.Decision, 1)
	a.events <- permissionMsg{req: req, reply: reply}
	select {
	case <-ctx.Done():
		return permission.Decision{Behavior: permission.Deny, Message: "cancelled"}
	case d := <-reply:
		return d
	}
}

// uiAsker implements tools.Asker by prompting the user in the UI and blocking
// for their choice.
type uiAsker struct {
	events chan tea.Msg
}

func (a *uiAsker) Ask(ctx context.Context, question string, options []tools.AskOption) (string, error) {
	reply := make(chan string, 1)
	a.events <- askMsg{question: question, options: options, reply: reply}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case choice := <-reply:
		return choice, nil
	}
}

// uiPlanner implements tools.Planner by showing the plan and blocking for the
// user's approval.
type uiPlanner struct {
	events chan tea.Msg
}

func (p *uiPlanner) ExitPlan(ctx context.Context, plan string) (bool, error) {
	reply := make(chan bool, 1)
	p.events <- planMsg{plan: plan, reply: reply}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case ok := <-reply:
		return ok, nil
	}
}
