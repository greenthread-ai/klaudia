// Package mcp connects to Model Context Protocol servers over stdio and
// exposes their tools and resources to Klaudia. It wraps the official
// modelcontextprotocol/go-sdk client.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread/klaudia/internal/version"
)

// ServerConfig defines how to launch a stdio MCP server.
type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Config is the .mcp.json shape: a map of server name → launch config.
type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// LoadConfig reads .mcp.json from dir. A missing file yields an empty config
// (not an error). A project .klaudia/.mcp.json overrides ./.mcp.json per server,
// so MCP servers can be locally overridden in the .klaudia folder.
func LoadConfig(dir string) (Config, error) {
	cfg := Config{MCPServers: map[string]ServerConfig{}}
	for _, p := range []string{
		filepath.Join(dir, ".mcp.json"),          // base
		filepath.Join(dir, ".klaudia", ".mcp.json"), // local override (wins)
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cfg, err
		}
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			return cfg, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		for name, sc := range c.MCPServers {
			cfg.MCPServers[name] = sc
		}
	}
	return cfg, nil
}

// Server is a connected MCP server session and its configured name.
type Server struct {
	Name    string
	session *mcpsdk.ClientSession
}

// Connected reports whether the server currently has a live session.
func (s *Server) Connected() bool { return s != nil && s.session != nil }

func newClient() *mcpsdk.Client {
	return mcpsdk.NewClient(&mcpsdk.Implementation{Name: "klaudia", Version: version.Version}, nil)
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
		srv, err := ConnectCommand(ctx, name, cfg.MCPServers[name])
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
func (m *Manager) Add(s *Server) { m.servers = append(m.servers, s) }

// Servers returns the connected servers.
func (m *Manager) Servers() []*Server { return m.servers }

// find returns the server with the given name, or nil.
func (m *Manager) find(name string) *Server {
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
	if s.session != nil {
		err := s.session.Close()
		s.session = nil
		return err
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
	cfg, ok := m.cfg.MCPServers[name]
	if !ok {
		return fmt.Errorf("no launch config for MCP server %q", name)
	}
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
	// Bound the launch+handshake so a hung server can't block the caller (the
	// TUI runs this synchronously). On timeout the server stays disconnected.
	ctx, cancel := context.WithTimeout(m.ctx, reconnectTimeout)
	defer cancel()
	fresh, err := ConnectCommand(ctx, name, cfg)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("mcp %q: reconnect timed out after %s", name, reconnectTimeout)
		}
		return err
	}
	s.session = fresh.session
	return nil
}

// reconnectTimeout bounds a single /mcp reconnect attempt.
const reconnectTimeout = 10 * time.Second

// Close terminates all server sessions.
func (m *Manager) Close() {
	for _, s := range m.servers {
		if s.session != nil {
			_ = s.session.Close()
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
