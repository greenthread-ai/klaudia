package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Flags and configuration that only mean something once they have gone
// through the binary: parsed, loaded, and turned into a request or a file.

// --create-config writes a starter config in the named scope, prints where,
// and refuses to overwrite one that exists. It needs no model.
func TestCreateConfigGlobalAndLocal(t *testing.T) {
	for _, tc := range []struct {
		scope string
		path  func(e *Env) string
	}{
		{"global", func(e *Env) string { return filepath.Join(e.Home, ".klaudia", "config.toml") }},
		{"local", func(e *Env) string { return e.Path(".klaudia", "config.toml") }},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			e := NewEnv(t, nil)
			r := e.Run("--create-config", tc.scope)
			want := tc.path(e)
			if r.ExitCode != 0 || !strings.Contains(r.Stdout, "Created "+want) {
				t.Fatalf("want 'Created %s'\n%s", want, r.dump())
			}
			body, err := os.ReadFile(want)
			if err != nil || !strings.Contains(string(body), `provider = "openai"`) {
				t.Fatalf("starter config = %q, %v", body, err)
			}
			again := e.Run("--create-config", tc.scope)
			if again.ExitCode != 1 || !strings.Contains(again.Stderr, "config already exists") {
				t.Errorf("second --create-config should refuse\n%s", again.dump())
			}
		})
	}
}

// --resume <id> picks a session by the id a previous run reported, and sends
// its transcript; --fork-session continues it under a new id.
func TestResumeByIDAndFork(t *testing.T) {
	m := NewFakeModel(t, Say("noted"), Say("resumed"), Say("forked"))
	e := NewEnv(t, m)

	first := e.Headless("The code word is QUINCE.")
	id, _ := first.ResultEvent()["session_id"].(string)
	if first.ExitCode != 0 || id == "" {
		t.Fatalf("first run gave no session id\n%s", first.dump())
	}

	resumed := e.Headless("What is the code word?", "--resume", id)
	if resumed.ExitCode != 0 || resumed.ResultEvent()["session_id"] != id {
		t.Fatalf("--resume did not continue session %s\n%s", id, resumed.dump())
	}
	forked := e.Headless("And again?", "--resume", id, "--fork-session")
	fid, _ := forked.ResultEvent()["session_id"].(string)
	if forked.ExitCode != 0 || fid == "" || fid == id {
		t.Fatalf("--fork-session session id = %q (original %s)\n%s", fid, id, forked.dump())
	}

	reqs := m.Requests()
	if len(reqs) != 3 {
		t.Fatalf("model called %d times, want 3", len(reqs))
	}
	if !strings.Contains(reqs[1].Raw(), "QUINCE") || len(reqs[1].Messages) != 3 {
		t.Errorf("--resume sent %d messages without the prior exchange", len(reqs[1].Messages))
	}
	// The fork resumed the full session so far: two exchanges plus the prompt.
	if !strings.Contains(reqs[2].Raw(), "QUINCE") || len(reqs[2].Messages) != 5 {
		t.Errorf("--fork-session sent %d messages, want 5", len(reqs[2].Messages))
	}
}

// Resuming an id with no transcript fails before the model is called.
func TestResumeUnknownIDFails(t *testing.T) {
	m := NewFakeModel(t)
	e := NewEnv(t, m)
	r := e.Headless("x", "--resume", "00000000-0000-0000-0000-000000000000")
	if r.ExitCode != 1 || !strings.Contains(r.Stderr, "resume 00000000") {
		t.Errorf("want exit 1 naming the session\n%s", r.dump())
	}
	if len(m.Requests()) != 0 {
		t.Error("model called for a session that does not exist")
	}
}

// With no credential anywhere, klaudia says how to get one and exits 1.
func TestMissingCredentialIsExplained(t *testing.T) {
	m := NewFakeModel(t)
	e := NewEnv(t, m)
	r := e.RunWithEnv([]string{"ANTHROPIC_API_KEY="}, "-p", "hi")
	if r.ExitCode != 1 || !strings.Contains(r.Stderr, "needs credentials") || !strings.Contains(r.Stderr, "--create-config") {
		t.Errorf("want a credentials explanation\n%s", r.dump())
	}
	if len(m.Requests()) != 0 {
		t.Error("model called without a credential")
	}
}

// provider = "openai" without the settings it needs stops with a message
// naming what is missing, from either the project or the global config.
func TestOpenAIProviderConfigErrors(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
		global             bool
	}{
		{"missing baseURL", "provider = \"openai\"\nmodel = \"m\"\napiKey = \"k\"\n", "requires baseURL", false},
		{"missing key", "provider = \"openai\"\nmodel = \"m\"\nbaseURL = \"http://127.0.0.1:9/v1\"\n", "needs apiKey or apiKeyEnv", false},
		{"key env unset", "provider = \"openai\"\nmodel = \"m\"\nbaseURL = \"http://127.0.0.1:9/v1\"\napiKeyEnv = \"E2E_UNSET_KEY\"\n", "needs apiKey or apiKeyEnv", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEnv(t, nil)
			dir := e.Path(".klaudia")
			if tc.global {
				dir = filepath.Join(e.Home, ".klaudia")
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tc.config), 0o644); err != nil {
				t.Fatal(err)
			}
			r := e.Run("-p", "hi")
			if r.ExitCode != 1 || !strings.Contains(r.Stderr, tc.want) {
				t.Errorf("want exit 1 with %q\n%s", tc.want, r.dump())
			}
		})
	}
}

// --output-format json prints exactly one result object.
func TestJSONOutputIsOneObject(t *testing.T) {
	m := NewFakeModel(t, Say("json reply"))
	e := NewEnv(t, m)
	r := e.Run("-p", "hi", "--output-format", "json")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	events := r.Events()
	if len(events) != 1 || events[0]["type"] != "result" || events[0]["result"] != "json reply" {
		t.Errorf("json output = %v", events)
	}
}

// Invalid combinations of output flags are usage errors (exit 2), caught
// before the model is called.
func TestOutputFlagCombinationsAreUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"stream-json without verbose", []string{"-p", "hi", "--output-format", "stream-json"}, "requires --verbose"},
		{"partial without stream-json", []string{"-p", "hi", "--include-partial-messages"}, "--include-partial-messages"},
		{"loop with stream-json input", []string{"--loop", "--input-format", "stream-json"}, "--loop cannot be combined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewFakeModel(t)
			e := NewEnv(t, m)
			r := e.Run(tc.args...)
			if r.ExitCode != 2 || !strings.Contains(r.Stderr, tc.want) {
				t.Errorf("want exit 2 mentioning %q\n%s", tc.want, r.dump())
			}
			if len(m.Requests()) != 0 {
				t.Error("model called on a usage error")
			}
		})
	}
}

// --include-partial-messages adds the raw stream events to stream-json output.
func TestIncludePartialMessages(t *testing.T) {
	m := NewFakeModel(t, Say("streamed"))
	e := NewEnv(t, m)
	r := e.Headless("hi", "--include-partial-messages")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	var deltas []string
	for _, ev := range r.Events() {
		if ev["type"] != "stream_event" {
			continue
		}
		inner, _ := ev["event"].(map[string]any)
		if d, ok := inner["delta"].(map[string]any); ok && d["type"] == "text_delta" {
			deltas = append(deltas, d["text"].(string))
		}
	}
	if strings.Join(deltas, "") != "streamed" {
		t.Errorf("text deltas = %q, want the streamed text\n%s", deltas, r.dump())
	}
}

// dontAsk with an allow list is the headless least-privilege setup: the
// allowed tool runs, anything else is denied without a prompt, and the run
// still completes.
func TestDontAskRunsOnlyAllowedTools(t *testing.T) {
	m := NewFakeModel(t,
		Use("Write", map[string]any{"file_path": "allowed.txt", "content": "ok"}),
		Use("Bash", map[string]any{"command": "touch denied.txt"}),
		Say("finished"),
	)
	e := NewEnv(t, m)
	r := e.Headless("x", "--permission-mode", "dontAsk", "--allowedTools", "Write")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	if _, err := os.Stat(e.Path("allowed.txt")); err != nil {
		t.Errorf("the allowed Write did not run: %v", err)
	}
	if _, err := os.Stat(e.Path("denied.txt")); err == nil {
		t.Error("a Bash call outside the allow list ran")
	}
	results := toolResults(r.Events())
	if len(results) != 2 || results[0]["is_error"] == true || results[1]["is_error"] != true {
		t.Errorf("tool results = %v, want Write ok and Bash denied", results)
	}
}

// --disallowedTools denies a tool even in bypassPermissions.
func TestDisallowedToolsDeniesInBypass(t *testing.T) {
	m := NewFakeModel(t,
		Use("Bash", map[string]any{"command": "touch nope.txt"}),
		Say("ok"),
	)
	e := NewEnv(t, m)
	r := e.Headless("x", "--dangerously-skip-permissions", "--disallowedTools", "Bash")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	if _, err := os.Stat(e.Path("nope.txt")); err == nil {
		t.Error("a disallowed Bash call ran")
	}
	if results := toolResults(r.Events()); len(results) != 1 || results[0]["is_error"] != true {
		t.Errorf("tool results = %v, want one denial", results)
	}
}

// The project's CLAUDE.md reaches the model as part of the system prompt.
func TestProjectInstructionsReachTheModel(t *testing.T) {
	m := NewFakeModel(t, Say("ok"))
	e := NewEnv(t, m)
	if err := os.WriteFile(e.Path("CLAUDE.md"), []byte("E2E-PROJECT-RULE-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.Headless("hi"); r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	if reqs := m.Requests(); len(reqs) != 1 || !strings.Contains(reqs[0].Raw(), "E2E-PROJECT-RULE-MARKER") {
		t.Error("CLAUDE.md did not reach the request")
	}
}
