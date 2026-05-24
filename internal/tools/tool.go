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

	"github.com/greenthread/klaudia/internal/permission"
)

// Result is a single tool_result content block produced by a tool execution.
// A tool may yield multiple (e.g. text + image) — Execute returns a slice.
type Result struct {
	// Content is the rendered tool_result payload sent back to the model.
	Content string
	// IsError marks the result as an error tool_result (is_error: true).
	IsError bool
	// Images are vision content blocks returned to the model (e.g. Read of an
	// image file). The agent loop emits them as image blocks in the tool_result.
	Images []ResultImage
	// Data carries structured output (metadata) when Content alone is
	// insufficient; serialized by the API layer as needed.
	Data map[string]any
}

// ResultImage is a base64-encoded image returned to the model for vision.
type ResultImage struct {
	MediaType string // e.g. "image/png", "image/jpeg", "image/gif", "image/webp"
	Base64    string // base64-encoded image bytes
}

// AskOption is one choice presented to the user by AskUserQuestion.
type AskOption struct {
	Label       string `json:"label" jsonschema:"description=The short answer label the user picks"`
	Description string `json:"description,omitempty" jsonschema:"description=Optional clarifying detail for this option"`
}

// Asker lets a tool put a multiple-choice question to the user and await the
// answer. Provided by the frontend (the TUI prompts; headless has none, so the
// tool reports it cannot ask). Returns the chosen option's label.
type Asker interface {
	Ask(ctx context.Context, question string, options []AskOption) (string, error)
}

// Planner handles ExitPlanMode: the model presents its plan and the frontend
// decides whether to approve it and leave read-only plan mode. Returns true if
// approved (the frontend then switches the session out of plan mode).
type Planner interface {
	ExitPlan(ctx context.Context, plan string) (approved bool, err error)
}

// Context carries per-invocation state into a tool. It is intentionally small
// and grows as features land.
type Context struct {
	// WorkingDir is the resolved cwd the tool operates relative to.
	WorkingDir string
	// AbortCh is closed when the turn is aborted; tools should stop promptly.
	AbortCh <-chan struct{}
	// Ask, if non-nil, lets interactive tools (AskUserQuestion) prompt the user.
	Ask Asker
	// Plan, if non-nil, handles ExitPlanMode approval.
	Plan Planner
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

	// PermissionRequest derives the rule-matchable action (specifier) from raw
	// input, e.g. the file path for Edit or the command line for Bash.
	PermissionRequest(raw json.RawMessage) permission.PermissionRequest

	// CheckPermissions returns the tool's intrinsic permission decision for the
	// current mode. It is consulted by permission.Check after deny/allow rules
	// and the bypass short-circuit. Read-only tools allow; mutating tools ask
	// (unless the mode auto-accepts).
	CheckPermissions(pctx permission.Context, req permission.PermissionRequest) permission.Decision

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

// All returns every registered tool.
func (r *Registry) All() []Tool {
	all := make([]Tool, 0, len(r.byName))
	for _, t := range r.byName {
		all = append(all, t)
	}
	return all
}
