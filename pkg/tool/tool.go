// Package tool defines the contract every tool implements and the registry the
// agent loop looks tools up in during dispatch. It is the part of tools a
// product embedding the loop needs: Klaudia's own built-in tools (Bash, Read,
// the browser, …) live in internal/tools and are written against this.
//
// The interface mirrors the JS tool contract (name, dynamic description,
// input schema, input validation, execution, permission check, render) so the
// Go port can be diffed against the JS reference one tool at a time.
package tool

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
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
	// Full is the untruncated output, for local display only — it is never sent
	// to the model. Tools that clamp Content to protect the context window set
	// it so the UI can still show everything the command actually printed;
	// leaving it empty means Content is already complete.
	Full string
}

// Display returns the text a local frontend should show: the untruncated output
// when the tool kept one, else the model-facing content.
func (r Result) Display() string {
	if r.Full != "" {
		return r.Full
	}
	return r.Content
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
	// Ask, if non-nil, lets interactive tools (AskUserQuestion) prompt the user.
	Ask Asker
	// Plan, if non-nil, handles ExitPlanMode approval.
	Plan Planner
	// Plans, if non-nil, is the conversation's plan store, and TodoWrite and
	// ExitPlanMode write there instead of the store they were built with.
	//
	// The registry is shared by every conversation in the process, so a store
	// fixed at construction was shared too: two ACP sessions overwrote each
	// other's plan, and a general-purpose sub-agent (which gets every tool)
	// replaced the user's approved plan with its own list. The loop sets this
	// from Options.Plan, which is also the store it announces, so the list a
	// frontend shows is by construction the one the tools changed.
	Plans *plan.Store
	// HostChange, if non-nil, handles RequestHostChange approval. Nil means
	// there is no one to ask, and the tool says so rather than pretending.
	HostChange HostApprover
	// Background, if non-nil, runs work past the end of this call and
	// reports back later (see BackgroundRunner). Nil — a frontend with no
	// one left to report to, such as a headless run — means the work runs in
	// the foreground.
	Background BackgroundRunner
	// Reveal, if non-nil, marks deferred tools active for the rest of the run
	// (used by ToolSearch to load tools on demand).
	Reveal func(names ...string)
	// Progress, if non-nil, reports what a long-running tool is doing while it
	// runs, one already-formatted line at a time. Without it a tool that works
	// for minutes (the Agent tool, which runs a whole child loop) is a closed
	// box: the frontend can only show a spinner, so a sub-agent researching for
	// twenty minutes looked indistinguishable from a hang. It is a plain string
	// callback rather than an event type because tools must not import agent.
	Progress func(line string)
	// ReadText, if non-nil, is where Read gets a text file instead of from
	// disk: an absolute path, a 1-based start line and a line limit (0 for
	// "all"), returning that window of the file.
	//
	// It exists for one frontend — ACP, where the editor serves the file and so
	// hands back the user's *unsaved buffer*. Only Read consults it. Glob and
	// Grep stay on disk: they walk a tree, and the protocol behind this hook is
	// per-file, so routing them through it would mean asking the editor for
	// every candidate.
	ReadText func(ctx context.Context, path string, line, limit int) (string, error)
	// Spill is where a tool that clamps its own output (Bash) keeps the
	// untruncated text. The loop sets it from Options.Spill, the same store
	// its own backstop cap uses; the zero value clamps without writing.
	Spill SpillStore
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

	// PermissionRequest derives the action being requested from raw input, e.g.
	// the file path for Edit or the command line for Bash. Nothing matches on
	// it any more — the specifier is what a prompt shows the user.
	PermissionRequest(raw json.RawMessage) permission.PermissionRequest

	// CheckPermissions returns the tool's intrinsic permission decision for the
	// current mode. permission.Check consults it after the bypass
	// short-circuit, and it is now the only thing that decides: plan mode
	// denies anything that would leave a trace, every other mode allows.
	// Host changes are stopped upstream by the gate in agent.dispatch, not
	// here.
	CheckPermissions(pctx permission.Context, req permission.PermissionRequest) permission.Decision

	// Execute runs the tool and returns one or more tool_result blocks.
	Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error)
}

// Registry maps tool names to implementations, mirroring the JS `q5` lookup.
//
// The mutex exists because the tool set is no longer fixed for a session: an
// MCP config edited while Klaudia runs replaces the MCP tools underneath a
// session that is concurrently reading them to build a request.
type Registry struct {
	mu     sync.RWMutex
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

// Replace swaps the entire tool set atomically. Whole-set replacement rather
// than Add/Remove: a reload has to be able to drop tools whose server is gone,
// and a reader must never observe a half-applied change.
func (r *Registry) Replace(ts ...Tool) {
	next := make(map[string]Tool, len(ts))
	for _, t := range ts {
		next[t.Name()] = t
	}
	r.mu.Lock()
	r.byName = next
	r.mu.Unlock()
}

// Lookup returns the tool registered under name, or (nil, false) if absent.
func (r *Registry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byName[name]
	return t, ok
}

// Names returns the registered tool names in no particular order.
// Names returns the registered tool names in a stable (sorted) order. Stability
// matters: the order drives the tool list sent on every turn, and a varying
// order would change the request's cached prefix each turn and defeat prompt
// caching (the cached prefix begins with the tool definitions).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// All returns every registered tool.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]Tool, 0, len(r.byName))
	for _, t := range r.byName {
		all = append(all, t)
	}
	return all
}

// BackgroundRunner runs work that outlives the tool call that started it, for
// the Agent tool's run_in_background. The host owns everything after Start:
// the work's lifetime (the session's, not the turn's — Esc ends a turn, not a
// research task), what the user is shown while it runs, and how its result
// reaches the model when it finishes.
type BackgroundRunner interface {
	// Start runs work in the background and returns its id at once. label
	// describes it to the user; progress lines from work go to the host.
	Start(label string, work func(ctx context.Context, progress func(line string)) (string, error)) (id string)
}
