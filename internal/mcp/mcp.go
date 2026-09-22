// Package mcp connects to Model Context Protocol servers over stdio or HTTP
// (streamable / SSE) and exposes their tools and resources to Klaudia. It wraps
// the official modelcontextprotocol/go-sdk client.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/internal/version"
)

// ServerConfig defines how to reach an MCP server. A stdio server sets
// Command (+ Args/Env); an HTTP server sets URL (Type selects the streamable
// HTTP transport, the default, or "sse" for the legacy SSE transport).
//
// Command, Args, Env values and URL may reference Klaudia's environment as
// ${VAR} or ${VAR:-default}, the syntax the reference MCP clients accept in
// .mcp.json. They are resolved at connect time, not at load: the stored config
// stays as written, so a reload can tell an unchanged file from a changed one.
type ServerConfig struct {
	// stdio transport
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// HTTP transport
	Type string `json:"type,omitempty"` // "http" (default when URL set) | "sse"
	URL  string `json:"url,omitempty"`
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
// The global scope is what makes a personal server — one you want in every
// project rather than in one repo — installable once. Without it the only
// answer was to copy the same file into every checkout.
func LoadConfig(dir string) (Config, error) {
	cfg := Config{MCPServers: map[string]ServerConfig{}}
	for _, p := range ConfigPaths(dir) {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cfg, err
		}
		var c Config
		if err := json.Unmarshal(stripJSONComments(data), &c); err != nil {
			// The full path, not the base name: three files share the name
			// ".mcp.json", and "which one is broken" is the entire question
			// when the answer is a config in a different directory.
			return cfg, fmt.Errorf("%s: %w", p, err)
		}
		for name, sc := range c.MCPServers {
			cfg.MCPServers[name] = sc
		}
	}
	return cfg, nil
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

func newClient() *mcpsdk.Client {
	return mcpsdk.NewClient(&mcpsdk.Implementation{Name: "klaudia", Version: version.Version}, nil)
}

// connectServer connects to a server using the transport its config implies:
// HTTP (streamable, or SSE) when URL is set, otherwise stdio. ${VAR} and
// ${VAR:-default} references in the config are resolved against Klaudia's
// environment first (see expandServerConfig); an unresolvable one is this
// server's error and leaves the others alone.
func connectServer(ctx context.Context, name string, cfg ServerConfig) (*Server, error) {
	cfg, err := expandServerConfig(name, cfg, os.LookupEnv)
	if err != nil {
		return nil, err
	}
	if url := strings.TrimSpace(cfg.URL); url != "" {
		var t mcpsdk.Transport
		switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
		case "sse":
			t = &mcpsdk.SSEClientTransport{Endpoint: url}
		default: // "http" / "streamable" / unset
			t = &mcpsdk.StreamableClientTransport{Endpoint: url}
		}
		return ConnectTransport(ctx, name, t)
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp %q: config has neither command (stdio) nor url (http)", name)
	}
	return ConnectCommand(ctx, name, cfg)
}

// ConnectCommand spawns a stdio MCP server and connects to it.
func ConnectCommand(ctx context.Context, name string, cfg ServerConfig) (*Server, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return ConnectTransport(ctx, name, &mcpsdk.CommandTransport{Command: cmd})
}

// ConnectTransport connects to a server over an arbitrary transport (used by
// command servers and by tests via an in-memory transport).
func ConnectTransport(ctx context.Context, name string, t mcpsdk.Transport) (*Server, error) {
	session, err := newClient().Connect(ctx, t, nil)
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
}

// Connect launches and connects every server in cfg. Servers that fail to
// connect are skipped (with their error collected), so one bad server does not
// abort startup.
func Connect(ctx context.Context, cfg Config) (*Manager, []error) {
	m := &Manager{cfg: cfg, ctx: ctx}
	var errs []error
	// Deterministic order for stable tool lists.
	names := make([]string, 0, len(cfg.MCPServers))
	for n := range cfg.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		srv, err := connectServer(ctx, name, cfg.MCPServers[name])
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
	if prev := s.swapSession(nil); prev != nil {
		_ = prev.Close()
	}
	// Bound the launch+handshake so a hung server can't block the caller (the
	// TUI runs this synchronously). On timeout the server stays disconnected.
	ctx, cancel := context.WithTimeout(m.ctx, reconnectTimeout)
	defer cancel()
	fresh, err := connectServer(ctx, name, cfg)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("mcp %q: reconnect timed out after %s", name, reconnectTimeout)
		}
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
		if had && reflect.DeepEqual(prevCfg.MCPServers[name], sc) && old.alive(ctx) {
			next = append(next, old)
			continue
		}

		if had {
			if s := old.swapSession(nil); s != nil {
				_ = s.Close()
			}
		}
		fresh, err := connectServer(ctx, name, sc)
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
			old.swapSession(fresh.sess())
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

// textOf concatenates the text content blocks of an MCP result.
func textOf(content []mcpsdk.Content) string {
	var out string
	for _, c := range content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			if out != "" {
				out += "\n"
			}
			out += tc.Text
		}
	}
	return out
}
