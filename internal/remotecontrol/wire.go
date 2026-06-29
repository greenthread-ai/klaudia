// Wire-protocol types for the klaudia ↔ ai-console WebSocket.
//
// Klaudia → server: {"type":"event","kind":"...",...}. The server persists
// each event to coding_events and publishes on a per-session broker that
// the browser / mobile SSE clients subscribe to.
//
// Server → klaudia: {"type":"input","input":{"kind":"...",...}}. Routed
// from POST /api/coding/sessions/{id}/input enqueues in the broker's
// per-session input channel.
//
// Kinds intentionally match the names defined in
// ai-console/internal/coding/broker.go so a JSON envelope built on one
// side decodes verbatim on the other.

package remotecontrol

import "encoding/json"

// Outbound event kinds.
const (
	KindSessionMeta       = "session.meta"
	KindAssistantDelta    = "assistant.delta"
	KindAssistantMessage  = "assistant.message"
	KindToolUse           = "tool.use"
	KindToolResult        = "tool.result"
	KindPermissionRequest = "permission.request"
	KindStatus            = "status"
	KindMessageUser       = "message.user"
	KindTurnDone          = "turn.done"
	KindError             = "error"
)

// Inbound input kinds.
const (
	InputKindMessage    = "message"
	InputKindPermission = "permission"
	InputKindSlash      = "slash"
)

// EventEnvelope wraps a single outbound event for the wire.
type EventEnvelope struct {
	Type    string         `json:"type"` // always "event"
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload,omitempty"`

	// session.meta inlines its fields rather than wrapping in payload so
	// the server can persist meta into the row columns directly.
	Cwd            string `json:"cwd,omitempty"`
	GitBranch      string `json:"git_branch,omitempty"`
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	Title          string `json:"title,omitempty"`
}

// SessionMetaPayload is the body of a session.meta event.
type SessionMetaPayload struct {
	Cwd            string `json:"cwd,omitempty"`
	GitBranch      string `json:"git_branch,omitempty"`
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
}

// InputEnvelope is what the server sends back.
type InputEnvelope struct {
	Type  string `json:"type"` // "input"
	Input Input  `json:"input"`
}

// Input is the user reply routed back to klaudia.
type Input struct {
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Behavior  string          `json:"behavior,omitempty"`
	Scope     string          `json:"scope,omitempty"`
	Command   string          `json:"command,omitempty"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}
