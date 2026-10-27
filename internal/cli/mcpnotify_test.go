package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/mcp"
)

// The config watcher is wired before the frontend exists, and a frontend may
// never register at all. Emitting into a notifier nobody has registered with is
// therefore the normal startup window, not an error path — it must not panic
// and must not block.
func TestMCPReloadNotifierWithoutListener(t *testing.T) {
	n := &mcpReloadNotifier{}
	n.emit(mcp.ReloadEvent{ConfigErr: "boom"}) // must be a silent no-op
}

func TestMCPReloadNotifierDeliversToListener(t *testing.T) {
	n := &mcpReloadNotifier{}
	var got []mcp.ReloadEvent
	n.register(func(ev mcp.ReloadEvent) { got = append(got, ev) })

	n.emit(mcp.ReloadEvent{ConfigErr: "unparseable"})
	n.emit(mcp.ReloadEvent{ServerErrs: []string{`mcp "godot" connect: nope`}})

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
			n.emit(mcp.ReloadEvent{ConfigErr: "x"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			n.register(func(mcp.ReloadEvent) {})
		}
	}()
	wg.Wait()
}

// Non-TUI modes had nowhere to put a reload failure, so the watcher built the
// event and the notifier discarded it: a typo in .mcp.json was indistinguishable
// from a clean reload until a tool call failed for an unrelated-looking reason.
func TestMCPReloadWriter(t *testing.T) {
	tests := []struct {
		name   string
		event  mcp.ReloadEvent
		want   []string
		silent bool
	}{
		{
			name:   "a reload that worked says nothing",
			event:  mcp.ReloadEvent{},
			silent: true,
		},
		{
			name:  "an unparseable config says the old servers survived",
			event: mcp.ReloadEvent{ConfigErr: "invalid character '}'"},
			// Without the second half the natural reading is that MCP is now
			// down, and the useful fact is the opposite.
			want: []string{"invalid character '}'", "still running"},
		},
		{
			name: "every failed server is named",
			event: mcp.ReloadEvent{ServerErrs: []string{
				`mcp "godot" connect: exec: "npx": not found`,
				`mcp "db" connect: dial tcp: refused`,
			}},
			want: []string{"2 servers failed", `"godot"`, `"db"`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			mcpReloadWriter(&buf)(tc.event)
			got := buf.String()
			if tc.silent {
				if got != "" {
					t.Errorf("a successful reload printed %q", got)
				}
				return
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("output %q does not mention %q", got, want)
				}
			}
		})
	}
}
