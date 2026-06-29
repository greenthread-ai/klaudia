// WebSocket session client. Opens a connection to ai-console's
// /api/coding/agent/ws, fans agent events out to the server, and reads
// user input (messages, permission allow/deny, slash commands) back
// from the server.
//
// One Session per klaudia process. The TUI calls:
//
//   sess, _ := remotecontrol.Open(ctx, cfg)
//   defer sess.Close()
//   sess.SendMeta(...)   // optional: cwd / git_branch / model
//   wrappedEmit := sess.WrapEmitter(originalEmit)
//   approver := sess.Approver(originalApprover)   // falls back to original on disconnect
//   inputs := sess.Inputs()   // <-chan Input, drained by the TUI's input pump
//
// Reconnect with the same SessionID is supported by passing the previous
// session id via Config.SessionID — the server will reuse the row.

package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/permission"
)

// SessionConfig configures a single WS session.
type SessionConfig struct {
	BaseURL   string // e.g. "https://ai-console.local"
	Secret    string // bearer token from device-code login
	SessionID string // optional resume id; empty = server creates one
	Title     string // optional title hint for the server's session row
	HTTP      *http.Client
}

// Session is the live WebSocket connection.
type Session struct {
	cfg     SessionConfig
	conn    *websocket.Conn
	sessionID string

	// Channels & state
	inputCh  chan Input
	writeCh  chan EventEnvelope
	closeOnce sync.Once
	closed    chan struct{}
	closeErr  atomic.Pointer[error]

	// Approval bridge: pending requests by request_id. The Approver
	// blocks the agent loop on a per-request channel; an inbound
	// {kind:permission} resolves it.
	apMu        sync.Mutex
	approvals   map[string]chan permission.Decision
}

// Open dials the WS and returns a running session. Caller MUST call
// Close to release goroutines and the network connection.
func Open(ctx context.Context, cfg SessionConfig) (*Session, error) {
	if cfg.BaseURL == "" || cfg.Secret == "" {
		return nil, errors.New("remotecontrol: BaseURL and Secret required")
	}
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/coding/agent/ws"

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second

	header := http.Header{
		"Authorization": {"Bearer " + cfg.Secret},
	}
	if cfg.SessionID != "" {
		header.Set("X-Klaudia-Session", cfg.SessionID)
	}
	if cfg.Title != "" {
		header.Set("X-Klaudia-Title", cfg.Title)
	}

	conn, resp, err := dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial %s: %w (HTTP %s)", u.String(), err, resp.Status)
		}
		return nil, fmt.Errorf("dial %s: %w", u.String(), err)
	}
	sid := cfg.SessionID
	if resp != nil {
		if h := resp.Header.Get("X-Klaudia-Session"); h != "" {
			sid = h
		}
	}
	if sid == "" {
		// Pre-pick one so the caller always has something stable.
		sid = uuid.NewString()
	}

	s := &Session{
		cfg:       cfg,
		conn:      conn,
		sessionID: sid,
		inputCh:   make(chan Input, 16),
		writeCh:   make(chan EventEnvelope, 256),
		closed:    make(chan struct{}),
		approvals: make(map[string]chan permission.Decision),
	}
	go s.readLoop()
	go s.writeLoop()
	return s, nil
}

// SessionID is the server-assigned (or caller-resumed) session id.
func (s *Session) SessionID() string { return s.sessionID }

// Inputs is the read-only channel of user inputs that arrived from the
// browser/mobile. The TUI's input pump drains this and injects messages
// or slash commands as if the local user typed them.
func (s *Session) Inputs() <-chan Input { return s.inputCh }

// SendMeta publishes a session.meta event. Server uses this to update
// the row columns and to surface cwd / git_branch / model in the UI.
func (s *Session) SendMeta(meta SessionMetaPayload, title string) {
	s.enqueue(EventEnvelope{
		Type:           "event",
		Kind:           KindSessionMeta,
		Cwd:            meta.Cwd,
		GitBranch:      meta.GitBranch,
		Model:          meta.Model,
		PermissionMode: meta.PermissionMode,
		Title:          title,
	})
}

// SendStatus emits a status event (thinking / tool_running / idle).
func (s *Session) SendStatus(state string) {
	s.enqueue(EventEnvelope{
		Type:    "event",
		Kind:    KindStatus,
		Payload: map[string]any{"state": state},
	})
}

// SendUserMessage echoes a user-typed message so the UI sees its own
// input even when typed in the klaudia TUI rather than the browser.
func (s *Session) SendUserMessage(text string) {
	s.enqueue(EventEnvelope{
		Type:    "event",
		Kind:    KindMessageUser,
		Payload: map[string]any{"text": text},
	})
}

// SendTurnDone emits a turn.done event.
func (s *Session) SendTurnDone(stopReason string) {
	s.enqueue(EventEnvelope{
		Type:    "event",
		Kind:    KindTurnDone,
		Payload: map[string]any{"stop_reason": stopReason},
	})
}

// WrapEmitter returns an Emitter that forwards events to the WS in
// addition to the underlying emitter. Safe to call with a nil base.
func (s *Session) WrapEmitter(base agent.Emitter) agent.Emitter {
	return func(ev agent.Event) {
		if base != nil {
			base(ev)
		}
		env := EventEnvelope{Type: "event"}
		switch ev.Type {
		case "assistant":
			env.Kind = KindAssistantDelta
			env.Payload = map[string]any{"text": ev.Text}
		case "tool_use":
			env.Kind = KindToolUse
			env.Payload = map[string]any{
				"tool_use_id": ev.ToolUseID,
				"tool_name":   ev.ToolName,
				"input":       ev.Input,
			}
		case "tool_result":
			env.Kind = KindToolResult
			env.Payload = map[string]any{
				"tool_use_id": ev.ToolUseID,
				"content":     ev.Content,
				"is_error":    ev.IsError,
			}
		default:
			// e.g. compaction events: stream them as generic events so
			// the browser can see "compacted history" rows if it cares.
			env.Kind = ev.Type
			env.Payload = map[string]any{
				"text":    ev.Text,
				"content": ev.Content,
			}
		}
		s.enqueue(env)
	}
}

// Approver returns an agent.Approver that routes permission asks over
// the WS. If the WS is closed when an ask arrives, fallback is consulted
// instead so the agent never wedges. fallback may be nil (defaults to
// agent.DenyAll-equivalent behavior).
func (s *Session) Approver(fallback agent.Approver) agent.Approver {
	return agent.ApproverFunc(func(ctx context.Context, req agent.ApprovalRequest) permission.Decision {
		select {
		case <-s.closed:
			if fallback != nil {
				return fallback.Approve(ctx, req)
			}
			return permission.Decision{
				Behavior: permission.Deny,
				Message:  "remote-control disconnected; no local approver",
			}
		default:
		}

		// Use the loop's tool_use_id as the request id when present so
		// the UI can pair the prompt with the pending tool call card.
		requestID := req.ToolUseID
		if requestID == "" {
			requestID = uuid.NewString()
		}
		ch := make(chan permission.Decision, 1)
		s.apMu.Lock()
		s.approvals[requestID] = ch
		s.apMu.Unlock()
		defer func() {
			s.apMu.Lock()
			delete(s.approvals, requestID)
			s.apMu.Unlock()
		}()

		var inputAny any
		if len(req.Input) > 0 {
			_ = json.Unmarshal(req.Input, &inputAny)
		}
		s.enqueue(EventEnvelope{
			Type: "event",
			Kind: KindPermissionRequest,
			Payload: map[string]any{
				"request_id": requestID,
				"tool_name":  req.ToolName,
				"input":      inputAny,
				"suggestion": req.Suggestion,
			},
		})

		select {
		case <-ctx.Done():
			return permission.Decision{Behavior: permission.Deny, Message: "context cancelled"}
		case <-s.closed:
			if fallback != nil {
				return fallback.Approve(ctx, req)
			}
			return permission.Decision{Behavior: permission.Deny, Message: "remote-control disconnected"}
		case d := <-ch:
			return d
		}
	})
}

// Close shuts the session. Idempotent.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(2*time.Second))
		_ = s.conn.Close()
	})
	if p := s.closeErr.Load(); p != nil {
		return *p
	}
	return nil
}

// Done is closed when the session ends (remote close or local Close).
func (s *Session) Done() <-chan struct{} { return s.closed }

// enqueue submits an event to the write loop, dropping delta-only
// events under backpressure so we never block the agent loop on a slow
// link. Tool calls and permission events are never dropped — they
// matter for correctness.
func (s *Session) enqueue(env EventEnvelope) {
	select {
	case <-s.closed:
		return
	default:
	}
	if env.Kind == KindAssistantDelta {
		select {
		case s.writeCh <- env:
		default:
			// drop; final assistant.message (if the agent emits one)
			// or the UI's autoscroll on next event will recover.
		}
		return
	}
	select {
	case s.writeCh <- env:
	case <-s.closed:
	}
}

// readLoop pulls input envelopes from the server.
func (s *Session) readLoop() {
	defer func() {
		err := errors.New("connection closed")
		s.closeErr.CompareAndSwap(nil, &err)
		s.closeOnce.Do(func() {
			close(s.closed)
			_ = s.conn.Close()
		})
		close(s.inputCh)
	}()
	s.conn.SetReadLimit(1 << 20)
	_ = s.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	s.conn.SetPingHandler(func(string) error {
		_ = s.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return s.conn.WriteControl(websocket.PongMessage, nil,
			time.Now().Add(5*time.Second))
	})

	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		var env InputEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		if env.Type != "input" {
			continue
		}
		if env.Input.Kind == InputKindPermission {
			s.deliverApproval(env.Input)
			continue
		}
		select {
		case s.inputCh <- env.Input:
		case <-s.closed:
			return
		}
	}
}

// writeLoop drains writeCh and serializes to the WS.
func (s *Session) writeLoop() {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.closed:
			return
		case env := <-s.writeCh:
			_ = s.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
			if err := s.conn.WriteJSON(env); err != nil {
				return
			}
		case <-t.C:
			_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := s.conn.WriteControl(websocket.PingMessage, nil,
				time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

func (s *Session) deliverApproval(in Input) {
	d := permission.Decision{}
	switch in.Behavior {
	case "allow":
		d.Behavior = permission.Allow
	default:
		d.Behavior = permission.Deny
	}
	s.apMu.Lock()
	ch, ok := s.approvals[in.RequestID]
	s.apMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- d:
	default:
	}
}
