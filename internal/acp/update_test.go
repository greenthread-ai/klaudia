package acp

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// recorder collects session/update payloads as decoded JSON, because the JSON
// shape is the contract an editor reads — asserting on Go structs would pass
// while a renamed tag broke every client.
type recorder struct {
	mu       sync.Mutex
	updates  []map[string]any
	sessions []string
}

func (r *recorder) update(sessionID string, u any) {
	b, err := json.Marshal(u)
	if err != nil {
		panic("update does not marshal: " + err.Error())
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic("update is not an object: " + err.Error())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, m)
	r.sessions = append(r.sessions, sessionID)
}

// jsonEqual compares a value against the JSON an editor should see. got is
// round-tripped rather than compared field by field, because the wire form is
// the thing under test: a struct tag typo is invisible to a Go comparison.
func jsonEqual(t *testing.T, got any, want string) bool {
	t.Helper()
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON %q: %v", want, err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("got does not marshal: %v", err)
	}
	var g any
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("got does not round-trip: %v", err)
	}
	return reflect.DeepEqual(g, w)
}

func TestTranslate(t *testing.T) {
	tests := []struct {
		name  string
		event agent.Event
		want  []string
	}{
		{
			name:  "assistant text becomes a message chunk",
			event: agent.Event{Type: "assistant", Text: "hello"},
			want:  []string{`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}`},
		},
		{
			name:  "an empty delta says nothing",
			event: agent.Event{Type: "assistant"},
			want:  nil,
		},
		{
			name: "a per-call usage delta is dropped",
			// ACP does have usage_update, but it means "tokens resident in the
			// context, out of this many" — not a delta. Forwarding these would
			// accumulate against a fixed window and show a session at 300% of
			// its context. Agent.postUsage sends the resident figure once per
			// turn instead.
			event: agent.Event{Type: "usage", InputDelta: 1200, OutputDelta: 300, TurnDelta: 1},
			want:  nil,
		},
		{
			name: "tool_use starts a pending tool call",
			event: agent.Event{
				Type: "tool_use", ToolName: "Read", ToolUseID: "tu_1",
				Input: map[string]any{"file_path": "/project/main.go"},
			},
			want: []string{`{
				"sessionUpdate":"tool_call","toolCallId":"tu_1","name":"Read",
				"title":"Read /project/main.go","kind":"read","status":"pending",
				"locations":[{"path":"/project/main.go"}],
				"rawInput":{"file_path":"/project/main.go"}
			}`},
		},
		{
			name: "Bash is an execute call titled by its command",
			event: agent.Event{
				Type: "tool_use", ToolName: "Bash", ToolUseID: "tu_2",
				Input: map[string]any{"command": "go test ./..."},
			},
			want: []string{`{
				"sessionUpdate":"tool_call","toolCallId":"tu_2","name":"Bash",
				"title":"Bash go test ./...","kind":"execute","status":"pending",
				"rawInput":{"command":"go test ./..."}
			}`},
		},
		{
			name: "an MCP tool gets a readable title",
			event: agent.Event{
				Type: "tool_use", ToolName: "mcp__gsx__godot_game_time", ToolUseID: "tu_3",
			},
			want: []string{`{
				"sessionUpdate":"tool_call","toolCallId":"tu_3","name":"mcp__gsx__godot_game_time",
				"title":"godot_game_time (gsx)","kind":"other","status":"pending"
			}`},
		},
		{
			name: "progress moves the call to in_progress",
			event: agent.Event{
				Type: "tool_progress", ToolName: "Agent", ToolUseID: "tu_4", Text: "Grep foo",
			},
			want: []string{`{
				"sessionUpdate":"tool_call_update","toolCallId":"tu_4","status":"in_progress",
				"content":[{"type":"content","content":{"type":"text","text":"Grep foo"}}]
			}`},
		},
		{
			name: "a result completes the call",
			event: agent.Event{
				Type: "tool_result", ToolName: "Read", ToolUseID: "tu_1", Content: "package main",
			},
			want: []string{`{
				"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"completed",
				"content":[{"type":"content","content":{"type":"text","text":"package main"}}]
			}`},
		},
		{
			name: "a failure fails the call",
			event: agent.Event{
				Type: "tool_result", ToolName: "Read", ToolUseID: "tu_1",
				Content: "no such file", IsError: true,
			},
			want: []string{`{
				"sessionUpdate":"tool_call_update","toolCallId":"tu_1","status":"failed",
				"content":[{"type":"content","content":{"type":"text","text":"no such file"}}]
			}`},
		},
		{
			name: "a host-gate refusal is failed, with its reason as content",
			// ACP v1 has no "refused" status, so this is the lesser wrong:
			// "completed" would claim the tool ran. The refusal text carries
			// the distinction the status cannot.
			event: agent.Event{
				Type: "tool_result", ToolName: "Bash", ToolUseID: "tu_5",
				Content: "blocked: this would change /etc", IsError: true, HostBlocked: true,
			},
			want: []string{`{
				"sessionUpdate":"tool_call_update","toolCallId":"tu_5","status":"failed",
				"content":[{"type":"content","content":{"type":"text","text":"blocked: this would change /etc"}}]
			}`},
		},
		{
			name: "the editor gets the untruncated output",
			// Content is what the model saw after clamping; FullContent is the
			// whole thing. An editor has a scrollbar, so it gets the latter.
			event: agent.Event{
				Type: "tool_result", ToolName: "Bash", ToolUseID: "tu_6",
				Content: "line 1\n… 400 lines elided", FullContent: "line 1\nline 2\nline 3",
			},
			want: []string{`{
				"sessionUpdate":"tool_call_update","toolCallId":"tu_6","status":"completed",
				"content":[{"type":"content","content":{"type":"text","text":"line 1\nline 2\nline 3"}}]
			}`},
		},
		{
			name:  "a bare compaction event still says something",
			event: agent.Event{Type: "compaction"},
			want: []string{`{"sessionUpdate":"agent_thought_chunk",
				"content":{"type":"text","text":"compacting the conversation…"}}`},
		},
		{
			name:  "a notice is a thought, not prose",
			event: agent.Event{Type: "notice", Content: "hook: project hooks will not run"},
			want: []string{`{"sessionUpdate":"agent_thought_chunk",
				"content":{"type":"text","text":"hook: project hooks will not run"}}`},
		},
		{
			name:  "a mode change is reported",
			event: agent.Event{Type: "permission_mode", Content: "autonomous"},
			want:  []string{`{"sessionUpdate":"current_mode_update","currentModeId":"autonomous"}`},
		},
		{
			name:  "an unknown event type is ignored rather than guessed at",
			event: agent.Event{Type: "something_new", Text: "x"},
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r recorder
			newTranslator(&r, "sess_1").event(tc.event)
			if len(r.updates) != len(tc.want) {
				t.Fatalf("got %d updates, want %d: %v", len(r.updates), len(tc.want), r.updates)
			}
			for i, want := range tc.want {
				if !jsonEqual(t, r.updates[i], want) {
					got, _ := json.Marshal(r.updates[i])
					t.Errorf("update %d:\ngot  %s\nwant %s", i, got, want)
				}
				if r.sessions[i] != "sess_1" {
					t.Errorf("update %d went to session %q", i, r.sessions[i])
				}
			}
		})
	}
}

func TestToolLocations(t *testing.T) {
	// Locations are the translation an editor actually uses: Zed follows them
	// to keep the open buffer on whatever Klaudia is looking at.
	tests := []struct {
		name  string
		tool  string
		input map[string]any
		want  string
	}{
		{
			name:  "Read with an offset points at the line",
			tool:  "Read",
			input: map[string]any{"file_path": "/a/b.go", "offset": 120},
			want:  `[{"path":"/a/b.go","line":120}]`,
		},
		{
			name:  "an LSP tool uses line",
			tool:  "Definition",
			input: map[string]any{"file": "/a/b.go", "line": 7, "character": 3},
			want:  `[{"path":"/a/b.go","line":7}]`,
		},
		{
			name:  "no offset is a whole-file location",
			tool:  "Write",
			input: map[string]any{"file_path": "/a/b.go", "content": "x"},
			want:  `[{"path":"/a/b.go"}]`,
		},
		{
			name:  "a zero offset is not a line",
			tool:  "Read",
			input: map[string]any{"file_path": "/a/b.go", "offset": 0},
			want:  `[{"path":"/a/b.go"}]`,
		},
		{
			name:  "a tool with no file has no location",
			tool:  "Grep",
			input: map[string]any{"pattern": "func main"},
			want:  `null`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(toolLocations(tc.tool, tc.input))
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			_ = json.Unmarshal(b, &got)
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("locations = %s, want %s", b, tc.want)
			}
		})
	}
}

func TestStopReasonFor(t *testing.T) {
	tests := []struct {
		klaudia string
		want    string
	}{
		{"end_turn", stopEndTurn},
		{"tool_use", stopEndTurn},
		{"stop_sequence", stopEndTurn},
		{"", stopEndTurn},
		{"max_tokens", stopMaxTokens},
		{"refusal", stopRefusal},
		{"max_turns", stopMaxTurnRequests},
		{"user_halt", stopCancelled},
		// Neither of these is end_turn in spirit, but ACP v1 has no code for
		// either and the explanation reaches the user as text instead.
		{"model_context_window_exceeded", stopEndTurn},
		{"blocked_by_hook", stopEndTurn},
	}
	for _, tc := range tests {
		t.Run(tc.klaudia, func(t *testing.T) {
			if got := stopReasonFor(tc.klaudia); got != tc.want {
				t.Errorf("stopReasonFor(%q) = %q, want %q", tc.klaudia, got, tc.want)
			}
		})
	}
}

func TestToolTitleIsReadable(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input map[string]any
		want  string
	}{
		{
			name: "a tool with no salient field is just its name",
			tool: "TodoWrite", input: map[string]any{"todos": []any{}},
			want: "TodoWrite",
		},
		{
			name: "the Agent tool names its sub-agent and task",
			tool: "Agent",
			input: map[string]any{
				"subagent_type": "Explore", "description": "find the ACP handlers",
			},
			want: "Agent (Explore): find the ACP handlers",
		},
		{
			name: "a sub-agent with no description still says which one",
			tool: "Agent", input: map[string]any{"subagent_type": "Plan"},
			want: "Agent: Plan",
		},
		{
			name: "a multi-line command is flattened",
			tool: "Bash", input: map[string]any{"command": "go build ./...\ngo test ./..."},
			want: "Bash go build ./... go test ./...",
		},
		{
			name: "a malformed MCP name falls back to the raw name",
			tool: "mcp__onlyserver", input: nil,
			want: "mcp__onlyserver",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolTitle(tc.tool, tc.input); got != tc.want {
				t.Errorf("toolTitle(%q) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}

func TestToolTitleIsBounded(t *testing.T) {
	// A client renders the title on one line. An unbounded one would be a
	// whole heredoc in a dialog header.
	long := make([]byte, 4096)
	for i := range long {
		long[i] = 'x'
	}
	got := toolTitle("Bash", map[string]any{"command": string(long)})
	if n := len([]rune(got)); n > maxTitle+len("Bash ") {
		t.Errorf("title is %d runes, want at most %d", n, maxTitle+len("Bash "))
	}
}
