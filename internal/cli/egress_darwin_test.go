//go:build darwin

package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// Under the macOS OS sandbox the allowlist is enforced, not advised: an
// allowed host answers through the proxy, a refused one gets the policy's
// 403, and a command that ignores the proxy cannot connect at all.
func TestEgressIsEnforcedUnderSeatbelt(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("no sandbox-exec")
	}
	serve := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }))
	}
	allowed, other := serve("allowed ok"), serve("should not be reached")
	defer allowed.Close()
	defer other.Close()
	defer sandbox.SetEgress(nil, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sb := config.Sandbox{Mode: "os", Egress: []string{strings.TrimPrefix(allowed.URL, "http://")}}
	exe := sandbox.NewSeatbelt(nil, "")
	if p := startEgress(ctx, sb, exe, func(m string) { t.Log(m) }); p == nil {
		t.Fatal("proxy not started")
	}
	if got := egressReach(sb, exe); got != "enforced" {
		t.Fatalf("reach = %q", got)
	}
	run := func(cmd string) string {
		resp, err := exe.Run(ctx, sandbox.Request{Command: cmd})
		if err != nil {
			t.Fatal(err)
		}
		return resp.Stdout + resp.Stderr
	}
	if out := run("curl -s " + allowed.URL); out != "allowed ok" {
		t.Errorf("allowed host: %q", out)
	}
	if out := run("curl -s " + other.URL); !strings.Contains(out, "not in the allowlist") {
		t.Errorf("refused host: %q", out)
	}
	if out := run("curl -s --noproxy '*' " + other.URL + "; echo exit=$?"); strings.Contains(out, "should not be reached") || !strings.Contains(out, "exit=7") {
		t.Errorf("a command ignoring the proxy connected: %q", out)
	}
}
