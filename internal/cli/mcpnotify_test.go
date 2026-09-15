package cli

import (
	"sync"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/tui"
)

// The config watcher is wired before the TUI model exists, and in headless runs
// no model ever exists. Emitting into a notifier nobody has registered with is
// therefore the normal startup window, not an error path — it must not panic
// and must not block.
func TestMCPReloadNotifierWithoutListener(t *testing.T) {
	n := &mcpReloadNotifier{}
	n.emit(tui.MCPReloadEvent{ConfigErr: "boom"}) // must be a silent no-op
}

func TestMCPReloadNotifierDeliversToListener(t *testing.T) {
	n := &mcpReloadNotifier{}
	var got []tui.MCPReloadEvent
	n.register(func(ev tui.MCPReloadEvent) { got = append(got, ev) })

	n.emit(tui.MCPReloadEvent{ConfigErr: "unparseable"})
	n.emit(tui.MCPReloadEvent{ServerErrs: []string{`mcp "godot" connect: nope`}})

	if len(got) != 2 {
		t.Fatalf("listener saw %d events, want 2", len(got))
	}
	if got[0].ConfigErr != "unparseable" {
		t.Errorf("config error not delivered: %+v", got[0])
	}
	if len(got[1].ServerErrs) != 1 {
		t.Errorf("server errors not delivered: %+v", got[1])
	}
}

// register runs on the TUI's goroutine while emit runs on the watcher's, so the
// two race by construction. Guarded by -race in CI.
func TestMCPReloadNotifierIsConcurrencySafe(t *testing.T) {
	n := &mcpReloadNotifier{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			n.emit(tui.MCPReloadEvent{ConfigErr: "x"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			n.register(func(tui.MCPReloadEvent) {})
		}
	}()
	wg.Wait()
}
