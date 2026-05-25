package lsp

import (
	"context"
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
	ext := filepath.Ext(path)
	spec, ok := specForExt(ext)
	if !ok {
		return ServerSpec{}, "", false
	}
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
	if !found {
		return nil, spec, fmt.Errorf("no %s language server found (install %q and ensure it's on PATH or a standard toolchain dir)", spec.Language, spec.Bin)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.clients[spec.Language]; c != nil {
		return c, spec, nil
	}
	c, err := NewClient(p.parent, bin, spec.Args...)
	if err != nil {
		return nil, spec, err
	}
	if err := c.Initialize(p.parent, p.root); err != nil {
		c.Close()
		return nil, spec, fmt.Errorf("initialize %s: %w", spec.Bin, err)
	}
	p.clients[spec.Language] = c
	return c, spec, nil
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
