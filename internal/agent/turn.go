package agent

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/plan"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Turn is everything a frontend varies from one user turn to the next: the
// prompt, the history it continues, and the callbacks through which the loop
// reaches back out to whoever is driving it.
//
// It is a struct and not a parameter list, and that is the whole point of the
// type. Each frontend used to declare its own RunFunc — tui.RunFunc took nine
// positional parameters, streamjson.RunFunc took five — and the CLI wrote a
// separate closure per mode that assigned the fields of Options by hand. Two
// consequences, both of which were live bugs rather than hypotheticals:
//
//   - A frontend that *omitted* a capability was indistinguishable from one that
//     did not want it. The stream-json closure never set Asker or Planner, so
//     AskUserQuestion and ExitPlanMode were silently dead over that transport:
//     the model called them and got told there was nobody to ask, on a channel
//     whose entire purpose is an editor with a user sitting in front of it.
//   - Adding a parameter meant touching every frontend, so nobody did. The
//     capability gap between the TUI and the embedding channel only ever widened.
//
// With a struct the loop does the copying (see Apply) and a frontend states what
// it supports by setting fields. A new capability is a new field that existing
// frontends leave zero, which is exactly the behaviour they had before it existed.
type Turn struct {
	// Prompt is the user's new input for this turn. It is appended after History.
	Prompt string
	// History is the conversation so far — Result.Messages from the previous
	// turn, or a resumed transcript for the first.
	History []anthropic.BetaMessageParam
	// Emit receives the loop's event stream. Required: a nil Emit means the
	// frontend shows the user nothing.
	Emit Emitter
	// Approver resolves permission asks, including host changes. Nil means
	// DenyAll.
	Approver Approver
	// Asker, if set, lets AskUserQuestion (and MCP elicitation, which is pointed
	// at the same prompt) reach the user.
	Asker tools.Asker
	// Planner, if set, handles ExitPlanMode approval.
	Planner tools.Planner
	// Interject is polled for input the user typed while the turn was running.
	// Nil means nothing can interrupt — correct for a one-shot transport.
	Interject func() Interjection
	// BeforeEdit is called with the paths a mutating tool is about to change,
	// synchronously, just before it runs. Nil means no undo checkpointing.
	BeforeEdit func(tool string, paths []string)
	// Mode, if set, is the live permission mode for this turn, overriding
	// whatever the caller put in Options.Permission.
	//
	// A function and not a value, for the same reason permission.Context uses
	// one: the mode can change in the middle of a turn — /mode bypass, or
	// ExitPlanMode being approved — and every tool dispatch after that should
	// see the new one rather than waiting for the next turn boundary.
	//
	// It is here rather than left to the caller because the frontend is the
	// only thing that knows which conversation a turn belongs to, and the ACP
	// frontend has more than one: its permission mode is per editor session,
	// so a single mode resolved by the CLI at startup cannot be right for all
	// of them. Nil keeps the caller's own Permission context, which is what a
	// frontend with one fixed mode wants.
	Mode func() permission.Mode
	// Recorder, if set, is the transcript this turn belongs to, overriding the
	// process-wide one the caller opened.
	//
	// Here for the same reason Mode is: a frontend with more than one
	// conversation is the only thing that knows which. With one process-wide
	// transcript, two ACP threads appended to the same file — interleaving two
	// conversations into one, so session/load replayed something that never
	// happened. Nil keeps the caller's recorder.
	Recorder Recorder
	// Plan, if set, is this conversation's plan store, overriding the
	// process-wide one the caller built. The same reason again: the ACP
	// frontend holds several conversations, and with one store they shared
	// one step list and one plan file. Nil keeps the caller's store.
	Plan *plan.Store
	// ReadText, if set, reads a text file on the frontend's behalf instead of
	// from disk: path, a 1-based start line and a line limit (0 for "all"),
	// returning the requested window.
	//
	// The ACP frontend sets it so Read goes through the editor, which hands back
	// the user's *unsaved buffer*. Reading disk while the user looks at
	// unsaved edits is how the model ends up reasoning about text that is no
	// longer there. Nil — every other frontend — reads disk.
	ReadText func(ctx context.Context, path string, line, limit int) (string, error)
	// Background, if set, runs the Agent tool's run_in_background work past
	// the end of the call and reports it back to this frontend (see
	// Background in background.go). Nil — a frontend with no one left to
	// report to, like a one-shot headless run — keeps every sub-agent in the
	// foreground.
	Background tools.BackgroundRunner
}

// Apply copies the turn onto opts, leaving every other field of opts alone.
//
// Frontends and the CLI call this instead of assigning the fields themselves.
// The assignment list is written once, here, next to the struct it mirrors —
// which is the property the old per-frontend lists did not have.
func (t Turn) Apply(opts *Options) {
	opts.Prompt = t.Prompt
	opts.InitialMessages = t.History
	opts.Approver = t.Approver
	opts.Asker = t.Asker
	opts.Planner = t.Planner
	opts.Interject = t.Interject
	opts.BeforeEdit = t.BeforeEdit
	opts.ReadText = t.ReadText
	opts.Background = t.Background
	// Overridden, not cleared: a nil Mode leaves whatever permission context
	// the caller built, so a frontend with one fixed mode need not restate it
	// on every turn.
	if t.Mode != nil {
		opts.Permission.Mode = t.Mode
	}
	// Same again: a nil Recorder leaves the caller's process-wide transcript
	// rather than turning persistence off.
	if t.Recorder != nil {
		opts.Recorder = t.Recorder
	}
	if t.Plan != nil {
		opts.Plan = t.Plan
	}
}

// RunFunc runs one turn to completion and reports the result. The CLI supplies
// it, closing over the provider, tools, model, system prompt and permission
// context; the frontend calls it once per prompt and carries Result.Messages
// forward as the next Turn's History.
type RunFunc func(ctx context.Context, turn Turn) (Result, error)
