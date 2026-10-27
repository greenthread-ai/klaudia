package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

var apiToolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// MCP tool names may carry dots and run to 128 characters, and server names
// are free text; the Anthropic API takes neither.
func TestQualifiedNameFitsTheAPI(t *testing.T) {
	for _, tc := range []struct{ server, tool, want string }{
		{"github", "create_issue", "mcp__github__create_issue"}, // valid names are unchanged
		{"my.server", "shot.png", "mcp__my_server__shot_png"},
	} {
		if got := qualifiedName(tc.server, tc.tool); got != tc.want {
			t.Errorf("qualifiedName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
	long := strings.Repeat("x", 120)
	a, b := qualifiedName("srv", long+"a"), qualifiedName("srv", long+"b")
	if !apiToolName.MatchString(a) || !apiToolName.MatchString(b) {
		t.Errorf("long names do not fit: %q %q", a, b)
	}
	if a == b {
		t.Error("two long names truncated to the same qualified name")
	}
}

// /mcp Disconnect used to last until the model's next call, which revived the
// server. It now holds until /mcp Reconnect.
func TestDisconnectIsNotUndoneByTheNextCall(t *testing.T) {
	h := newReviveHarness(t, true)
	tl := h.m.Tools(context.Background())
	if len(tl) == 0 {
		t.Fatal("no tools")
	}
	if err := h.m.Disconnect("testsrv"); err != nil {
		t.Fatal(err)
	}
	res, err := tl[0].Execute(context.Background(), tools.Context{}, json.RawMessage(`{"message":"hi"}`))
	if err != nil || len(res) == 0 || !res[0].IsError {
		t.Fatalf("a call after Disconnect should fail gracefully: %+v %v", res, err)
	}
	if h.srv.Connected() || h.launches.Load() != 0 {
		t.Fatalf("the call revived a disconnected server (launches=%d)", h.launches.Load())
	}
	if err := h.m.Reconnect("testsrv"); err != nil {
		t.Fatal(err)
	}
	res, _ = tl[0].Execute(context.Background(), tools.Context{}, json.RawMessage(`{"message":"hi"}`))
	if len(res) == 0 || res[0].IsError {
		t.Fatalf("Reconnect should bring it back: %+v", res)
	}
}

// A dial that outlives its timeout is abandoned, and the session it produces
// later is closed rather than leaked.
func TestDialWithinTimesOutAndClosesTheLateSession(t *testing.T) {
	h := newReviveHarness(t, true)
	release := make(chan struct{})
	late := make(chan *Server, 1)
	h.m.mu.Lock()
	inMemory := h.m.connect
	h.m.connect = func(ctx context.Context, name string, cfg ServerConfig, el *Elicitor) (*Server, error) {
		<-release
		s, err := inMemory(ctx, name, cfg, el)
		late <- s
		return s, err
	}
	h.m.mu.Unlock()
	if _, err := h.m.dialWithin(50*time.Millisecond, "connect", "testsrv", ServerConfig{}); err == nil || !strings.Contains(err.Error(), "connect timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	close(release)
	s := <-late
	deadline := time.Now().Add(2 * time.Second)
	for s.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("the late session was left open")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
