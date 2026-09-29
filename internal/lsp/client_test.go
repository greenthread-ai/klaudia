package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// goFile writes a Go source file into a fresh workspace and returns its path.
func goFile(t *testing.T, body string) (root, path string) {
	t.Helper()
	root = t.TempDir()
	path = filepath.Join(root, "main.go")
	write(t, path, body)
	return root, path
}

// fakePool is a Pool whose "go" server is the scripted fake.
func fakePool(t *testing.T, script, root string) (*Pool, func() []string) {
	t.Helper()
	bin, events := useFakeLSP(t, script)
	p := NewPool(context.Background(), root, nil, map[string]ServerSpec{"go": {Bin: bin}})
	t.Cleanup(p.Close)
	return p, events
}

func waitForEvent(t *testing.T, events func() []string, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Contains(events(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server never logged %q; events: %q", want, events())
}

func TestPoolDiagnosticsReturnsWhatTheServerPublishes(t *testing.T) {
	root, path := goFile(t, "package main\n\nfunc main() {\n\tx := 1\n}\n")
	p, events := fakePool(t, "ok", root)

	diags, err := p.Diagnostics(context.Background(), path)
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diags) != 1 || diags[0].Message != "declared and not used: x" ||
		diags[0].Severity != 1 || diags[0].Range.Start.Line != 3 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	// The server was handed the file's on-disk contents, and the handshake was
	// completed before it was.
	got := events()
	if !slices.Contains(got, "initialized") || !slices.Contains(got, `didOpen "package main\n\nfunc main() {\n\tx := 1\n}\n"`) {
		t.Errorf("server events = %q", got)
	}
	waitForEvent(t, events, "didClose")
}

// A server-to-client request the client doesn't implement is answered with
// null, so a server that blocks on it (gopls' workspace/configuration) isn't
// left waiting forever.
func TestClientAnswersUnimplementedServerRequestsWithNull(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, events := fakePool(t, "ok", root)
	if _, err := p.Diagnostics(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, "reply 900 null")
}

func TestPoolDefinitionAcceptsASingleLocation(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, _ := fakePool(t, "ok", root)

	locs, err := p.Definition(context.Background(), path, Position{Line: 4, Character: 2})
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 14 {
		t.Fatalf("locations = %+v, want one at line 14", locs)
	}
	if got := URIPath(locs[0].URI); got != path {
		t.Errorf("location path = %q, want %q", got, path)
	}
}

func TestPoolReferencesAsksForTheDeclarationToo(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, _ := fakePool(t, "ok", root)

	// The fake refuses a references request without includeDeclaration.
	locs, err := p.References(context.Background(), path, Position{Line: 1})
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(locs) != 2 || locs[1].URI != "file:///elsewhere.go" || locs[1].Range.Start.Line != 7 {
		t.Fatalf("locations = %+v", locs)
	}
}

// One server per language per session: later calls reuse the running client
// instead of spawning (and re-indexing) again. Close shuts it down politely.
func TestPoolReusesTheServerAndShutsItDownOnClose(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, events := fakePool(t, "ok", root)
	ctx := context.Background()

	if _, err := p.Diagnostics(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Definition(ctx, path, Position{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.References(ctx, path, Position{}); err != nil {
		t.Fatal(err)
	}
	p.Close()

	var starts int
	for _, e := range events() {
		if e == "start" {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("server started %d times, want 1", starts)
	}
	// Close waits for the server to exit on its own before killing it, so the
	// exit notification is always read. (It used to kill straight after
	// sending it, and this check failed about 2 runs in 5 under -race.)
	got := events()
	if !slices.Contains(got, "shutdown") || !slices.Contains(got, "exit") {
		t.Errorf("Close did not send shutdown+exit; events = %q", got)
	}

	// After Close the pool starts afresh rather than handing out a dead client.
	if _, err := p.Definition(ctx, path, Position{}); err != nil {
		t.Fatalf("Definition after Close: %v", err)
	}
	starts = 0
	for _, e := range events() {
		if e == "start" {
			starts++
		}
	}
	if starts != 2 {
		t.Errorf("server started %d times after Close+call, want 2", starts)
	}
}

func TestPoolReportsAFailedHandshake(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, _ := fakePool(t, "init-error", root)

	_, err := p.Diagnostics(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "initialize") || !strings.Contains(err.Error(), "cannot index this workspace") {
		t.Fatalf("err = %v, want the initialize failure with the server's message", err)
	}
	p.mu.Lock()
	kept := len(p.clients)
	p.mu.Unlock()
	if kept != 0 {
		t.Error("a client whose handshake failed was kept in the pool")
	}
}

// A server that dies mid-request fails the pending call instead of hanging it,
// and later calls on the dead client fail fast too (with "closed" or the
// broken pipe, depending on whether the read loop has noticed yet).
func TestClientFailsPendingCallsWhenTheServerExits(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, _ := fakePool(t, "crash-on-def", root)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := p.Definition(ctx, path, Position{})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the server's exit reported promptly", err)
	}
	_, err = p.Definition(ctx, path, Position{})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("second call err = %v, want a prompt failure", err)
	}
}

// A server that dies is replaced on the next call rather than handed out dead
// for the rest of the session.
func TestPoolReplacesADeadServer(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, events := fakePool(t, "crash-on-def", root)
	ctx := context.Background()

	if _, err := p.Definition(ctx, path, Position{}); err == nil {
		t.Fatal("Definition against a crashing server succeeded")
	}
	// The crash is noticed by the read loop; wait for it rather than racing it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.mu.Lock()
		c := p.clients["go"]
		p.mu.Unlock()
		if c == nil || !c.Alive() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pool never noticed the server exit")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// This server crashes on every definition request too, so the call fails
	// either way. What matters is which server it reached: a fresh one (two
	// starts), not the dead one ("lsp client closed", one start).
	_, _ = p.Definition(ctx, path, Position{})
	starts := 0
	for _, e := range events() {
		if e == "start" {
			starts++
		}
	}
	if starts != 2 {
		t.Errorf("server started %d times, want 2 (the dead one replaced)", starts)
	}
}

// Diagnostics waiting on a server that dies returns at once with an error,
// instead of sitting out diagnosticsWait for a publish that can never come.
func TestDiagnosticsFailsPromptlyWhenTheServerDies(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, events := fakePool(t, "mute", root) // never publishes diagnostics
	c, spec, err := p.clientFor(path)
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		_, err := c.Diagnostics(context.Background(), path, spec.LanguageID)
		done <- result{err, time.Since(start)}
	}()
	waitForEvent(t, events, `didOpen "package main\n"`) // the call is now waiting
	_ = c.cmd.Process.Kill()

	select {
	case r := <-done:
		if r.err == nil {
			t.Error("Diagnostics on a dead server returned no error")
		}
		if r.elapsed > 2*time.Second {
			t.Errorf("Diagnostics took %s to notice the server died", r.elapsed)
		}
	case <-time.After(diagnosticsWait / 2):
		t.Fatal("Diagnostics kept waiting after the server died")
	}
}

// Close lets the server act on the exit notification before killing it.
func TestCloseLetsTheServerExitOnItsOwn(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, events := fakePool(t, "slow-exit", root)
	if _, err := p.Definition(context.Background(), path, Position{}); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if got := events(); !slices.Contains(got, "exit") {
		t.Errorf("the server was killed before it could exit; events = %q", got)
	}
}

// However Diagnostics returns, its waiter is unregistered.
func TestDiagnosticsLeavesNoWaiterBehind(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, _ := fakePool(t, "mute", root)
	c, spec, err := p.clientFor(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Diagnostics(ctx, path, spec.LanguageID); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	c.mu.Lock()
	n := len(c.diagCh)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("%d diagnostics waiter(s) left registered after a cancelled call", n)
	}
}

func TestClientCallHonoursContextCancellation(t *testing.T) {
	root, path := goFile(t, "package main\n")
	p, _ := fakePool(t, "mute", root)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := p.Definition(ctx, path, Position{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Definition err = %v, want deadline exceeded", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	if _, err := p.Diagnostics(ctx2, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Diagnostics err = %v, want deadline exceeded", err)
	}
	p.mu.Lock()
	c := p.clients["go"]
	p.mu.Unlock()
	c.mu.Lock()
	pending := len(c.pending)
	c.mu.Unlock()
	if pending != 0 {
		t.Error("an abandoned request was left registered as pending")
	}
}

func TestClientReportsUnreadableFiles(t *testing.T) {
	root, _ := goFile(t, "package main\n")
	p, _ := fakePool(t, "ok", root)
	missing := filepath.Join(root, "gone.go")

	if _, err := p.Diagnostics(context.Background(), missing); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Diagnostics err = %v, want not-exist", err)
	}
	if _, err := p.Definition(context.Background(), missing, Position{}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Definition err = %v, want not-exist", err)
	}
}

func TestNewClientReportsAnUnstartableBinary(t *testing.T) {
	_, err := NewClient(context.Background(), filepath.Join(t.TempDir(), "no-such-server"))
	if err == nil || !strings.Contains(err.Error(), "start") {
		t.Fatalf("err = %v, want a start error", err)
	}
}
