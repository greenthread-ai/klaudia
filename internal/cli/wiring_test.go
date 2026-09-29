package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/browser"
	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/mcp"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/skill"
)

// /doctor reports the provider, sandbox, config, auth and skills it actually
// finds, and tells project skills from user ones.
func TestBuildDoctorInputReportsWhatItFinds(t *testing.T) {
	e := newCLIEnv(t, nil)
	skillMD := func(name string) string {
		return "---\nname: " + name + "\ndescription: d\n---\nbody\n"
	}
	e.write(".klaudia/skills/proj-skill/SKILL.md", skillMD("proj-skill"))
	userSkill := filepath.Join(e.Home, ".klaudia", "skills", "user-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userSkill, []byte(skillMD("user-skill")), 0o644); err != nil {
		t.Fatal(err)
	}

	in := buildDoctorInput(config.Config{}, "claude-sonnet-4-5", e.Dir, 2)
	if in.Provider != "anthropic" || in.SandboxMode != "local" || in.MCPServers != 2 {
		t.Errorf("defaults = %q/%q/%d", in.Provider, in.SandboxMode, in.MCPServers)
	}
	if !in.AuthOK || in.AuthKind != "api-key" {
		t.Errorf("auth = %v/%q, want api-key from ANTHROPIC_API_KEY", in.AuthOK, in.AuthKind)
	}
	if in.ConfigFound {
		t.Error("ConfigFound with no config file")
	}
	scopes := map[string]string{}
	for _, s := range in.Skills {
		scopes[s.Name] = s.Scope
	}
	if scopes["proj-skill"] != "project" || scopes["user-skill"] != "user" {
		t.Errorf("skill scopes = %v", scopes)
	}

	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "oauth-token")
	if in := buildDoctorInput(config.Config{}, "m", e.Dir, 0); in.AuthKind != "oauth" {
		t.Errorf("auth kind with a bearer token = %q, want oauth", in.AuthKind)
	}
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if in := buildDoctorInput(config.Config{}, "m", e.Dir, 0); in.AuthOK || in.AuthKind != "none" {
		t.Errorf("no credential: auth = %v/%q", in.AuthOK, in.AuthKind)
	}

	e.write(".klaudia/config.toml", "")
	openai := config.Config{Provider: config.ProviderOpenAI, APIKey: "k", Sandbox: config.Sandbox{Mode: "container"}}
	in = buildDoctorInput(openai, "m", e.Dir, 0)
	if in.Provider != "openai" || in.SandboxMode != "container" || !in.ConfigFound || !in.AuthOK || in.AuthKind != "api-key" {
		t.Errorf("openai input = %+v", in)
	}
	if in := buildDoctorInput(config.Config{Provider: config.ProviderOpenAI}, "m", e.Dir, 0); in.AuthOK {
		t.Error("openai without a key reported auth OK")
	}
}

// A global config counts as found, as well as a project one.
func TestConfigFileExistsSeesGlobalConfig(t *testing.T) {
	e := newCLIEnv(t, nil)
	if configFileExists(e.Dir) {
		t.Fatal("found a config in an empty HOME and project")
	}
	p := filepath.Join(e.Home, ".klaudia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !configFileExists(e.Dir) {
		t.Error("global config not found")
	}
}

// Browser options: config beats environment, environment beats defaults, and a
// remote URL switches to attach mode.
func TestBuildBrowserOptionsPrecedence(t *testing.T) {
	for _, k := range []string{"KLAUDIA_CHROME_PATH", "KLAUDIA_CHROME_USER_DATA_DIR", "KLAUDIA_CHROME_REMOTE_URL",
		"KLAUDIA_BROWSER_HEADLESS", "KLAUDIA_BROWSER_HEADED_FALLBACK"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", t.TempDir())

	def := buildBrowserOptions(config.Browser{})
	if def.Mode != browser.ModeLaunch || !def.Headless || !def.HeadedFallback || def.UserDataDir == "" {
		t.Errorf("defaults = %+v", def)
	}

	t.Setenv("KLAUDIA_BROWSER_HEADLESS", "off")
	t.Setenv("KLAUDIA_BROWSER_HEADED_FALLBACK", "no")
	t.Setenv("KLAUDIA_CHROME_PATH", "/env/chrome")
	t.Setenv("KLAUDIA_CHROME_USER_DATA_DIR", "/env/profile")
	env := buildBrowserOptions(config.Browser{})
	if env.Headless || env.HeadedFallback || env.ChromePath != "/env/chrome" || env.UserDataDir != "/env/profile" {
		t.Errorf("env options = %+v", env)
	}
	t.Setenv("KLAUDIA_BROWSER_HEADLESS", "yes")
	t.Setenv("KLAUDIA_BROWSER_HEADED_FALLBACK", "1")
	if o := buildBrowserOptions(config.Browser{}); !o.Headless || !o.HeadedFallback {
		t.Errorf("truthy env not honoured: %+v", o)
	}

	f := false
	cfg := buildBrowserOptions(config.Browser{
		Headless: &f, HeadedFallback: &f, ChromePath: "/cfg/chrome", UserDataDir: "/cfg/profile",
		RemoteURL: "http://127.0.0.1:9222", SearchEngine: "google",
	})
	if cfg.Headless || cfg.HeadedFallback || cfg.ChromePath != "/cfg/chrome" || cfg.UserDataDir != "/cfg/profile" ||
		cfg.SearchEngine != "google" || cfg.Mode != browser.ModeAttach {
		t.Errorf("config options = %+v", cfg)
	}
}

// Each sandbox mode that cannot be honoured falls back to local execution and
// says why; nothing silently runs unconfined.
func TestBuildExecutorFallsBackWithAWarning(t *testing.T) {
	var warned []string
	warn := func(m string) { warned = append(warned, m) }

	if _, ok := buildExecutor(config.Sandbox{}, warn).(*sandbox.Local); !ok || len(warned) != 0 {
		t.Errorf("default mode: want local and no warning, got %v", warned)
	}

	warned = nil
	buildExecutor(config.Sandbox{Mode: config.SandboxContainer, Image: "img", Runtime: "no-such-runtime-unit"}, warn)
	if len(warned) != 1 || !strings.Contains(warned[0], "no-such-runtime-unit is not installed") {
		t.Errorf("missing runtime warnings = %v", warned)
	}

	warned = nil
	ex := buildExecutor(config.Sandbox{Mode: config.SandboxOS}, warn)
	tool := map[string]string{"linux": "bwrap", "darwin": "sandbox-exec"}[goruntime.GOOS]
	if _, err := exec.LookPath(tool); tool == "" || err != nil {
		if _, ok := ex.(*sandbox.Local); !ok || len(warned) != 1 || !strings.Contains(warned[0], "falling back to local") {
			t.Errorf("os mode without %q: executor %T, warnings %v", tool, ex, warned)
		}
	} else if _, ok := ex.(*sandbox.Local); ok || len(warned) != 0 {
		t.Errorf("os mode with %s available fell back: %v", tool, warned)
	}
}

// A skill named like a built-in slash command is still offered, but the user
// is told it is reachable only through the Skill tool.
func TestTUISkillsWarnsOnShadowedBuiltin(t *testing.T) {
	var warned []string
	skills := []skill.Skill{{Name: "deploy", Description: "d"}, {Name: "help", Description: "h"}}
	cmds := tuiSkills(skills, func(m string) { warned = append(warned, m) })
	if len(cmds) != 2 || cmds[0].Name != "deploy" || cmds[1].Name != "help" {
		t.Errorf("commands = %+v", cmds)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], `skill "help" shadows built-in /help`) {
		t.Errorf("warnings = %v", warned)
	}
	infos := skillToolInfos(skills)
	if len(infos) != 2 || infos[1].Name != "help" || infos[1].Render == nil {
		t.Errorf("skill tool infos = %+v", infos)
	}
}

func TestTUIAgentsListsBuiltins(t *testing.T) {
	agents := tuiAgents()
	if len(agents) == 0 {
		t.Fatal("no built-in agents offered to /agents")
	}
	for _, a := range agents {
		if a.Name == "" || a.Description == "" {
			t.Errorf("agent without a name or description: %+v", a)
		}
	}
}

// /add-dir directories are listed in the system prompt; with none it is
// unchanged.
func TestWithExtraDirs(t *testing.T) {
	if got := withExtraDirs("SYS", nil); got != "SYS" {
		t.Errorf("no dirs: %q", got)
	}
	got := withExtraDirs("SYS", []string{"/a", "/b"})
	if !strings.HasPrefix(got, "SYS\n\n") || !strings.Contains(got, "- /a\n- /b") {
		t.Errorf("with dirs: %q", got)
	}
}

func TestFirstNonEmptyTrims(t *testing.T) {
	if got := firstNonEmpty("", "  ", " x ", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty(" ", ""); got != "" {
		t.Errorf("all blank = %q", got)
	}
}

// stubProvider satisfies api.Provider but cannot list models.
type stubProvider struct{}

func (stubProvider) StreamTurn(context.Context, anthropic.BetaMessageNewParams, api.StreamSink) (anthropic.BetaMessage, error) {
	return anthropic.BetaMessage{}, nil
}

// /model offers a list only when the provider can enumerate models.
func TestModelListerOnlyForListingProviders(t *testing.T) {
	if modelLister(stubProvider{}) != nil {
		t.Error("a provider without ListModels got a lister")
	}
	if modelLister(api.NewOpenAIProvider("http://127.0.0.1:1/v1", "k", nil)) == nil {
		t.Error("the OpenAI provider's lister was dropped")
	}
}

// A configured OpenAI provider is built with the configured model.
func TestBuildProviderOpenAI(t *testing.T) {
	p, model, err := buildProvider(config.Config{Provider: config.ProviderOpenAI, BaseURL: "http://127.0.0.1:1/v1",
		Model: "gpt-unit", APIKey: "k"})
	if err != nil || p == nil || model != "gpt-unit" {
		t.Fatalf("buildProvider = %v, %q, %v", p, model, err)
	}
}

// With no MCP servers, /mcp shows none and reconnecting an unknown one fails
// by name.
func TestMCPControllerWithNoServers(t *testing.T) {
	ctx := context.Background()
	mgr, errs := mcp.Connect(ctx, mcp.Config{})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	defer mgr.Close()
	c := mcpController{mgr: mgr, ctx: ctx}
	if s := c.Servers(); len(s) != 0 {
		t.Errorf("servers = %+v", s)
	}
	if err := c.Reconnect("ghost"); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("Reconnect(ghost) = %v", err)
	}
	if err := c.Disconnect("ghost"); err == nil {
		t.Error("Disconnect(ghost) succeeded")
	}
}

// Renderer: intermediate events only appear in stream-json, and the text
// renderer prints just the result.
func TestRendererFormats(t *testing.T) {
	var b bytes.Buffer
	text := NewRenderer(FormatText, &b)
	if err := text.Event(map[string]any{"type": "x"}); err != nil || b.Len() != 0 {
		t.Errorf("text Event wrote %q", b.String())
	}
	if err := text.Result(ResultMessage{Result: "r"}); err != nil || b.String() != "r\n" {
		t.Errorf("text Result wrote %q", b.String())
	}

	b.Reset()
	sj := NewRenderer(FormatStreamJSON, &b)
	if err := sj.Event(map[string]any{"type": "x"}); err != nil || b.String() != "{\"type\":\"x\"}\n" {
		t.Errorf("stream-json Event wrote %q", b.String())
	}
	if err := sj.Event(func() {}); err == nil {
		t.Error("an unencodable event was written without error")
	}
	if err := NewRenderer("xml", &b).Result(ResultMessage{}); err == nil {
		t.Error("an unknown format rendered a result")
	}
}

// multiRecorder skips nil recorders and stops at the first failure.
func TestMultiRecorderSkipsNilAndStopsOnError(t *testing.T) {
	var b bytes.Buffer
	boom := errors.New("boom")
	failing := agentRecorderFunc(func(string, json.RawMessage) error { return boom })
	m := multiRecorder{nil, newEnvelopeRecorder(&b, "sid"), failing, newEnvelopeRecorder(&b, "sid")}
	if err := m.Record("assistant", json.RawMessage(`{"role":"assistant"}`)); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if n := strings.Count(b.String(), "\n"); n != 1 {
		t.Errorf("%d envelopes written, want 1 (the recorder after the failure must not run)", n)
	}
	var env map[string]any
	if err := json.Unmarshal(b.Bytes(), &env); err != nil || env["type"] != "assistant" || env["session_id"] != "sid" {
		t.Errorf("envelope = %s", b.String())
	}
}

type agentRecorderFunc func(string, json.RawMessage) error

func (f agentRecorderFunc) Record(role string, m json.RawMessage) error { return f(role, m) }

var _ agent.Recorder = agentRecorderFunc(nil)

// A stream event with no raw JSON (one not decoded from the wire) is not
// forwarded as an empty stream_event line.
func TestPartialEmitterSkipsEventsWithoutRawJSON(t *testing.T) {
	var b bytes.Buffer
	newPartialEmitter(&b, "sid", nil).emit(anthropic.BetaRawMessageStreamEventUnion{})
	if b.Len() != 0 {
		t.Errorf("wrote %q for an event with no raw JSON", b.String())
	}
}

// hostChangeWasBlocked is false for a gate that stopped nothing.
func TestHostChangeWasBlockedFalseForQuietGate(t *testing.T) {
	if hostChangeWasBlocked(&agent.HostGate{}) {
		t.Error("an unused gate reported a block")
	}
}
