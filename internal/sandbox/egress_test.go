package sandbox

import (
	"strings"
	"testing"
)

// With egress set, every command is pointed at the proxy — over a user's own
// proxy settings — and the macOS profile lets it reach the proxy only.
func TestEgressReachesEnvAndProfile(t *testing.T) {
	defer SetEgress(nil, 0)
	SetEgress([]string{"HTTPS_PROXY=http://127.0.0.1:4000", "NO_PROXY="}, 4000)
	env := strings.Join(childEnv(Request{}), "\n")
	if !strings.Contains(env, "HTTPS_PROXY=http://127.0.0.1:4000") {
		t.Error("child env lacks the proxy")
	}
	prof := NewSeatbelt(nil, "").profile(Request{})
	if !strings.Contains(prof, "(deny network-outbound)") || !strings.Contains(prof, `(remote ip "localhost:4000")`) {
		t.Errorf("profile:\n%s", prof)
	}
	if p := NewSeatbelt(nil, "none").profile(Request{}); strings.Contains(p, "localhost:4000") {
		t.Error("network = none must stay a full deny")
	}
}
