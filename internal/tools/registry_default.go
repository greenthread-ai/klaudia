package tools

import (
	"context"
	"fmt"

	"github.com/greenthread-ai/klaudia/internal/browser"
	"github.com/greenthread-ai/klaudia/internal/lsp"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// regOptions holds the caller-owned, session-scoped resources the registry's
// tools share. The caller is responsible for their lifecycle (Close/KillAll).
type regOptions struct {
	browser *browser.Engine
	shells  *JobStore
	lsp     *lsp.Pool
	plans   *plan.Store
	media   api.MediaClient
	models  MediaModels
}

// RegOption configures DefaultRegistry.
type RegOption func(*regOptions)

// WithBrowserEngine supplies the lazy browser engine backing the web tools. The
// caller Close()s it at session end so any launched Chrome is terminated.
func WithBrowserEngine(e *browser.Engine) RegOption { return func(o *regOptions) { o.browser = e } }

// WithJobStore supplies the managed-job store backing run_in_background,
// BashOutput/KillShell/Jobs/RestartJob. The caller KillAll()s it at session end.
func WithJobStore(s *JobStore) RegOption { return func(o *regOptions) { o.shells = s } }

// WithPlanStore supplies the session plan shared by TodoWrite and
// ExitPlanMode. Pass it when the caller wants to render the plan or persist it:
// the store built by default is reachable only from inside the registry, which
// is how the todo list came to be rendered by exactly one frontend.
func WithPlanStore(s *plan.Store) RegOption { return func(o *regOptions) { o.plans = s } }

// WithMedia supplies the endpoint's non-chat models and the client that calls
// them. Each media tool is registered only when models lists one of its kind
// (media.go); omitted, or with a provider that serves none, there are none.
func WithMedia(client api.MediaClient, models MediaModels) RegOption {
	return func(o *regOptions) { o.media, o.models = client, models }
}

// WithLSP supplies the language-server pool backing Diagnostics/Definition/
// References. The caller Close()s it at session end. When omitted, the LSP tools
// are not registered.
func WithLSP(p *lsp.Pool) RegOption { return func(o *regOptions) { o.lsp = p } }

// DeferredBuiltins are built-in tools that are registered but, like MCP
// tools, withheld from the initial request behind ToolSearch. They are the
// narrower code-intelligence lookups: useful when wanted, but four more
// definitions in every request otherwise. Diagnostics, Definition and
// References stay loaded; they are the edit-check-fix loop.
var DeferredBuiltins = map[string]bool{
	"Implementation": true, "Hover": true, "DocumentSymbols": true, "Rename": true,
}

// DefaultRegistry builds the registry of all implemented local tools, with the
// Bash tool wired to the given executor (local host, or a container sandbox).
// New tools are added here as they are ported. Pass WithBrowserEngine /
// WithJobStore to share caller-owned, session-scoped resources; sensible
// defaults are built when omitted (e.g. in tests) and launch nothing until used.
func DefaultRegistry(executor sandbox.Executor, opts ...RegOption) (*Registry, error) {
	if executor == nil {
		executor = sandbox.NewLocal()
	}
	var cfg regOptions
	for _, o := range opts {
		o(&cfg)
	}
	// The session plan, shared with TodoWrite and ExitPlanMode. Built here only
	// when the caller did not supply one; a caller that wants to render the
	// plan passes WithPlanStore and keeps the handle. The fallback writes no
	// artefact: plans are keyed by session and the registry has no session id.
	plans := cfg.plans
	if plans == nil {
		plans = plan.New("")
	}

	// Shared lazy browser engine for local web tools. Chrome launches only when a
	// browser-backed tool call needs it.
	engine := cfg.browser
	if engine == nil {
		engine = browser.DefaultEngine(context.Background())
	}
	// Background-shell store for run_in_background / BashOutput / KillShell.
	shells := cfg.shells
	if shells == nil {
		shells = NewJobStore(context.Background(), "", "")
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
		{"Jobs", func() (Tool, error) { return NewJobs(shells) }},
		{"RestartJob", func() (Tool, error) { return NewRestartJob(shells) }},
		{"TodoWrite", func() (Tool, error) { return NewTodoWrite(plans) }},
		{"NotebookEdit", func() (Tool, error) { return NewNotebookEdit() }},
		{"AskUserQuestion", func() (Tool, error) { return NewAskUserQuestion() }},
		{"ExitPlanMode", func() (Tool, error) { return NewExitPlanMode(plans) }},
		{"RequestHostChange", func() (Tool, error) { return NewRequestHostChange() }},
		{"BrowserSearch", func() (Tool, error) { return NewBrowserSearch(engine) }},
		{"BrowserFetch", func() (Tool, error) { return NewBrowserFetch(engine) }},
		{"BrowserNavigate", func() (Tool, error) { return NewBrowserNavigate(engine) }},
		{"BrowserSnapshot", func() (Tool, error) { return NewBrowserSnapshot(engine) }},
	}
	// LSP code-intel tools are registered only when a language-server pool is
	// supplied (i.e. the interactive/headless CLI, not bare tests). The ones
	// in DeferredBuiltins are registered but withheld from requests until
	// ToolSearch reveals them.
	if cfg.lsp != nil {
		pool := cfg.lsp
		ctors = append(ctors,
			ctor{"Diagnostics", func() (Tool, error) { return NewDiagnostics(pool) }},
			ctor{"Definition", func() (Tool, error) { return NewDefinition(pool) }},
			ctor{"References", func() (Tool, error) { return NewReferences(pool) }},
			ctor{"Implementation", func() (Tool, error) { return NewImplementation(pool) }},
			ctor{"Hover", func() (Tool, error) { return NewHover(pool) }},
			ctor{"DocumentSymbols", func() (Tool, error) { return NewDocumentSymbols(pool) }},
			ctor{"Rename", func() (Tool, error) { return NewRename(pool) }},
			ctor{"WorkspaceSymbol", func() (Tool, error) { return NewWorkspaceSymbol(pool) }},
		)
	}

	// One tool per kind of non-chat model the endpoint lists.
	if cfg.media != nil {
		client, models := cfg.media, cfg.models
		if ms := models.of(api.ModelKindTranscribe); len(ms) > 0 {
			ctors = append(ctors, ctor{"Transcribe", func() (Tool, error) { return NewTranscribe(client, ms) }})
		}
		if ms := models.of(api.ModelKindSpeech); len(ms) > 0 {
			ctors = append(ctors, ctor{"Speak", func() (Tool, error) { return NewSpeak(client, ms) }})
		}
		if ms := models.of(api.ModelKindImage); len(ms) > 0 {
			ctors = append(ctors, ctor{"GenerateImage", func() (Tool, error) { return NewGenerateImage(client, ms) }})
		}
		if ms := models.of(api.ModelKindMusic); len(ms) > 0 {
			ctors = append(ctors, ctor{"GenerateMusic", func() (Tool, error) { return NewGenerateMusic(client, ms) }})
		}
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
