package tools

import (
	"context"
	"fmt"

	"github.com/greenthread/klaudia/internal/browser"
	"github.com/greenthread/klaudia/internal/sandbox"
	"github.com/greenthread/klaudia/internal/tasks"
)

// DefaultRegistry builds the registry of all implemented local tools, with the
// Bash tool wired to the given executor (local host, or a container sandbox).
// New tools are added here as they are ported.
//
// browserEngine is the (caller-owned) lazy browser engine backing the web
// tools; the caller is responsible for Close()ing it at session end so Chrome
// is reliably terminated. When omitted (e.g. tests), a lazy default engine is
// built — it launches no process unless a web tool actually runs.
func DefaultRegistry(executor sandbox.Executor, browserEngine ...*browser.Engine) (*Registry, error) {
	if executor == nil {
		executor = sandbox.NewLocal()
	}
	// Per-session todo state shared with the TodoWrite tool.
	todos := &TodoStore{}
	// Per-session task state shared with task tools.
	taskStore := tasks.New()

	// Shared lazy browser engine for local web tools. Chrome launches only when a
	// browser-backed tool call needs it.
	var engine *browser.Engine
	if len(browserEngine) > 0 && browserEngine[0] != nil {
		engine = browserEngine[0]
	} else {
		engine = browser.DefaultEngine(context.Background())
	}

	type ctor struct {
		name string
		make func() (Tool, error)
	}
	ctors := []ctor{
		{"Read", func() (Tool, error) { return NewRead() }},
		{"Write", func() (Tool, error) { return NewWrite() }},
		{"Edit", func() (Tool, error) { return NewEdit() }},
		{"Glob", func() (Tool, error) { return NewGlob() }},
		{"Grep", func() (Tool, error) { return NewGrep() }},
		{"Bash", func() (Tool, error) { return NewBash(executor) }},
		{"TodoWrite", func() (Tool, error) { return NewTodoWrite(todos) }},
		{"TaskCreate", func() (Tool, error) { return NewTaskCreate(taskStore) }},
		{"TaskList", func() (Tool, error) { return NewTaskList(taskStore) }},
		{"TaskGet", func() (Tool, error) { return NewTaskGet(taskStore) }},
		{"TaskUpdate", func() (Tool, error) { return NewTaskUpdate(taskStore) }},
		{"NotebookEdit", func() (Tool, error) { return NewNotebookEdit() }},
		{"AskUserQuestion", func() (Tool, error) { return NewAskUserQuestion() }},
		{"ExitPlanMode", func() (Tool, error) { return NewExitPlanMode() }},
		{"WebSearch", func() (Tool, error) { return NewWebSearch(engine) }},
		{"WebFetch", func() (Tool, error) { return NewWebFetch(engine) }},
		{"BrowserNavigate", func() (Tool, error) { return NewBrowserNavigate(engine) }},
		{"BrowserSnapshot", func() (Tool, error) { return NewBrowserSnapshot(engine) }},
	}

	ts := make([]Tool, 0, len(ctors))
	for _, c := range ctors {
		t, err := c.make()
		if err != nil {
			return nil, fmt.Errorf("build tool %s: %w", c.name, err)
		}
		ts = append(ts, t)
	}
	return NewRegistry(ts...), nil
}
