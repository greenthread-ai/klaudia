package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitForChange waits for one reload signal, failing the test if none arrives.
func waitForChange(t *testing.T, ch <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("no reload signal: %s", why)
	}
}

func TestWatchNoticesGlobalConfigAppearing(t *testing.T) {
	root := isolateConfigRoot(t)
	dir := t.TempDir()

	changed := make(chan struct{}, 16)
	stop, err := watchWithDebounce(dir, 20*time.Millisecond, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// The file does not exist when the watch starts — installing a server for
	// the first time mid-session is the case that matters most.
	os.WriteFile(filepath.Join(root, ".mcp.json"),
		[]byte(`{"mcpServers":{"godot":{"command":"godot-mcp"}}}`), 0o644)

	waitForChange(t, changed, "a global .mcp.json was created")
}

func TestWatchNoticesProjectConfigChange(t *testing.T) {
	isolateConfigRoot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o644)

	changed := make(chan struct{}, 16)
	stop, err := watchWithDebounce(dir, 20*time.Millisecond, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	os.WriteFile(path, []byte(`{"mcpServers":{"x":{"command":"x"}}}`), 0o644)
	waitForChange(t, changed, "the project .mcp.json was rewritten")
}

// The reason the watch is neither recursive nor attached to the whole config
// directory: ~/.klaudia holds sessions/, jobs/ and browser/, all written
// continuously by the running session. If those woke the watcher, every
// message typed would tear down and rebuild the MCP servers.
func TestWatchIgnoresTheRestOfTheConfigDir(t *testing.T) {
	root := isolateConfigRoot(t)
	dir := t.TempDir()

	changed := make(chan struct{}, 16)
	stop, err := watchWithDebounce(dir, 20*time.Millisecond, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// Exactly the traffic a live session generates in there.
	os.MkdirAll(filepath.Join(root, "sessions", "proj"), 0o755)
	os.WriteFile(filepath.Join(root, "sessions", "proj", "log.jsonl"), []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("model = \"opus\"\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "jobs"), 0o755)
	os.WriteFile(filepath.Join(root, "jobs", "dev.log"), []byte("listening\n"), 0o644)

	select {
	case <-changed:
		t.Fatal("ordinary session writes under the config dir triggered an MCP reload")
	case <-time.After(500 * time.Millisecond):
	}
}

// A single save can reach the filesystem as several operations. One reload per
// save is the contract; one per write syscall would reconnect servers against
// a half-written file.
func TestWatchCoalescesRapidWrites(t *testing.T) {
	isolateConfigRoot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")

	changed := make(chan struct{}, 32)
	stop, err := watchWithDebounce(dir, 150*time.Millisecond, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	for i := 0; i < 6; i++ {
		os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o644)
	}
	waitForChange(t, changed, "a burst of writes")

	// Let any stragglers land, then confirm the burst did not become a storm.
	time.Sleep(400 * time.Millisecond)
	if extra := len(changed); extra > 1 {
		t.Errorf("six writes produced %d reloads beyond the first; debounce is not coalescing", extra)
	}
}

func TestWatchStopIsIdempotent(t *testing.T) {
	isolateConfigRoot(t)
	stop, err := watchWithDebounce(t.TempDir(), 20*time.Millisecond, func() {})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	stop() // must not panic on a second close
}
