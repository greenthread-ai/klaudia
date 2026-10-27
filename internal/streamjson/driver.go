// Package streamjson implements the bidirectional stream-json frontend: a
// persistent agent driven over stdin/stdout with newline-delimited JSON. This
// is the embedding channel used by SDKs and editor integrations — no terminal
// required. (An ACP editor integration speaks ACP instead; see internal/acp.)
//
// Protocol (matching the JS reference):
//
//	in:  {"type":"user","message":{"role":"user","content":"..."}}
//	in:  {"type":"control_response","response":{"subtype":"success",
//	      "request_id":"<id>","response":{...}}}
//	out: assistant / tool_use / tool_result / compaction / notice /
//	     permission_mode / plan events (agent.Event)
//	out: {"type":"plan","plan":{"items":[{"content","status","active_form"}],
//	      "approved":bool,"path","rev"}} — the whole list, sent whenever it
//	      changes; "items":[] is a cleared plan. A Klaudia extension.
//	out: {"type":"control_request","request_id":"<id>","request":{...}}
//	out: {"type":"result", ...}
//
// Three control_request subtypes, all answered on the same request_id channel:
//
//	can_use_tool: {"tool_name","input","tool_use_id","specifier","suggestion",
//	               "host_change":{...}} -> {"behavior":"allow"|"deny","message"}
//	ask_user:     {"question","options":[{"label","description"}]} -> {"label"}
//	exit_plan:    {"plan"} -> {"approved":bool,"message"}
//
// can_use_tool is the one the JS reference defines. ask_user and exit_plan are
// Klaudia extensions, added because without them AskUserQuestion and
// ExitPlanMode were dead over this transport: the CLI had no wire form to put
// them in, so it passed no Asker and no Planner and the model was told there was
// nobody to ask — on the channel whose whole purpose is a client with a user in
// front of it. A peer that does not implement them should answer with an error,
// which is read as "cancelled" and leaves the model where it was before.
package streamjson

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// RunFunc runs one user turn to completion. It is the shared frontend contract
// — see agent.Turn.
type RunFunc = agent.RunFunc

// inMessage is a decoded stdin line.
type inMessage struct {
	Type     string          `json:"type"`
	Message  json.RawMessage `json:"message,omitempty"`
	Response *controlResp    `json:"response,omitempty"`
}

type controlResp struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// ok reports whether the peer answered rather than failed. A missing subtype is
// treated as success so that a peer which only ever echoes request_id and a
// payload still works; an explicit error, or an unparseable payload, is not.
func (r *controlResp) ok() bool {
	return r != nil && r.Error == "" && (r.Subtype == "" || r.Subtype == "success")
}

// permissionAnswer is the payload inside a can_use_tool control response.
type permissionAnswer struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message,omitempty"`
}

// askAnswer is the payload inside an ask_user control response.
type askAnswer struct {
	Label string `json:"label"`
}

// planAnswer is the payload inside an exit_plan control response.
type planAnswer struct {
	Approved bool   `json:"approved"`
	Message  string `json:"message,omitempty"`
}

// Driver runs the stream-json protocol over a reader/writer pair.
type Driver struct {
	out     io.Writer
	mu      sync.Mutex // serializes writes to out
	pending sync.Map   // request_id -> chan *controlResp
}

// NewDriver builds a Driver writing to w.
func NewDriver(w io.Writer) *Driver {
	return &Driver{out: w}
}

// Run reads messages from r until EOF, running the agent for each user message.
// history seeds a resumed conversation (may be nil). Permission asks, questions
// and plan approvals are emitted as control_request lines and block until the
// peer sends the matching control_response.
func (d *Driver) Run(ctx context.Context, r io.Reader, history []anthropic.BetaMessageParam, run RunFunc) error {
	lines := make(chan inMessage, 8)

	// Reader goroutine: route control responses to waiters, user messages to
	// the run loop.
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			var m inMessage
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				continue
			}
			if m.Type == "control_response" {
				d.deliverControlResponse(m.Response)
				continue
			}
			lines <- m
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m, ok := <-lines:
			if !ok {
				return nil // stdin closed
			}
			if m.Type != "user" {
				continue
			}
			prompt := decodeUserContent(m.Message)
			if prompt == "" {
				continue
			}
			res, err := run(ctx, agent.Turn{
				Prompt:   prompt,
				History:  history,
				Emit:     func(ev agent.Event) { d.write(ev) },
				Approver: &controlApprover{driver: d},
				Asker:    &controlAsker{driver: d},
				Planner:  &controlPlanner{driver: d},
			})
			if res.Messages != nil {
				history = res.Messages // carry conversation forward
			}
			d.write(resultEvent(res, err))
		}
	}
}

// write emits one JSON line, serialized against concurrent writers.
func (d *Driver) write(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, _ = d.out.Write(b)
	_, _ = d.out.Write([]byte("\n"))
}

// deliverControlResponse resolves a pending control request.
func (d *Driver) deliverControlResponse(resp *controlResp) {
	if resp == nil || resp.RequestID == "" {
		return
	}
	ch, ok := d.pending.LoadAndDelete(resp.RequestID)
	if !ok {
		return
	}
	ch.(chan *controlResp) <- resp
}

// errNoAnswer is returned when the turn was cancelled before the peer replied.
var errNoAnswer = errors.New("cancelled")

// request emits one control_request and blocks until the peer answers it or ctx
// is cancelled. The payload's "subtype" selects the request kind.
func (d *Driver) request(ctx context.Context, payload map[string]any) (*controlResp, error) {
	id := uuid.NewString()
	ch := make(chan *controlResp, 1)
	d.pending.Store(id, ch)
	defer d.pending.Delete(id)

	d.write(map[string]any{
		"type":       "control_request",
		"request_id": id,
		"request":    payload,
	})

	select {
	case <-ctx.Done():
		return nil, errNoAnswer
	case resp := <-ch:
		return resp, nil
	}
}

// controlApprover emits a can_use_tool request and waits for the peer's answer.
type controlApprover struct {
	driver *Driver
}

func (a *controlApprover) Approve(ctx context.Context, req agent.ApprovalRequest) permission.Decision {
	payload := map[string]any{
		"subtype":   "can_use_tool",
		"tool_name": req.ToolName,
		"input":     json.RawMessage(req.Input),
	}
	// The fields below used to be dropped on the floor, which made every ask
	// look the same. The host-change one is the damaging omission: "allow Bash?"
	// is the wrong question to put to someone about `systemctl restart nginx`,
	// and a peer that cannot tell the two apart cannot render the right card.
	if req.ToolUseID != "" {
		payload["tool_use_id"] = req.ToolUseID
	}
	if req.Specifier != "" {
		payload["specifier"] = req.Specifier
	}
	if req.Suggestion != "" {
		payload["suggestion"] = req.Suggestion
	}
	if hostgate.CardOf(req) != nil {
		// Fields lives on hostgate.HostChange because ACP needs the same
		// rendering, and two copies of "which fields to omit" would drift.
		payload["host_change"] = hostgate.CardOf(req).Fields()
	}

	resp, err := a.driver.request(ctx, payload)
	if err != nil {
		return permission.Decision{Behavior: permission.Deny, Message: "cancelled"}
	}
	decision := permission.Decision{Behavior: permission.Deny, Message: "denied"}
	if resp.ok() && len(resp.Response) > 0 {
		var ans permissionAnswer
		if json.Unmarshal(resp.Response, &ans) == nil {
			if ans.Behavior == "allow" {
				decision = permission.Decision{Behavior: permission.Allow}
			} else {
				decision = permission.Decision{Behavior: permission.Deny, Message: ans.Message}
			}
		}
	}
	return decision
}

// controlAsker emits an ask_user request for AskUserQuestion and for MCP
// elicitation, which is pointed at the same Asker.
type controlAsker struct {
	driver *Driver
}

func (a *controlAsker) Ask(ctx context.Context, question string, options []tools.AskOption) (string, error) {
	opts := make([]map[string]string, 0, len(options))
	for _, o := range options {
		entry := map[string]string{"label": o.Label}
		if o.Description != "" {
			entry["description"] = o.Description
		}
		opts = append(opts, entry)
	}
	resp, err := a.driver.request(ctx, map[string]any{
		"subtype":  "ask_user",
		"question": question,
		"options":  opts,
	})
	if err != nil {
		return "", err
	}
	if !resp.ok() {
		// A peer that has not implemented ask_user lands here. Returning an
		// error rather than picking the first option matters: the model is told
		// the question could not be put, and carries on without inventing an
		// answer the user never gave.
		msg := resp.Error
		if msg == "" {
			msg = "the client could not ask the user"
		}
		return "", errors.New(msg)
	}
	var ans askAnswer
	if err := json.Unmarshal(resp.Response, &ans); err != nil || ans.Label == "" {
		return "", errors.New("the client returned no answer")
	}
	return ans.Label, nil
}

// controlPlanner emits an exit_plan request for ExitPlanMode.
type controlPlanner struct {
	driver *Driver
}

func (p *controlPlanner) ExitPlan(ctx context.Context, plan string) (bool, error) {
	resp, err := p.driver.request(ctx, map[string]any{
		"subtype": "exit_plan",
		"plan":    plan,
	})
	if err != nil {
		return false, err
	}
	if !resp.ok() {
		msg := resp.Error
		if msg == "" {
			msg = "the client could not review the plan"
		}
		return false, errors.New(msg)
	}
	var ans planAnswer
	if err := json.Unmarshal(resp.Response, &ans); err != nil {
		return false, errors.New("the client returned no decision")
	}
	if !ans.Approved && ans.Message != "" {
		return false, errors.New(ans.Message)
	}
	return ans.Approved, nil
}

// decodeUserContent extracts the text of a user message whose content is either
// a string or an array of content blocks.
func decodeUserContent(raw json.RawMessage) string {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(msg.Content, &blocks) == nil {
		var out string
		for _, b := range blocks {
			if b.Type == "text" {
				out += b.Text
			}
		}
		return out
	}
	return ""
}

// resultEvent builds the terminal result line for a turn.
func resultEvent(res agent.Result, err error) map[string]any {
	m := map[string]any{
		"type":        "result",
		"subtype":     "success",
		"is_error":    err != nil,
		"num_turns":   res.NumTurns,
		"result":      res.Text,
		"stop_reason": res.StopReason,
	}
	switch {
	case err != nil:
		m["subtype"] = "error_during_execution"
		m["result"] = "Error: " + api.FriendlyError(err)
	case agent.TurnEndedEmpty(res.Text):
		// A refusal or a limit completes cleanly at the protocol level and says
		// nothing. Reporting subtype "success" with an empty result let a
		// pipeline treat a refusal as a finished task — the same bug the
		// headless path already fixes, which this one had not inherited.
		if note := agent.TurnNote(res.StopReason, false); note != "" {
			m["subtype"] = res.StopReason
			m["is_error"] = true
			m["result"] = note
		}
	}
	return m
}
