package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A reload triggered by adding one server must not disturb the servers that
// were already running: restarting them would drop their state and stall any
// work in flight, which is the difference between a reload and a restart.
func TestReloadLeavesUnchangedServerAlone(t *testing.T) {
	m, cleanup := startTestServer(t)
	defer cleanup()
	ctx := context.Background()

	cfg := Config{MCPServers: map[string]ServerConfig{"testsrv": {Command: "testsrv"}}}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()

	before := m.Servers()[0].sess()
	if errs := m.Reload(ctx, cfg); len(errs) != 0 {
		t.Fatalf("reload with an identical config reported errors: %v", errs)
	}

	servers := m.Servers()
	if len(servers) != 1 {
		t.Fatalf("server count changed: %d", len(servers))
	}
	if got := servers[0].sess(); got != before {
		t.Error("an unchanged server was torn down and reconnected")
	}
	if !servers[0].Connected() {
		t.Error("an unchanged server lost its session")
	}
	// The session is not merely present, it still works.
	if tl := m.Tools(ctx); len(tl) == 0 {
		t.Error("no tools after reload; the session is connected but unusable")
	}
}

// Removing a server from the config removes it from the session, rather than
// leaving a dead entry that /mcp still lists.
func TestReloadDropsServerRemovedFromConfig(t *testing.T) {
	m, cleanup := startTestServer(t)
	defer cleanup()
	ctx := context.Background()

	m.mu.Lock()
	m.cfg = Config{MCPServers: map[string]ServerConfig{"testsrv": {Command: "testsrv"}}}
	m.mu.Unlock()

	if errs := m.Reload(ctx, Config{MCPServers: map[string]ServerConfig{}}); len(errs) != 0 {
		t.Fatalf("dropping a server reported errors: %v", errs)
	}
	if got := m.Servers(); len(got) != 0 {
		t.Errorf("server survived removal from the config: %v", got)
	}
	if tl := m.Tools(ctx); len(tl) != 0 {
		t.Errorf("tools of a removed server are still offered: %d", len(tl))
	}
}

// A server that cannot be launched must not take the reload down with it: the
// error is reported and a disconnected entry remains, so /mcp can retry it.
func TestReloadReportsUnlaunchableServerAndKeepsPlaceholder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m := &Manager{ctx: ctx}

	errs := m.Reload(ctx, Config{MCPServers: map[string]ServerConfig{
		"broken": {Command: "klaudia-no-such-binary-exists-here"},
	}})
	if len(errs) == 0 {
		t.Fatal("an unlaunchable server reloaded without an error")
	}
	servers := m.Servers()
	if len(servers) != 1 || servers[0].Name != "broken" {
		t.Fatalf("want a placeholder entry for the failed server, got %v", servers)
	}
	if servers[0].Connected() {
		t.Error("a server that failed to launch is reported as connected")
	}
}

// Changing a server's launch config restarts it — otherwise editing a command
// or an env var appears to do nothing until the next restart, which is exactly
// the complaint hot reload exists to answer.
func TestReloadRestartsServerWhoseConfigChanged(t *testing.T) {
	m, cleanup := startTestServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	m.mu.Lock()
	m.ctx = ctx
	m.cfg = Config{MCPServers: map[string]ServerConfig{"testsrv": {Command: "testsrv"}}}
	m.mu.Unlock()

	before := m.Servers()[0].sess()
	if before == nil {
		t.Fatal("fixture server is not connected")
	}

	// Same name, different command: the old session must go, and since the new
	// command cannot launch, the server ends up disconnected rather than
	// silently still running the old one.
	errs := m.Reload(ctx, Config{MCPServers: map[string]ServerConfig{
		"testsrv": {Command: "klaudia-no-such-binary-exists-here"},
	}})
	if len(errs) == 0 {
		t.Fatal("expected the failed relaunch to be reported")
	}
	servers := m.Servers()
	if len(servers) != 1 {
		t.Fatalf("server count changed: %d", len(servers))
	}
	if servers[0].sess() == before {
		t.Error("config changed but the old session is still in place")
	}
	if servers[0].Connected() {
		t.Error("server reports connected after a failed relaunch")
	}
}

// The end-to-end shape of the feature: a config file appears on disk, and the
// servers it names become live without restarting the process.
func TestReloadPicksUpAServerAddedToTheGlobalConfig(t *testing.T) {
	root := isolateConfigRoot(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	m := &Manager{ctx: ctx}
	if got := len(m.Servers()); got != 0 {
		t.Fatalf("fresh manager already has %d servers", got)
	}

	os.WriteFile(filepath.Join(root, ".mcp.json"),
		[]byte(`{"mcpServers":{"godot":{"command":"klaudia-no-such-binary-exists-here"}}}`), 0o644)

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Reload(ctx, cfg)

	servers := m.Servers()
	if len(servers) != 1 || servers[0].Name != "godot" {
		t.Fatalf("the server from the global config never arrived: %v", servers)
	}
}
