package acp

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// Everything Klaudia needs to put to a human goes out as
// session/request_permission.
//
// ACP v1 has exactly one "ask the user to choose" primitive, and it is this
// one: a tool call to display plus a list of options to pick from. Klaudia has
// three such asks — permission for a tool, a host change, and the model's own
// AskUserQuestion / ExitPlanMode — and all three fit that shape, so all three
// use it rather than waiting for a protocol that distinguishes them. The same
// choice is what the other ACP agents make.
//
// What is lost is wording. A client renders request_permission as a permission
// dialog, so an ordinary multiple-choice question arrives looking like a
// security prompt. The mitigations are the title (the question itself), the
// content block (the options with their descriptions, which ACP's
// PermissionOption cannot carry), and _meta, which carries the structured form
// for a client that wants to render Klaudia's asks properly.

// metaPrefix namespaces Klaudia's _meta keys. ACP reserves _meta for exactly
// this and tells implementations not to assume anything about other people's
// keys, so the prefix is not optional.
const metaPrefix = "klaudia/"

// requester is the half of a session the asks need: post updates, and make a
// request that blocks for an answer.
type requester interface {
	notifier
	requestPermission(ctx context.Context, p requestPermissionParams) (permissionOutcome, error)
}

// approver resolves agent permission asks, including host changes, by putting
// them to the editor.
type approver struct {
	req       requester
	sessionID string
}

func (a *approver) Approve(ctx context.Context, r agent.ApprovalRequest) permission.Decision {
	call := toolCallStart{
		Kind:       "tool_call",
		ToolCallID: r.ToolUseID,
		Name:       r.ToolName,
		Title:      toolTitle(r.ToolName, nil),
		ToolKind:   toolKind(r.ToolName),
		Status:     statusPending,
	}
	if len(r.Input) > 0 {
		call.RawInput = r.Input
	}
	meta := map[string]any{}

	// A host change is not "allow Bash?" and must not be rendered as it. The
	// summary becomes the title, the reason and the scope become readable
	// content, and the structured form goes in _meta for a client that can do
	// better than a one-line dialog.
	if hc := hostgate.CardOf(r); hc != nil {
		call.Title = hostTitle(hc)
		call.ToolKind = kindOther
		if body := hostBody(hc); body != "" {
			call.Content = []toolCallContent{toolText(body)}
		}
		meta[metaPrefix+"hostChange"] = hc.Fields()
	} else {
		if r.Specifier != "" {
			call.Title = r.ToolName + " " + oneline(r.Specifier)
		}
		if r.Suggestion != "" {
			call.Content = []toolCallContent{toolText(r.Suggestion)}
		}
	}

	allowID := "allow"
	outcome, err := a.req.requestPermission(ctx, requestPermissionParams{
		SessionID: a.sessionID,
		ToolCall:  call,
		Options: []permissionOption{
			{OptionID: allowID, Name: approveLabel(r), Kind: optAllowOnce},
			{OptionID: "deny", Name: "Don't allow", Kind: optRejectOnce},
		},
		Meta: nonEmpty(meta),
	})
	if err != nil {
		// A client that has not implemented request_permission lands here, as
		// does a cancelled turn. Deny is the safe answer and the message says
		// which it was, because "the editor could not ask" and "you said no"
		// lead the model to different next steps.
		return permission.Decision{Behavior: permission.Deny, Message: "not approved: " + err.Error()}
	}
	if outcome.Outcome != "selected" || outcome.OptionID != allowID {
		return permission.Decision{Behavior: permission.Deny, Message: "the user did not approve it"}
	}
	// The call was pending while the user decided; say that it is running now,
	// so a client does not show an approved tool as still waiting.
	a.req.update(a.sessionID, toolCallPatch{
		Kind:       "tool_call_update",
		ToolCallID: r.ToolUseID,
		Status:     statusInProgress,
	})
	return permission.Decision{Behavior: permission.Allow}
}

func approveLabel(r agent.ApprovalRequest) string {
	if hostgate.CardOf(r) != nil {
		return "Allow this change to my machine"
	}
	return "Allow"
}

func hostTitle(hc *hostgate.HostChange) string {
	if s := oneline(hc.Summary); s != "" {
		return s
	}
	if hc.Hooks {
		return "Run this repository's hooks"
	}
	return "Change this machine"
}

// hostBody is the prose Klaudia's own host card shows, flattened for a dialog
// that only has room for text: why, what was detected, and what it covers.
func hostBody(hc *hostgate.HostChange) string {
	var b strings.Builder
	line := func(label, s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(s)
	}
	if hc.Drift {
		b.WriteString("This was not part of what you approved earlier.")
	}
	line("Why", hc.Reason)
	if !hc.Declared && len(hc.Effects) > 0 {
		var found []string
		seen := map[string]bool{}
		for _, e := range hc.Effects {
			d := e.Describe()
			if !seen[d] {
				seen[d] = true
				found = append(found, d)
			}
		}
		line("Detected", strings.Join(found, "; "))
	}
	line("Paths", strings.Join(hc.Paths, ", "))
	line("Services", strings.Join(hc.Services, ", "))
	line("Packages", strings.Join(hc.Packages, ", "))
	line("Commands", strings.Join(hc.Commands, "\n          "))
	line("Zone", hc.Zone.String())
	return b.String()
}

// asker puts AskUserQuestion (and MCP elicitation, which is pointed at the same
// Asker) to the editor.
type asker struct {
	req       requester
	sessionID string
}

func (a *asker) Ask(ctx context.Context, question string, options []tools.AskOption) (string, error) {
	if len(options) == 0 {
		return "", errors.New("no options to choose from")
	}
	opts := make([]permissionOption, 0, len(options))
	var body strings.Builder
	for i, o := range options {
		id := strconv.Itoa(i)
		opt := permissionOption{OptionID: id, Name: o.Label, Kind: optAllowOnce}
		if o.Description != "" {
			// PermissionOption has no description in v1, so it goes to _meta
			// for a client that reads it and to the content block for one that
			// does not. Dropping it would hide the text that makes the options
			// distinguishable.
			opt.Meta = map[string]any{metaPrefix + "description": o.Description}
		}
		opts = append(opts, opt)
		body.WriteString("• ")
		body.WriteString(o.Label)
		if o.Description != "" {
			body.WriteString(" — ")
			body.WriteString(o.Description)
		}
		body.WriteByte('\n')
	}

	outcome, err := a.req.requestPermission(ctx, requestPermissionParams{
		SessionID: a.sessionID,
		ToolCall: toolCallStart{
			Kind:       "tool_call",
			ToolCallID: "ask-" + strconv.FormatInt(nextAskID(), 10),
			Title:      oneline(question),
			ToolKind:   kindOther,
			Status:     statusPending,
			Content:    []toolCallContent{toolText(strings.TrimRight(body.String(), "\n"))},
		},
		Options: opts,
		Meta: map[string]any{
			metaPrefix + "ask": map[string]any{"question": question, "options": options},
		},
	})
	if err != nil {
		return "", err
	}
	if outcome.Outcome != "selected" {
		return "", errors.New("the user did not answer")
	}
	i, cerr := strconv.Atoi(outcome.OptionID)
	if cerr != nil || i < 0 || i >= len(options) {
		// Returning an error rather than guessing: a fabricated answer is
		// reported to the model as the user's choice, and it is not.
		return "", errors.New("the editor returned an option Klaudia did not offer")
	}
	return options[i].Label, nil
}

// planner handles ExitPlanMode.
type planner struct {
	req       requester
	sessionID string
	// approved, when set, is called after the user approves. Leaving plan mode
	// is the frontend's job — the tool only asks — and in a multi-session agent
	// the frontend has to know *which* session to take out of plan mode.
	approved func()
}

func (p *planner) ExitPlan(ctx context.Context, plan string) (bool, error) {
	const approveID = "approve"
	outcome, err := p.req.requestPermission(ctx, requestPermissionParams{
		SessionID: p.sessionID,
		ToolCall: toolCallStart{
			Kind:       "tool_call",
			ToolCallID: "plan-" + strconv.FormatInt(nextAskID(), 10),
			Title:      "Review the plan",
			ToolKind:   kindSwitchMode,
			Status:     statusPending,
			Content:    []toolCallContent{toolText(plan)},
		},
		Options: []permissionOption{
			{OptionID: approveID, Name: "Approve and start working", Kind: optAllowOnce},
			{OptionID: "keep_planning", Name: "Keep planning", Kind: optRejectOnce},
		},
		Meta: map[string]any{metaPrefix + "plan": plan},
	})
	if err != nil {
		return false, err
	}
	if outcome.Outcome != "selected" || outcome.OptionID != approveID {
		return false, nil
	}
	if p.approved != nil {
		p.approved()
	}
	return true, nil
}

func nonEmpty(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	return m
}
