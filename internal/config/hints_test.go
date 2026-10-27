package config

import (
	"net"
	"strings"
	"testing"
)

// Klaudia's frontends render errors with these hints, so its users keep being
// pointed at its own config file.
func TestHintsPointAtKlaudiasConfig(t *testing.T) {
	msg := Hints.FriendlyError(&net.DNSError{Err: "no such host", Name: "api.example.com"})
	if !strings.Contains(msg, "~/.klaudia/config.toml") {
		t.Errorf("message = %q, want it to name Klaudia's config", msg)
	}
}
