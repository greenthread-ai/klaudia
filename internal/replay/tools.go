package replay

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/schema"
)

// recorded is one tool call's result as the recording has it.
type recorded struct {
	content string
	isError bool
}

// resultStore hands each replayed call the result its recorded twin got.
// The loop gives a tool only its input, not the call's id, so calls are
// matched on name and input, in order — which is also right for a call the
// model made twice with the same input.
type resultStore struct {
	byCall   map[string][]recorded
	turn     int
	findings []Finding
}

func callKey(name string, input json.RawMessage) string {
	var v any
	if json.Unmarshal(input, &v) == nil {
		if b, err := json.Marshal(v); err == nil { // map keys sorted
			input = b
		}
	}
	return name + "\x00" + string(input)
}

func (s *resultStore) take(name string, input json.RawMessage) (recorded, bool) {
	k := callKey(name, input)
	q := s.byCall[k]
	if len(q) == 0 {
		return recorded{}, false
	}
	s.byCall[k] = q[1:]
	return q[0], true
}

// replayTool is a real tool whose Execute returns the recorded result. Its
// name, description, schema, validation and permissions are today's, so the
// loop treats the call as it would now; only the side effect is the
// recording's.
type replayTool struct {
	tools.Tool
	store *resultStore
}

func (t replayTool) Execute(_ context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	return t.executeAs(t.Name(), raw)
}

// unknownTool stands in for a tool this build does not define — an MCP
// server's, usually. It accepts any input.
type unknownTool struct {
	name  string
	store *resultStore
}

func (t unknownTool) Name() string { return t.name }
func (t unknownTool) Description(context.Context) (string, error) {
	return "a tool from the recording", nil
}
func (t unknownTool) InputSchema() json.RawMessage        { return json.RawMessage(`{"type":"object"}`) }
func (t unknownTool) ValidateInput(json.RawMessage) error { return nil }
func (t unknownTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (t unknownTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (t unknownTool) Execute(_ context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	return replayTool{store: t.store}.executeAs(t.name, raw)
}

func (t replayTool) executeAs(name string, raw json.RawMessage) ([]tools.Result, error) {
	r, ok := t.store.take(name, raw)
	if !ok {
		t.store.findings = append(t.store.findings, Finding{Turn: t.store.turn, Kind: "unrecorded-call",
			Detail: fmt.Sprintf("%s was called with input the recording never saw: %s", name, clip(string(raw), 200))})
		return []tools.Result{{Content: "[replay: no recorded result for this call]", IsError: true}}, nil
	}
	return []tools.Result{{Content: r.content, IsError: r.isError}}, nil
}

// envDependent names tools whose validation reads the machine's own setup —
// Skill accepts only the skills installed where it runs, Agent only the
// sub-agent types defined there (user-defined ones included) — so a refusal
// says nothing about today's schema.
var envDependent = map[string]bool{"Skill": true, "Agent": true}

// newTools builds the replay's registry and result store from a recording,
// and checks every recorded call that succeeded against today's schema: one
// that worked then and would be refused now is either a regression or a
// rename that sessions still in flight will trip over.
func newTools(steps Recording) (*tools.Registry, *resultStore, error) {
	builtin, err := tools.BuiltinTools()
	if err != nil {
		return nil, nil, err
	}
	known := map[string]tools.Tool{}
	for _, t := range builtin {
		known[t.Name()] = t
	}
	store := &resultStore{byCall: map[string][]recorded{}}

	// Pair each tool_use with its tool_result by id.
	results := map[string]recorded{}
	for _, s := range steps {
		if s.role != "user" || s.msg == nil {
			continue
		}
		for _, b := range s.msg.Content {
			if tr := b.OfToolResult; tr != nil {
				raw, _ := json.Marshal(tr.Content)
				results[tr.ToolUseID] = recorded{content: resultText(raw), isError: tr.IsError.Value}
			}
		}
	}
	turn := 0
	used := map[string]bool{}
	for _, s := range steps {
		if s.newTurn {
			turn++
		}
		for _, u := range s.toolUses {
			used[u.name] = true
			// The loop repairs string-encoded scalars against the schema
			// before validating or running a call (schema.Coerce), so the
			// input a tool receives is the repaired one; index and check
			// that, or a call the model sent "offset":"180" is never found.
			if t, ok := known[u.name]; ok {
				if fixed, changed := schema.Coerce(t.InputSchema(), u.input); len(changed) > 0 {
					u.input = fixed
				}
			}
			r, answered := results[u.id]
			if answered {
				k := callKey(u.name, u.input)
				store.byCall[k] = append(store.byCall[k], r)
			}
			// Only a call that worked then: one the model got wrong was
			// refused then too, and refusing it again is not a change.
			if answered && r.isError {
				continue
			}
			if t, ok := known[u.name]; ok && !envDependent[u.name] {
				if err := t.ValidateInput(u.input); err != nil {
					store.findings = append(store.findings, Finding{Turn: turn, Kind: "schema",
						Detail: fmt.Sprintf("%s: a recorded call is refused today: %v", u.name, err)})
				}
			}
		}
	}

	var ts []tools.Tool
	for _, t := range builtin {
		ts = append(ts, replayTool{Tool: t, store: store})
	}
	for name := range used {
		if _, ok := known[name]; !ok {
			ts = append(ts, unknownTool{name: name, store: store})
		}
	}
	return tools.NewRegistry(ts...), store, nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
