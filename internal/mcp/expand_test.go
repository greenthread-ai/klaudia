package mcp

import (
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// TestExpandRefs pins the two accepted forms and what is deliberately left
// alone. The first row is the config from the report: the server was being
// spawned with the literal "${MSP_LOKI_URL:-http://loki:3100}" in its
// environment.
func TestExpandRefs(t *testing.T) {
	env := lookupFrom(map[string]string{
		"HOST":  "real-host",
		"EMPTY": "",
	})
	tests := []struct {
		in, want string
	}{
		{"${MSP_LOKI_URL:-http://loki:3100}", "http://loki:3100"}, // unset → default
		{"${HOST:-fallback}", "real-host"},                        // set → value, default ignored
		{"${HOST}", "real-host"},
		{"http://${HOST}:3100/loki", "http://real-host:3100/loki"}, // embedded
		{"${HOST}-${HOST}", "real-host-real-host"},                 // several
		{"${EMPTY:-fallback}", ""},                                 // set-but-empty is set, as in sh
		{"${UNSET:-}", ""},                                         // explicit empty default
		{"${UNSET:-a:-b}", "a:-b"},                                 // default may itself contain ":-"
		{"plain", "plain"},
		{"$HOST", "$HOST"},                                   // bare form is not expanded
		{"${", "${"},                                         // unterminated: left alone
		{"${}", "${}"},                                       // empty name: left alone
		{"${1BAD}", "${1BAD}"},                               // not a variable name: left alone
		{"${not a name} ${HOST}", "${not a name} real-host"}, // scanning continues past junk
	}
	for _, tt := range tests {
		got, err := expandRefs(tt.in, env)
		if err != nil {
			t.Errorf("expandRefs(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("expandRefs(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpandRefsUnsetWithoutDefaultIsAnError(t *testing.T) {
	_, err := expandRefs("http://${NOPE}:3100", lookupFrom(nil))
	if err == nil {
		t.Fatal("want error for unset variable with no default")
	}
	for _, want := range []string{"${NOPE}", "not set", "${NOPE:-value}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// TestExpandServerConfigCoversEveryOutwardField: command, args, env and url
// all reach the outside world and all expand; readOnly and type are ours and
// do not. The input is not mutated — Reload compares the raw config across
// reloads, and expanding in place would make an unchanged file look changed.
func TestExpandServerConfigCoversEveryOutwardField(t *testing.T) {
	env := lookupFrom(map[string]string{"BIN": "/opt/srv", "TOKEN": "s3cret", "HOST": "h"})
	in := ServerConfig{
		Command: "${BIN}/mcp",
		Args:    []string{"--token", "${TOKEN}", "--port", "${PORT:-3100}"},
		Env:     map[string]string{"URL": "http://${HOST}:${PORT:-3100}", "PLAIN": "x"},
		URL:     "https://${HOST}/mcp",
		Type:    "${NOT_A_FIELD_WE_EXPAND}",
	}
	got, err := expandServerConfig("srv", in, env)
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "/opt/srv/mcp" {
		t.Errorf("command = %q", got.Command)
	}
	if want := []string{"--token", "s3cret", "--port", "3100"}; strings.Join(got.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %q, want %q", got.Args, want)
	}
	if got.Env["URL"] != "http://h:3100" || got.Env["PLAIN"] != "x" {
		t.Errorf("env = %v", got.Env)
	}
	if got.URL != "https://h/mcp" {
		t.Errorf("url = %q", got.URL)
	}
	if got.Type != in.Type {
		t.Errorf("type was expanded: %q", got.Type)
	}
	// Input untouched.
	if in.Command != "${BIN}/mcp" || in.Args[1] != "${TOKEN}" || in.Env["URL"] != "http://${HOST}:${PORT:-3100}" {
		t.Errorf("input mutated: %+v", in)
	}
}

// The error names the server, the field and the variable, because "which of
// my four servers, and which of its six settings" is the whole question.
func TestExpandServerConfigErrorNamesServerAndField(t *testing.T) {
	_, err := expandServerConfig("loki", ServerConfig{
		Command: "python",
		Env:     map[string]string{"MSP_LOKI_URL": "${MSP_LOKI_URL}"},
	}, lookupFrom(nil))
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{`mcp "loki"`, "env MSP_LOKI_URL", "${MSP_LOKI_URL}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// An unresolvable reference fails that server's connect, and only that
// server's: connectServer is per server and Connect/Reload collect errors.
func TestConnectServerRefusesUnresolvableReference(t *testing.T) {
	_, err := connectServer(t.Context(), "loki", ServerConfig{
		Command: "true",
		Env:     map[string]string{"X": "${KLAUDIA_TEST_DEFINITELY_UNSET_VAR_7f3a}"},
	})
	if err == nil || !strings.Contains(err.Error(), "KLAUDIA_TEST_DEFINITELY_UNSET_VAR_7f3a") {
		t.Fatalf("err = %v, want unresolved-variable error naming the variable", err)
	}
}
