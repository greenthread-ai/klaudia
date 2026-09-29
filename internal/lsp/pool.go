package lsp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

// Pool lazily spawns and reuses one language server per language, scoped to a
// session (root context). Servers launch only when a tool first needs them and
// are shut down by Close. Safe for concurrent use.
type Pool struct {
	parent   context.Context
	root     string                // workspace root (cwd)
	disabled map[string]bool       // languages turned off via config
	override map[string]ServerSpec // config-supplied server commands

	mu      sync.Mutex
	clients map[string]*Client // language -> client
}

// NewPool creates a pool rooted at cwd. disabled lists languages to skip;
// override supplies custom server commands keyed by language.
func NewPool(parent context.Context, root string, disabled []string, override map[string]ServerSpec) *Pool {
	if parent == nil {
		parent = context.Background()
	}
	dis := map[string]bool{}
	for _, d := range disabled {
		dis[d] = true
	}
	return &Pool{
		parent: parent, root: root, disabled: dis, override: override,
		clients: map[string]*Client{},
	}
}

// serverFor resolves the spec handling a file (override → builtin), or false.
func (p *Pool) serverFor(path string) (ServerSpec, string, bool) {
	spec, ok := specForExt(filepath.Ext(path))
	if !ok {
		return ServerSpec{}, "", false
	}
	return p.resolve(spec)
}

// resolve applies config to a builtin spec: a disabled language resolves to
// nothing, and an override replaces the command. It reports the binary found.
func (p *Pool) resolve(spec ServerSpec) (ServerSpec, string, bool) {
	if p.disabled[spec.Language] {
		return ServerSpec{}, "", false
	}
	if o, ok := p.override[spec.Language]; ok {
		o.Language, o.LanguageID, o.Exts = spec.Language, firstNonEmpty(o.LanguageID, spec.LanguageID), spec.Exts
		bin, found := detect(o)
		return o, bin, found
	}
	bin, found := detect(spec)
	return spec, bin, found
}

// clientFor returns a ready (initialized) client for the file's language,
// spawning one on first use. Returns a clear error when no server is available.
func (p *Pool) clientFor(path string) (*Client, ServerSpec, error) {
	spec, bin, found := p.serverFor(path)
	if spec.Language == "" {
		return nil, spec, fmt.Errorf("no language server configured for %q files", filepath.Ext(path))
	}
	c, err := p.clientForSpec(spec, bin, found)
	return c, spec, err
}

// clientForSpec returns the running client for a resolved spec, spawning and
// initializing one on first use.
func (p *Pool) clientForSpec(spec ServerSpec, bin string, found bool) (*Client, error) {
	if !found {
		return nil, fmt.Errorf("no %s language server found (install %q and ensure it's on PATH or a standard toolchain dir)", spec.Language, spec.Bin)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.clients[spec.Language]; c != nil {
		return c, nil
	}
	c, err := NewClient(p.parent, bin, spec.Args...)
	if err != nil {
		return nil, err
	}
	if err := c.Initialize(p.parent, p.root); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize %s: %w", spec.Bin, err)
	}
	p.clients[spec.Language] = c
	return c, nil
}

// Diagnostics returns the server's diagnostics for a file.
func (p *Pool) Diagnostics(ctx context.Context, path string) ([]Diagnostic, error) {
	c, spec, err := p.clientFor(path)
	if err != nil {
		return nil, err
	}
	return c.Diagnostics(ctx, path, spec.LanguageID)
}

// Definition returns definition location(s) for the symbol at (line,character).
func (p *Pool) Definition(ctx context.Context, path string, pos Position) ([]Location, error) {
	c, spec, err := p.clientFor(path)
	if err != nil {
		return nil, err
	}
	return c.Definition(ctx, path, spec.LanguageID, pos)
}

// References returns reference location(s) for the symbol at (line,character).
func (p *Pool) References(ctx context.Context, path string, pos Position) ([]Location, error) {
	c, spec, err := p.clientFor(path)
	if err != nil {
		return nil, err
	}
	return c.References(ctx, path, spec.LanguageID, pos)
}

// WorkspaceSymbol searches the workspace for symbols matching query. With a
// path, the server for that file's language answers. Without one, every
// language that is already running or whose project marker (go.mod,
// Cargo.toml, …) sits at the workspace root is asked, and the matches are
// concatenated in that order. Servers that fail are reported in the error
// alongside whatever the others found, so a partial answer is not lost.
func (p *Pool) WorkspaceSymbol(ctx context.Context, query, path string) ([]Symbol, error) {
	if path != "" {
		c, _, err := p.clientFor(path)
		if err != nil {
			return nil, err
		}
		return c.WorkspaceSymbol(ctx, query)
	}
	specs := p.workspaceSpecs()
	if len(specs) == 0 {
		return nil, fmt.Errorf("no language detected at the workspace root; pass a file in the language to search")
	}
	var (
		all  []Symbol
		errs []error
	)
	for _, spec := range specs {
		resolved, bin, found := p.resolve(spec)
		c, err := p.clientForSpec(resolved, bin, found)
		if err == nil {
			var syms []Symbol
			syms, err = c.WorkspaceSymbol(ctx, query)
			all = append(all, syms...)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", spec.Language, err))
		}
	}
	return all, errors.Join(errs...)
}

// workspaceSpecs lists the builtin languages a file-less workspace search
// should ask: those not disabled that already have a running server or whose
// project marker is at the root. Detection of the binary is left to the caller
// so a missing server is reported rather than silently skipped.
func (p *Pool) workspaceSpecs() []ServerSpec {
	p.mu.Lock()
	running := make(map[string]bool, len(p.clients))
	for lang := range p.clients {
		running[lang] = true
	}
	p.mu.Unlock()

	var specs []ServerSpec
	for _, spec := range builtinServers {
		if p.disabled[spec.Language] {
			continue
		}
		if running[spec.Language] || (p.root != "" && hasAnyMarker(p.root, spec.Markers)) {
			specs = append(specs, spec)
		}
	}
	return specs
}

// Close shuts down all spawned servers.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for lang, c := range p.clients {
		c.Close()
		delete(p.clients, lang)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
