// Package streamjson implements the bidirectional stream-json frontend: a
// persistent agent driven over stdin/stdout with newline-delimited JSON. This
// is the embedding channel used by SDKs and editor integrations (e.g. Zed/ACP
// can build on it) — no terminal required.
//
// Protocol (matching the JS reference):
//
//	in:  {"type":"user","message":{"role":"user","content":"..."}}
//	in:  {"type":"control_response","response":{"subtype":"success",
//	      "request_id":"<id>","response":{"behavior":"allow"|"deny",...}}}
//	out: assistant / tool_use / tool_result / compaction events (agent.Event)
//	out: {"type":"control_request","request_id":"<id>",
//	      "request":{"subtype":"can_use_tool","tool_name":"...","input":{...}}}
//	out: {"type":"result", ...}
package streamjson

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"

	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/permission"
)

// RunFunc runs one user turn to completion, seeded with prior conversation
// history, using the supplied approver and emitter. It returns the agent
// Result whose Messages field carries the updated history forward. The CLI
// provides this, wiring in the API client, tools, model, and permission context.
type RunFunc func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, approver agent.Approver, emit agent.Emitter) (agent.Result, error)

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

// permissionAnswer is the payload inside a can_use_tool control response.
type permissionAnswer struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message,omitempty"`
}

// Driver runs the stream-json protocol over a reader/writer pair.
type Driver struct {
	out     io.Writer
	mu      sync.Mutex // serializes writes to out
	pending sync.Map   // request_id -> chan permission.Decision
}

// NewDriver builds a Driver writing to w.
func NewDriver(w io.Writer) *Driver {
	return &Driver{out: w}
}

// Run reads messages from r until EOF, running the agent for each user message.
// Permission asks are emitted as control_request lines and block until the
// peer sends the matching control_response.
func (d *Driver) Run(ctx context.Context, r io.Reader, run RunFunc) error {
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

	approver := &controlApprover{driver: d}
	var history []anthropic.BetaMessageParam
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
			emit := func(ev agent.Event) { d.write(ev) }
			res, err := run(ctx, prompt, history, approver, emit)
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

// deliverControlResponse resolves a pending permission request.
func (d *Driver) deliverControlResponse(resp *controlResp) {
	if resp == nil || resp.RequestID == "" {
		return
	}
	ch, ok := d.pending.LoadAndDelete(resp.RequestID)
	if !ok {
		return
	}
	decision := permission.Decision{Behavior: permission.Deny, Message: "denied"}
	if resp.Subtype == "success" && len(resp.Response) > 0 {
		var ans permissionAnswer
		if json.Unmarshal(resp.Response, &ans) == nil {
			if ans.Behavior == "allow" {
				decision = permission.Decision{Behavior: permission.Allow}
			} else {
				decision = permission.Decision{Behavior: permission.Deny, Message: ans.Message}
			}
		}
	}
	ch.(chan permission.Decision) <- decision
}

// controlApprover emits a control_request and waits for the peer's answer.
type controlApprover struct {
	driver *Driver
}

func (a *controlApprover) Approve(ctx context.Context, req agent.ApprovalRequest) permission.Decision {
	id := uuid.NewString()
	ch := make(chan permission.Decision, 1)
	a.driver.pending.Store(id, ch)
	defer a.driver.pending.Delete(id)

	a.driver.write(map[string]any{
		"type":       "control_request",
		"request_id": id,
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": req.ToolName,
			"input":     json.RawMessage(req.Input),
		},
	})

	select {
	case <-ctx.Done():
		return permission.Decision{Behavior: permission.Deny, Message: "cancelled"}
	case dec := <-ch:
		return dec
	}
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
	if err != nil {
		m["subtype"] = "error_during_execution"
		m["result"] = "Error: " + api.FriendlyError(err)
	}
	return m
}
