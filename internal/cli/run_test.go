package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/session"
)

// Text output is the answer and nothing else, so `klaudia -p` composes in a
// shell pipeline.
func TestRunTextOutputIsJustTheAnswer(t *testing.T) {
	m := newFakeModel(t, say("the answer"))
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "question")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if r.Stdout != "the answer\n" {
		t.Errorf("stdout = %q, want the answer and a newline", r.Stdout)
	}
}

// A bare positional argument is shorthand for -p: it runs headless rather than
// opening the TUI.
func TestRunPositionalPromptRunsHeadless(t *testing.T) {
	m := newFakeModel(t, say("positional ok"))
	e := newCLIEnv(t, m)
	r := e.run(nil, "what is this")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if strings.TrimSpace(r.Stdout) != "positional ok" {
		t.Errorf("stdout = %q", r.Stdout)
	}
	reqs := m.Requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0].Raw(), "what is this") {
		t.Errorf("the positional prompt did not reach the model: %+v", reqs)
	}
}

// JSON output is one result object carrying the session id, turn count and the
// token usage the API reported.
func TestRunJSONOutputIsOneResultObject(t *testing.T) {
	m := newFakeModel(t, say("json answer"))
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "q", "--output-format", "json")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	lines := r.lines()
	if len(lines) != 1 {
		t.Fatalf("json output has %d objects, want 1\n%s", len(lines), r.dump())
	}
	res := lines[0]
	if res["type"] != "result" || res["subtype"] != "success" || res["result"] != "json answer" || res["is_error"] != false {
		t.Errorf("result = %v", res)
	}
	if res["num_turns"] != float64(1) {
		t.Errorf("num_turns = %v, want 1", res["num_turns"])
	}
	usage, _ := res["usage"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) {
		t.Errorf("usage = %v, want the fake's 10 in / 5 out", usage)
	}
	id, _ := res["session_id"].(string)
	if id == "" {
		t.Fatal("result has no session_id")
	}
	if _, err := os.Stat(session.ExistingPath(e.Dir, id)); err != nil {
		t.Errorf("no transcript for session %s: %v", id, err)
	}
}

// stream-json output is the conversation as message envelopes, stamped with
// the session id, and ends with the result line; the tool ran on disk.
func TestRunStreamJSONEmitsEnvelopesThenResult(t *testing.T) {
	m := newFakeModel(t,
		use("Write", map[string]any{"file_path": "out.txt", "content": "written"}),
		say("wrote it"),
	)
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "write a file", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if got, err := os.ReadFile(filepath.Join(e.Dir, "out.txt")); err != nil || string(got) != "written" {
		t.Fatalf("out.txt = %q, %v", got, err)
	}
	lines := r.lines()
	var types []string
	for _, l := range lines {
		types = append(types, l["type"].(string))
	}
	// user prompt, assistant tool_use, user tool_result, assistant text, result.
	if got, want := strings.Join(types, ","), "user,assistant,user,assistant,result"; got != want {
		t.Fatalf("line types = %s, want %s\n%s", got, want, r.dump())
	}
	sid := lines[len(lines)-1]["session_id"]
	for _, l := range lines[:len(lines)-1] {
		if l["session_id"] != sid {
			t.Errorf("envelope session_id %v != result session_id %v", l["session_id"], sid)
		}
		if _, ok := l["message"].(map[string]any); !ok {
			t.Errorf("envelope without a message: %v", l)
		}
	}
}

// --include-partial-messages forwards the raw stream events as stream_event
// lines alongside the envelopes.
func TestRunPartialMessagesForwardsRawStreamEvents(t *testing.T) {
	m := newFakeModel(t, say("partial"))
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "q", "--output-format", "stream-json", "--verbose", "--include-partial-messages")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	var kinds []string
	for _, l := range r.lines() {
		if l["type"] != "stream_event" {
			continue
		}
		ev, _ := l["event"].(map[string]any)
		kinds = append(kinds, ev["type"].(string))
	}
	joined := strings.Join(kinds, ",")
	for _, want := range []string{"message_start", "content_block_delta", "message_stop"} {
		if !strings.Contains(joined, want) {
			t.Errorf("stream_event kinds %q lack %s", joined, want)
		}
	}
}

// Invocation mistakes exit 2 and say what was wrong, before any model call.
func TestRunUsageErrorsExit2WithoutCallingTheModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		args   []string
		want   string
	}{
		{"stream-json needs verbose", "", []string{"-p", "x", "--output-format", "stream-json"}, "requires --verbose"},
		{"partial needs stream-json", "", []string{"-p", "x", "--include-partial-messages"}, "--include-partial-messages only works"},
		{"loop with stream-json input", "", []string{"--loop", "--input-format", "stream-json"}, "--loop cannot be combined"},
		{"loop with json output", "", []string{"--loop", "--output-format", "json"}, "--loop only supports --output-format text"},
		{"invalid permission mode", "", []string{"-p", "x", "--permission-mode", "yolo"}, `invalid permission mode "yolo"`},
		{"invalid mode from config", "[permissions]\nmode = \"yolo\"\n", []string{"-p", "x"}, `invalid permission mode "yolo"`},
		{"autonomous without enforcing gate", "[trust]\nmode = \"off\"\n", []string{"-p", "x", "--permission-mode", "autonomous"}, "needs the host guardrail enforcing"},
		{"malformed allow rule", "", []string{"-p", "x", "--allowedTools", "Bash(git"}, "--allowedTools/permissions.allow"},
		{"malformed deny rule", "", []string{"-p", "x", "--disallowedTools", "Bash(rm"}, "--disallowedTools/permissions.deny"},
		{"unknown flag", "", []string{"--no-such-flag"}, "unknown flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newFakeModel(t)
			e := newCLIEnv(t, m)
			if tc.config != "" {
				e.write(".klaudia/config.toml", tc.config)
			}
			r := e.run(nil, tc.args...)
			if r.Code != ExitUsage {
				t.Errorf("exit %d, want %d\n%s", r.Code, ExitUsage, r.dump())
			}
			if r.Err == nil || !strings.Contains(r.Err.Error(), tc.want) {
				t.Errorf("error %v does not mention %q", r.Err, tc.want)
			}
			if n := len(m.Requests()); n != 0 {
				t.Errorf("model called %d times on a usage error", n)
			}
		})
	}
}

// An unknown --output-format is rejected before anything runs.
func TestRunRejectsUnknownOutputFormat(t *testing.T) {
	m := newFakeModel(t)
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "x", "--output-format", "yaml")
	if r.Err == nil || !strings.Contains(r.Err.Error(), `invalid output format "yaml"`) {
		t.Fatalf("err = %v, want invalid output format\n%s", r.Err, r.dump())
	}
	if len(m.Requests()) != 0 {
		t.Error("the model was called despite the bad format")
	}
}

// --create-config writes the starter file, says where, and runs nothing else.
func TestRunCreateConfigWritesStarterAndExits(t *testing.T) {
	m := newFakeModel(t)
	e := newCLIEnv(t, m)
	r := e.run(nil, "--create-config", "local")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	path := filepath.Join(e.Dir, ".klaudia", "config.toml")
	if !strings.Contains(r.Stdout, "Created "+path) {
		t.Errorf("stdout = %q, want it to name %s", r.Stdout, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config not written: %v", err)
	}
	if len(m.Requests()) != 0 {
		t.Error("--create-config called the model")
	}
	if r := e.run(nil, "--create-config", "local"); r.Err == nil || !strings.Contains(r.Err.Error(), "already exists") {
		t.Errorf("second --create-config: %v, want already exists", r.Err)
	}
}

// With no credential of any kind, the run stops and says how to get one.
func TestRunWithoutCredentialExplainsHowToGetOne(t *testing.T) {
	m := newFakeModel(t)
	e := newCLIEnv(t, m)
	t.Setenv("ANTHROPIC_API_KEY", "")
	r := e.run(nil, "-p", "x")
	if r.Code != ExitError || r.Err == nil || !strings.Contains(r.Err.Error(), "needs credentials") {
		t.Fatalf("want a credentials error, got\n%s", r.dump())
	}
	if len(m.Requests()) != 0 {
		t.Error("model called without a credential")
	}
}

// An OpenAI-provider config that cannot work is reported by what is missing.
func TestRunOpenAIProviderConfigErrors(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{"no baseURL", "provider = \"openai\"\nmodel = \"m\"\napiKey = \"k\"\n", "requires baseURL"},
		{"no key", "provider = \"openai\"\nmodel = \"m\"\nbaseURL = \"http://127.0.0.1:1/v1\"\n", "needs apiKey or apiKeyEnv"},
		{"unset key env", "provider = \"openai\"\nmodel = \"m\"\nbaseURL = \"http://127.0.0.1:1/v1\"\napiKeyEnv = \"UNIT_UNSET_KEY\"\n", "needs apiKey or apiKeyEnv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newCLIEnv(t, nil)
			t.Setenv("UNIT_UNSET_KEY", "")
			t.Setenv("UNIT_UNSET_HEADER", "")
			e.write(".klaudia/config.toml", tc.config)
			r := e.run(nil, "-p", "x")
			if r.Code != ExitError || r.Err == nil || !strings.Contains(r.Err.Error(), tc.want) {
				t.Fatalf("want error mentioning %q, got\n%s", tc.want, r.dump())
			}
		})
	}
}

// --max-turns with work still outstanding exits 3.
func TestRunMaxTurnsExits3(t *testing.T) {
	m := newFakeModel(t,
		use("Bash", map[string]any{"command": "true"}),
		use("Bash", map[string]any{"command": "true"}),
		use("Bash", map[string]any{"command": "true"}),
	)
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "loop", "--max-turns", "1", "--dangerously-skip-permissions", "--output-format", "json")
	if r.Code != ExitMaxTurns {
		t.Fatalf("exit %d, want %d\n%s", r.Code, ExitMaxTurns, r.dump())
	}
	if res := r.result(); res == nil || res["stop_reason"] != "max_turns" {
		t.Errorf("result = %v, want stop_reason max_turns", res)
	}
}

// An API failure is rendered into the result payload and exits 1 with no
// further message (it has already been said).
func TestRunAPIErrorRendersErrorResult(t *testing.T) {
	m := newFakeModel(t, fakeTurn{Status: 400})
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "x", "--output-format", "json")
	if r.Code != ExitError {
		t.Fatalf("exit %d, want 1\n%s", r.Code, r.dump())
	}
	res := r.result()
	if res == nil || res["is_error"] != true || res["subtype"] != "error_during_execution" ||
		!strings.HasPrefix(res["result"].(string), "Error: ") {
		t.Errorf("result = %v", res)
	}
	if r.Err.Error() != "" {
		t.Errorf("the error carries text %q; it would be printed a second time", r.Err.Error())
	}
}

// A refusal that returns no text is a failure a pipeline can see: the result
// is non-empty, flagged as an error, and the exit code is 1.
func TestRunEmptyRefusalIsAnError(t *testing.T) {
	m := newFakeModel(t, fakeTurn{StopReason: "refusal"})
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "x", "--output-format", "json")
	if r.Code != ExitError {
		t.Fatalf("exit %d, want 1\n%s", r.Code, r.dump())
	}
	res := r.result()
	if res == nil || res["is_error"] != true || res["subtype"] != "refusal" ||
		!strings.Contains(res["result"].(string), "declined") {
		t.Errorf("result = %v", res)
	}
}

// A host change the headless run cannot get agreement for exits 4, and the
// file outside the project is untouched.
func TestRunHostChangeBlockedExits4(t *testing.T) {
	m := newFakeModel(t,
		use("Bash", map[string]any{"command": "echo unit >> ~/.bashrc"}),
		say("could not"),
	)
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "x", "--output-format", "json")
	if r.Code != ExitHostChangeBlocked {
		t.Fatalf("exit %d, want %d\n%s", r.Code, ExitHostChangeBlocked, r.dump())
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".bashrc")); err == nil {
		t.Error("~/.bashrc was written despite the block")
	}
}

// A --disallowedTools rule stops the tool; the model is told so and the file
// is not written.
func TestRunDisallowedToolIsDenied(t *testing.T) {
	m := newFakeModel(t,
		use("Write", map[string]any{"file_path": "no.txt", "content": "x"}),
		say("ok"),
	)
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "x", "--disallowedTools", "Write", "--permission-mode", "bypassPermissions", "--output-format", "json")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if _, err := os.Stat(filepath.Join(e.Dir, "no.txt")); err == nil {
		t.Error("a disallowed Write ran")
	}
	reqs := m.Requests()
	if len(reqs) != 2 || !strings.Contains(reqs[1].Raw(), `"is_error":true`) {
		t.Errorf("the denial did not reach the model as an error result")
	}
}

// The project's CLAUDE.md is part of the system prompt the model receives.
func TestRunProjectInstructionsReachSystemPrompt(t *testing.T) {
	m := newFakeModel(t, say("ok"))
	e := newCLIEnv(t, m)
	e.write("CLAUDE.md", "UNIT-PROJECT-INSTRUCTION-MARKER")
	if r := e.run(nil, "-p", "x"); r.Err != nil {
		t.Fatal(r.dump())
	}
	if reqs := m.Requests(); len(reqs) != 1 || !strings.Contains(reqs[0].Raw(), "UNIT-PROJECT-INSTRUCTION-MARKER") {
		t.Error("CLAUDE.md did not reach the request")
	}
}

// Configuration problems that do not stop the run are reported on stderr: a
// .mcp.json that does not parse, and a container sandbox with no image.
func TestRunWarnsAboutBrokenOptionalConfig(t *testing.T) {
	m := newFakeModel(t, say("ok"))
	e := newCLIEnv(t, m)
	e.write(".mcp.json", "{not json")
	e.write(".klaudia/config.toml", "[sandbox]\nmode = \"container\"\n")
	r := e.run(nil, "-p", "x")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	for _, want := range []string{"warning: mcp config", "no image set"} {
		if !strings.Contains(r.Stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.Stderr)
		}
	}
}

// --model overrides the configured model.
func TestRunModelFlagOverridesConfig(t *testing.T) {
	m := newFakeModel(t, say("a"), say("b"))
	e := newCLIEnv(t, m)
	e.write(".klaudia/config.toml", "model = \"claude-config-model\"\n")
	e.run(nil, "-p", "x")
	e.run(nil, "-p", "x", "--model", "claude-flag-model")
	reqs := m.Requests()
	if len(reqs) != 2 || reqs[0].Model != "claude-config-model" || reqs[1].Model != "claude-flag-model" {
		t.Errorf("models = %+v, want config then flag", reqs)
	}
}
