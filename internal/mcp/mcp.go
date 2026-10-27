// Package mcp connects to Model Context Protocol servers over stdio or HTTP
// (streamable / SSE) and exposes their tools and resources to Klaudia. It wraps
// the official modelcontextprotocol/go-sdk client.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/internal/version"
)

// ServerConfig defines how to reach an MCP server. A stdio server sets
// Command (+ Args/Env); an HTTP server sets URL (Type selects the streamable
// HTTP transport, the default, or "sse" for the legacy SSE transport).
type ServerConfig struct {
	// stdio transport
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// HTTP transport
	Type string `json:"type,omitempty"` // "http" (default when URL set) | "sse"
	URL  string `json:"url,omitempty"`
	// Headers are sent on every request to an HTTP server — typically
	// Authorization. They were once accepted by the decoder and dropped, so a
	// server needing a token failed with nothing pointing at the config.
	Headers map[string]string `json:"headers,omitempty"`
	// ReadOnly overrides what this server's tools claim about themselves, in
	// either direction. Unset trusts the server's readOnlyHint annotations.
	//
	//	(unset)  trust each tool's readOnlyHint
	//	true     every tool is read-only, whatever it says or omits
	//	false    no tool is read-only, whatever it claims
	//
	// True is for a server that annotates nothing — including one launched in
	// its own read-only mode, where the operator knows something the protocol
	// was not told.
	//
	// False is the more important one. readOnlyHint is a claim made by the
	// server about itself, and read-only sub-agents are handed tools on the
	// strength of it. Nothing verifies it, and a third-party server that
	// asserts it wrongly — through carelessness or otherwise — would have its
	// word taken. False is how an operator declines to take it, without giving
	// up the server for the main agent, which still asks before every call.
	ReadOnly *bool `json:"readOnly,omitempty"`
}

// Config is the .mcp.json shape: a map of server name → launch config.
type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ConfigPaths returns the .mcp.json files that apply to a project at dir, in
// increasing order of precedence: the global ~/.klaudia/.mcp.json (honouring
// KLAUDIA_CONFIG_DIR), the project's ./.mcp.json, then ./.klaudia/.mcp.json.
//
// Loading and watching must agree on this list — a file that is read but not
// watched reloads only by restart, and one that is watched but not read fires
// reloads that change nothing — so both go through here.
func ConfigPaths(dir string) []string {
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, p := range []string{
		filepath.Join(session.ConfigRoot(), ".mcp.json"),
		filepath.Join(dir, ".mcp.json"),
		filepath.Join(dir, ".klaudia", ".mcp.json"),
	} {
		// When the project IS the config dir the same file appears twice.
		// Collapsing it here keeps a duplicate from re-reporting its errors.
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// LoadConfig reads the .mcp.json files that apply to dir (see ConfigPaths), in
// increasing precedence. A missing file yields an empty config (not an error);
// later files override earlier ones per server name, so a project can redefine
// or extend a globally configured server.
//
// A file that cannot be read or parsed is reported and skipped, and the others
// still load: a typo in one project's .mcp.json used to take the global
// servers down with it. The config returned alongside an error is therefore
// usable, but partial — the reload path declines to apply it for that reason.
//
// The global scope is what makes a personal server — one you want in every
// project rather than in one repo — installable once. Without it the only
// answer was to copy the same file into every checkout.
func LoadConfig(dir string) (Config, error) {
	cfg := Config{MCPServers: map[string]ServerConfig{}}
	var errs []error
	for _, p := range ConfigPaths(dir) {
		data, err := os.ReadFile(p)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			continue
		}
		var c Config
		if err := json.Unmarshal(stripJSONComments(data), &c); err != nil {
			// The full path, not the base name: three files share the name
			// ".mcp.json", and "which one is broken" is the entire question
			// when the answer is a config in a different directory.
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		for name, sc := range c.MCPServers {
			cfg.MCPServers[name] = sc
		}
	}
	return cfg, errors.Join(errs...)
}

// Server is a connected MCP server session and its configured name.
//
// session is guarded because the goroutines that read it and the ones that
// replace it are different: tool calls run on the agent loop, /mcp runs on the
// TUI, and a config reload runs on the config watcher.
type Server struct {
	Name string

	mu      sync.RWMutex
	session *mcpsdk.ClientSession

	// reviveMu serialises automatic relaunches of this one server, and
	// lastRevive rate-limits them. Both are needed because the attempt can come
	// from anywhere: two tool calls, or a tool call and /mcp, arriving together
	// would otherwise each spawn a child process and close one of them.
	reviveMu   sync.Mutex
	lastRevive time.Time

	// disabled is set by /mcp Disconnect and cleared by Reconnect. Without it
	// the model's next call revived the server the user had just turned off.
	disabled atomic.Bool
}

// sess returns the live session, or nil when the server is disconnected.
func (s *Server) sess() *mcpsdk.ClientSession {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.session
}

// swapSession installs next and returns the session it displaced, which the
// caller closes. Returning it rather than closing it here keeps the (possibly
// blocking) Close outside the lock.
func (s *Server) swapSession(next *mcpsdk.ClientSession) *mcpsdk.ClientSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.session
	s.session = next
	return prev
}

// Connected reports whether the server currently holds a session object. It is
// cheap and non-blocking, which is what /mcp and the tool wrappers want, but it
// is not a health check: see alive.
func (s *Server) Connected() bool { return s.sess() != nil }

// alive reports whether the server still answers a protocol ping.
//
// Connected only says a session object exists. A stdio server whose child
// process has died keeps a non-nil ClientSession — nothing nils it out — so a
// reload that trusted Connected would leave the corpse in place and skip the
// relaunch, and no edit to .mcp.json could bring the server back short of
// restarting Klaudia. Renaming the server key was the only workaround, because
// that made it look new rather than unchanged.
func (s *Server) alive(ctx context.Context) bool {
	sess := s.sess()
	if sess == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, livenessTimeout)
	defer cancel()
	return sess.Ping(ctx, nil) == nil
}

// newClient builds the MCP client for one server.
//
// Capabilities are set explicitly, which also drops the SDK's default
// "roots":{"listChanged":true}. Klaudia never registers a root, so that default
// advertised a feature whose only possible answer was an empty list — and roots
// is deprecated as of protocol 2026-07-28 anyway. Elicitation is advertised
// form-only, and only when there is a frontend to carry the question; see
// elicit.go.
func newClient(name string, el *Elicitor) *mcpsdk.Client {
	opts := &mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}}
	if h := el.handlerFor(name); h != nil {
		opts.ElicitationHandler = h
		opts.Capabilities.Elicitation = &mcpsdk.ElicitationCapabilities{
			Form: &mcpsdk.FormElicitationCapabilities{},
		}
	}
	return mcpsdk.NewClient(&mcpsdk.Implementation{Name: "klaudia", Version: version.Version}, opts)
}

// clientWithHeaders returns an HTTP client that sets the configured headers on
// every request, or nil (the transport's default client) when there are none.
func clientWithHeaders(h map[string]string) *http.Client {
	if len(h) == 0 {
		return nil
	}
	return &http.Client{Transport: headerTransport{base: http.DefaultTransport, headers: h}}
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context()) // a RoundTripper must not modify the caller's request
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.base.RoundTrip(r)
}

// connectServer connects to a server using the transport its config implies:
// HTTP (streamable, or SSE) when URL is set, otherwise stdio.
func connectServer(ctx context.Context, name string, cfg ServerConfig, el *Elicitor) (*Server, error) {
	if url := strings.TrimSpace(cfg.URL); url != "" {
		var t mcpsdk.Transport
		switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
		case "sse":
			t = &mcpsdk.SSEClientTransport{Endpoint: url, HTTPClient: clientWithHeaders(cfg.Headers)}
		default: // "http" / "streamable" / unset
			t = &mcpsdk.StreamableClientTransport{Endpoint: url, HTTPClient: clientWithHeaders(cfg.Headers)}
		}
		return ConnectTransport(ctx, name, t, el)
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp %q: config has neither command (stdio) nor url (http)", name)
	}
	return ConnectCommand(ctx, name, cfg, el)
}

// ConnectCommand spawns a stdio MCP server and connects to it.
func ConnectCommand(ctx context.Context, name string, cfg ServerConfig, el *Elicitor) (*Server, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return ConnectTransport(ctx, name, &mcpsdk.CommandTransport{Command: cmd}, el)
}

// ConnectTransport connects to a server over an arbitrary transport (used by
// command servers and by tests via an in-memory transport). A nil Elicitor
// advertises no elicitation capability.
func ConnectTransport(ctx context.Context, name string, t mcpsdk.Transport, el *Elicitor) (*Server, error) {
	session, err := newClient(name, el).Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp %q connect: %w", name, err)
	}
	return &Server{Name: name, session: session}, nil
}

// Manager holds connected MCP servers. It keeps the launch config and connect
// context so servers can be disconnected and reconnected mid-session (e.g. when
// a server crashes); reconnect swaps the live session into the existing *Server
// pointer, so already-registered tool wrappers resume working.
type Manager struct {
	mu      sync.RWMutex
	servers []*Server
	cfg     Config
	ctx     context.Context

	// elicitor is handed to every client this manager builds, including the
	// ones a reload or a revive builds, so a relaunched server keeps the
	// ability to ask the user things. Nil in a headless run.
	elicitor *Elicitor

	// connect is how a server is launched, as a field so a test can relaunch
	// one without a real binary on PATH. Nothing in production sets it; nil
	// means connectServer.
	connect func(ctx context.Context, name string, cfg ServerConfig, el *Elicitor) (*Server, error)
}

// dial launches one server through whatever connect is in force.
func (m *Manager) dial(ctx context.Context, name string, cfg ServerConfig) (*Server, error) {
	m.mu.RLock()
	fn, el := m.connect, m.elicitor
	m.mu.RUnlock()
	if fn == nil {
		fn = connectServer
	}
	return fn(ctx, name, cfg, el)
}

// connectTimeout bounds launching one server at startup or on a config
// reload. Generous, because a first `npx` run downloads the server; what it
// stops is a server that never answers initialize hanging startup for good.
const connectTimeout = 60 * time.Second

// dialWithin launches a server, giving up after timeout.
//
// The dial itself runs on the manager's own context, not on one that ends at
// the deadline: a legacy SSE transport ties its long-lived stream to the
// context it was connected with, so a session dialled under a timeout context
// died as soon as that context was cancelled — every /mcp reconnect or
// auto-revive of an SSE server came back connected and then failed its first
// call. The deadline is enforced from outside instead, and a session that
// arrives after it is closed rather than leaked.
func (m *Manager) dialWithin(timeout time.Duration, verb, name string, cfg ServerConfig) (*Server, error) {
	type dialed struct {
		s   *Server
		err error
	}
	ch := make(chan dialed, 1)
	go func() {
		s, err := m.dial(m.ctx, name, cfg)
		ch <- dialed{s, err}
	}()
	discard := func() {
		go func() {
			if d := <-ch; d.s != nil {
				if sess := d.s.swapSession(nil); sess != nil {
					_ = sess.Close()
				}
			}
		}()
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case d := <-ch:
		return d.s, d.err
	case <-t.C:
		discard()
		return nil, fmt.Errorf("mcp %q: %s timed out after %s", name, verb, timeout)
	case <-m.ctx.Done():
		discard()
		return nil, m.ctx.Err()
	}
}

// Connect launches and connects every server in cfg. Servers that fail to
// connect are skipped (with their error collected), so one bad server does not
// abort startup. A nil Elicitor means servers cannot ask the user anything,
// which is what a headless run wants.
func Connect(ctx context.Context, cfg Config, el *Elicitor) (*Manager, []error) {
	m := &Manager{cfg: cfg, ctx: ctx, elicitor: el}
	var errs []error
	// Deterministic order for stable tool lists.
	names := make([]string, 0, len(cfg.MCPServers))
	for n := range cfg.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		srv, err := m.dialWithin(connectTimeout, "connect", name, cfg.MCPServers[name])
		if err != nil {
			errs = append(errs, err)
			// Keep a disconnected placeholder so it can be reconnected later.
			m.servers = append(m.servers, &Server{Name: name})
			continue
		}
		m.servers = append(m.servers, srv)
	}
	return m, errs
}

// Add registers an already-connected server (used by tests).
func (m *Manager) Add(s *Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = append(m.servers, s)
}

// Servers returns the connected servers.
func (m *Manager) Servers() []*Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]*Server(nil), m.servers...)
}

// serverConfig returns the launch config recorded for name.
func (m *Manager) serverConfig(name string) (ServerConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.cfg.MCPServers[name]
	return cfg, ok
}

// find returns the server with the given name, or nil.
func (m *Manager) find(name string) *Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.servers {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Disconnect closes a server's session (its tools then fail gracefully until
// reconnected). Unknown or already-disconnected servers are a no-op.
func (m *Manager) Disconnect(name string) error {
	s := m.find(name)
	if s == nil {
		return fmt.Errorf("no such MCP server %q", name)
	}
	s.disabled.Store(true)
	if prev := s.swapSession(nil); prev != nil {
		return prev.Close()
	}
	return nil
}

// Reconnect (re)establishes a server's session from its launch config, swapping
// the live session into the existing *Server pointer so registered tools resume.
func (m *Manager) Reconnect(name string) error {
	s := m.find(name)
	if s == nil {
		return fmt.Errorf("no such MCP server %q", name)
	}
	cfg, ok := m.serverConfig(name)
	if !ok {
		return fmt.Errorf("no launch config for MCP server %q", name)
	}
	s.disabled.Store(false)
	if prev := s.swapSession(nil); prev != nil {
		_ = prev.Close()
	}
	// Bound the launch+handshake so a hung server can't block the caller (the
	// TUI runs this synchronously). On timeout the server stays disconnected.
	fresh, err := m.dialWithin(reconnectTimeout, "reconnect", name, cfg)
	if err != nil {
		return err
	}
	if prev := s.swapSession(fresh.sess()); prev != nil {
		_ = prev.Close()
	}
	return nil
}

// Reload applies a freshly loaded config to the running servers: ones that are
// no longer configured are disconnected and dropped, ones whose launch config
// changed are restarted, and new ones are connected. A server whose config is
// untouched keeps its session — adding one server must not interrupt the work
// of the others, which is the whole difference between a reload and a restart.
//
// Errors are collected rather than returned early, matching Connect: one bad
// server leaves the rest running.
func (m *Manager) Reload(ctx context.Context, cfg Config) []error {
	m.mu.Lock()
	prevCfg := m.cfg
	prev := append([]*Server(nil), m.servers...)
	m.cfg = cfg
	m.mu.Unlock()

	existing := make(map[string]*Server, len(prev))
	for _, s := range prev {
		existing[s.Name] = s
	}

	names := make([]string, 0, len(cfg.MCPServers))
	for n := range cfg.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names) // deterministic order, for stable tool lists

	var errs []error
	next := make([]*Server, 0, len(names))
	for _, name := range names {
		sc := cfg.MCPServers[name]
		old, had := existing[name]
		delete(existing, name)

		// Unchanged and still answering: leave it completely alone. The config
		// comparison comes first so the ping only costs anything for a server
		// we would otherwise have kept.
		if had && reflect.DeepEqual(prevCfg.MCPServers[name], sc) && (old.alive(ctx) || old.disabled.Load()) {
			// Alive, or turned off with /mcp: either way an unrelated edit to
			// .mcp.json is no reason to touch it.
			next = append(next, old)
			continue
		}

		if had {
			if s := old.swapSession(nil); s != nil {
				_ = s.Close()
			}
		}
		fresh, err := m.dialWithin(connectTimeout, "connect", name, sc)
		if err != nil {
			errs = append(errs, err)
			// Keep a disconnected placeholder, so /mcp can retry it by hand.
			if had {
				next = append(next, old)
			} else {
				next = append(next, &Server{Name: name})
			}
			continue
		}
		if had {
			// Reuse the pointer: tool wrappers built before this reload hold
			// it, and swapping the session keeps them working.
			// Close whatever this displaces: a revive or /mcp reconnect can
			// have installed a session since the swap to nil above, and
			// dropping it orphaned that server's process.
			if displaced := old.swapSession(fresh.sess()); displaced != nil {
				_ = displaced.Close()
			}
			next = append(next, old)
			continue
		}
		next = append(next, fresh)
	}

	// Whatever is still in existing was dropped from the config.
	for _, s := range existing {
		if sess := s.swapSession(nil); sess != nil {
			_ = sess.Close()
		}
	}

	m.mu.Lock()
	m.servers = next
	m.mu.Unlock()
	return errs
}

// reconnectTimeout bounds a single /mcp reconnect attempt.
const reconnectTimeout = 10 * time.Second

// reviveCooldown is the minimum gap between automatic relaunches of one
// server. A server that cannot start — a bad command, a missing binary — would
// otherwise pay a full launch timeout on every tool call the model makes, which
// turns one misconfigured entry into a stalled session.
const reviveCooldown = 15 * time.Second

// revive brings a server back if it has died, and reports whether a usable
// session exists afterwards.
//
// This is what makes a crashed server recoverable without the user doing
// anything. A stdio server whose child process exits keeps a non-nil
// ClientSession — nothing nils it out — so until now the first sign was a tool
// call failing, and the only cure was /mcp or a restart: editing .mcp.json did
// not help, because a reload leaves an unchanged entry alone.
//
// The liveness probe comes first and decides everything. An ordinary call
// error — bad arguments, a tool that does not exist — must not restart a
// perfectly healthy server, so a relaunch happens only when the server has
// actually stopped answering.
func (m *Manager) revive(ctx context.Context, s *Server) bool {
	if s == nil {
		return false
	}
	if s.disabled.Load() {
		return false // the user turned it off; only /mcp brings it back
	}
	s.reviveMu.Lock()
	defer s.reviveMu.Unlock()

	// Re-probe under the lock: whoever held it before us may have already
	// fixed this, and a second relaunch would kill the session they just made.
	if s.alive(ctx) {
		return true
	}
	if time.Since(s.lastRevive) < reviveCooldown {
		return false
	}
	s.lastRevive = time.Now()
	if err := m.Reconnect(s.Name); err != nil {
		return false
	}
	return s.alive(ctx)
}

// livenessTimeout bounds the per-server health probe a reload runs before
// deciding an unchanged server can be left alone. It is short because it is
// paid for every surviving server on every reload, and a healthy stdio peer
// answers a ping in microseconds.
const livenessTimeout = 2 * time.Second

// Close terminates all server sessions.
func (m *Manager) Close() {
	for _, s := range m.Servers() {
		if sess := s.swapSession(nil); sess != nil {
			_ = sess.Close()
		}
	}
}
