package tools

import (
	"context"
	"fmt"

	"github.com/greenthread/klaudia/internal/browser"
	"github.com/greenthread/klaudia/internal/lsp"
	"github.com/greenthread/klaudia/internal/sandbox"
	"github.com/greenthread/klaudia/internal/tasks"
)

// regOptions holds the caller-owned, session-scoped resources the registry's
// tools share. The caller is responsible for their lifecycle (Close/KillAll).
type regOptions struct {
	browser *browser.Engine
	shells  *ShellStore
	lsp     *lsp.Pool
}

// RegOption configures DefaultRegistry.
type RegOption func(*regOptions)

// WithBrowserEngine supplies the lazy browser engine backing the web tools. The
// caller Close()s it at session end so any launched Chrome is terminated.
func WithBrowserEngine(e *browser.Engine) RegOption { return func(o *regOptions) { o.browser = e } }

// WithShellStore supplies the background-shell store backing run_in_background +
// BashOutput/KillShell. The caller KillAll()s it at session end.
func WithShellStore(s *ShellStore) RegOption { return func(o *regOptions) { o.shells = s } }

// WithLSP supplies the language-server pool backing Diagnostics/Definition/
// References. The caller Close()s it at session end. When omitted, the LSP tools
// are not registered.
func WithLSP(p *lsp.Pool) RegOption { return func(o *regOptions) { o.lsp = p } }

// DefaultRegistry builds the registry of all implemented local tools, with the
// Bash tool wired to the given executor (local host, or a container sandbox).
// New tools are added here as they are ported. Pass WithBrowserEngine /
// WithShellStore to share caller-owned, session-scoped resources; sensible
// defaults are built when omitted (e.g. in tests) and launch nothing until used.
func DefaultRegistry(executor sandbox.Executor, opts ...RegOption) (*Registry, error) {
	if executor == nil {
		executor = sandbox.NewLocal()
	}
	var cfg regOptions
	for _, o := range opts {
		o(&cfg)
	}
	// Per-session todo state shared with the TodoWrite tool.
	todos := &TodoStore{}
	// Per-session task state shared with task tools.
	taskStore := tasks.New()

	// Shared lazy browser engine for local web tools. Chrome launches only when a
	// browser-backed tool call needs it.
	engine := cfg.browser
	if engine == nil {
		engine = browser.DefaultEngine(context.Background())
	}
	// Background-shell store for run_in_background / BashOutput / KillShell.
	shells := cfg.shells
	if shells == nil {
		shells = NewShellStore(context.Background())
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
		{"Bash", func() (Tool, error) { return NewBash(executor, shells) }},
		{"BashOutput", func() (Tool, error) { return NewBashOutput(shells) }},
		{"KillShell", func() (Tool, error) { return NewKillShell(shells) }},
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
	// LSP code-intel tools are registered only when a language-server pool is
	// supplied (i.e. the interactive/headless CLI, not bare tests).
	if cfg.lsp != nil {
		pool := cfg.lsp
		ctors = append(ctors,
			ctor{"Diagnostics", func() (Tool, error) { return NewDiagnostics(pool) }},
			ctor{"Definition", func() (Tool, error) { return NewDefinition(pool) }},
			ctor{"References", func() (Tool, error) { return NewReferences(pool) }},
		)
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
