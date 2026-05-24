// Package tools defines the contract every local Klaudia tool implements and
// the registry used to look tools up by name during agentic dispatch.
//
// The interface mirrors the JS tool contract (name, dynamic description,
// input schema, input validation, execution, permission check, render) so the
// Go port can be diffed against the JS reference one tool at a time.
package tools

import (
	"context"
	"encoding/json"
)

// PermissionBehavior is the outcome of a permission check, matching the JS
// behaviors emitted by checkToolPermissions ("allow" | "deny" | "ask").
type PermissionBehavior string

const (
	PermissionAllow PermissionBehavior = "allow"
	PermissionDeny  PermissionBehavior = "deny"
	PermissionAsk   PermissionBehavior = "ask"
)

// PermissionDecision is returned by a tool's CheckPermissions. When Behavior is
// PermissionDeny or PermissionAsk, Message explains why (shown to the user/model).
type PermissionDecision struct {
	Behavior PermissionBehavior
	Message  string
	// UpdatedInput optionally replaces the tool input (e.g. after the user
	// edits it at an "ask" prompt). Nil means "use the original input".
	UpdatedInput json.RawMessage
}

// Result is a single tool_result content block produced by a tool execution.
// A tool may yield multiple (e.g. text + image) — Execute returns a slice.
type Result struct {
	// Content is the rendered tool_result payload sent back to the model.
	Content string
	// IsError marks the result as an error tool_result (is_error: true).
	IsError bool
	// Data carries structured output (images, metadata) when Content alone is
	// insufficient; serialized by the API layer as needed.
	Data map[string]any
}

// Context carries per-invocation state into a tool. It is intentionally small
// in Phase 0 and grows as the agent loop, sessions, and permission system land.
type Context struct {
	// WorkingDir is the resolved cwd the tool operates relative to.
	WorkingDir string
	// AbortCh is closed when the turn is aborted; tools should stop promptly.
	AbortCh <-chan struct{}
}

// Tool is the contract implemented by every local tool (Read, Write, Bash, …).
//
// Description is dynamic (the JS `prompt(...)`): the text the model sees can
// depend on available tools/agents and is built per request.
type Tool interface {
	// Name is the string the model uses to call the tool (e.g. "Read").
	Name() string

	// Description returns the model-facing description for this request.
	Description(ctx context.Context) (string, error)

	// InputSchema returns the JSON Schema (draft 2020-12) advertised to the API
	// as input_schema. Generated from a Go struct via the schema package.
	InputSchema() json.RawMessage

	// ValidateInput checks model-supplied raw JSON against the schema and any
	// tool-specific rules, returning a human-readable error if invalid.
	ValidateInput(raw json.RawMessage) error

	// CheckPermissions decides whether this invocation may proceed, given the
	// current permission context (filled in by the permission package).
	CheckPermissions(ctx context.Context, raw json.RawMessage) (PermissionDecision, error)

	// Execute runs the tool and returns one or more tool_result blocks.
	Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error)
}

// Registry maps tool names to implementations, mirroring the JS `q5` lookup.
type Registry struct {
	byName map[string]Tool
}

// NewRegistry builds a Registry from the given tools, keyed by Name().
func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{byName: make(map[string]Tool, len(ts))}
	for _, t := range ts {
		r.byName[t.Name()] = t
	}
	return r
}

// Lookup returns the tool registered under name, or (nil, false) if absent.
func (r *Registry) Lookup(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// Names returns the registered tool names in no particular order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	return names
}
