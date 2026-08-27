package tui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestExportMarkdown(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hello there")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("hi back")}},
	}
	got := exportMarkdown(history)
	for _, want := range []string{"# Klaudia conversation", "## User", "hello there", "## Assistant", "hi back"} {
		if !strings.Contains(got, want) {
			t.Errorf("exportMarkdown missing %q in:\n%s", want, got)
		}
	}
}

func TestPermissionSummaryAndDetail(t *testing.T) {
	m := newTestModel()
	raw, err := json.Marshal(map[string]any{
		"file_path":  "/tmp/example.go",
		"old_string": "old\nvalue",
		"new_string": "new value",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := agent.ApprovalRequest{ToolName: "Edit", Input: raw, Specifier: "/tmp/example.go"}
	if got := m.permissionSummary(req); got != "edit /tmp/example.go" {
		t.Errorf("permissionSummary = %q", got)
	}
	detail := permissionDetail(req)
	for _, want := range []string{"file: /tmp/example.go", `replace "old value" → "new value"`} {
		if !strings.Contains(detail, want) {
			t.Errorf("permissionDetail missing %q in %q", want, detail)
		}
	}
}

func TestRenderToolResultIncludesStatusAndToolName(t *testing.T) {
	m := newTestModel()
	m.renderEvent(agent.Event{Type: "tool_result", ToolName: "Edit", Content: "Edited /tmp/example.go (1 replacement(s))"})
	got := stripANSI(m.transcript.String())
	if !strings.Contains(got, "✓ Edit: Edited /tmp/example.go") {
		t.Errorf("tool result missing explicit success: %q", got)
	}
}

func TestRenderConfigDefaults(t *testing.T) {
	m := &Model{sess: &Session{}}
	got := m.renderConfig()
	for _, want := range []string{"provider=anthropic", "model=(default)", "sandbox=local", "Ask before risky operations"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderConfig missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderConfigResolved(t *testing.T) {
	m := &Model{sess: &Session{Provider: "openai", ResolvedModel: "openai/gpt-5.5", SandboxMode: "os", PermissionMode: "acceptEdits"}}
	got := m.renderConfig()
	for _, want := range []string{"provider=openai", "model=openai/gpt-5.5", "sandbox=os", "Auto-accept file edits"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderConfig missing %q in:\n%s", want, got)
		}
	}
}

func TestModeChoicesMarksCurrent(t *testing.T) {
	m := &Model{sess: &Session{PermissionMode: "plan", Trust: &fakeTrust{policy: agent.HostEnforce}}}
	items := m.modeChoices()
	var marked int
	for _, it := range items {
		if strings.Contains(it.label, "(current)") {
			marked++
			if !strings.Contains(it.label, "Plan mode") {
				t.Errorf("wrong item marked current: %q", it.label)
			}
		}
	}
	if marked != 1 {
		t.Errorf("expected exactly one (current) item, got %d", marked)
	}
	// Applying a choice mutates the session mode.
	if msg := items[0].apply(); !strings.Contains(msg, "Permission mode:") {
		t.Errorf("apply returned %q", msg)
	}
	if m.sess.PermissionMode != string(permission.SelectableModes()[0]) {
		t.Errorf("apply did not set the mode: %q", m.sess.PermissionMode)
	}
}

func TestThemeLookupAliases(t *testing.T) {
	cases := map[string]string{
		"Dracula":          "dracula",
		"tokyo night":      "tokyo-night",
		"catppuccin-mocha": "catppuccin",
		"mocha":            "catppuccin",
		"gruv":             "gruvbox",
	}
	for input, want := range cases {
		theme, ok := lookupTheme(input)
		if !ok {
			t.Fatalf("lookupTheme(%q) was not found", input)
		}
		if theme.id != want {
			t.Errorf("lookupTheme(%q) = %q, want %q", input, theme.id, want)
		}
	}
	if _, ok := lookupTheme("unknown"); ok {
		t.Error("lookupTheme(unknown) should not be found")
	}
}

func TestThemeChoicesApplyTheme(t *testing.T) {
	m := &Model{sess: &Session{Theme: "nord"}}
	items := m.themeChoices()
	if len(items) != len(renderThemes) {
		t.Fatalf("themeChoices len = %d, want %d", len(items), len(renderThemes))
	}
	var marked int
	for _, it := range items {
		if strings.Contains(it.label, "(current)") {
			marked++
			if !strings.Contains(it.label, "Nord") {
				t.Errorf("wrong item marked current: %q", it.label)
			}
		}
	}
	if marked != 1 {
		t.Errorf("expected exactly one current theme, got %d", marked)
	}
	if msg := items[0].apply(); !strings.Contains(msg, "Theme:") {
		t.Errorf("apply returned %q", msg)
	}
	if m.sess.Theme != renderThemes[0].id {
		t.Errorf("apply did not set theme: %q", m.sess.Theme)
	}
}

// Rendering inline makes scrollback immutable, so a theme change can only
// affect subsequent output. What must still hold is that the renderer is
// rebuilt and that already-printed text is left exactly as it was.
func TestSetThemeAffectsNewOutputOnly(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.width = 80
	m.buildGlamour(80)
	m.appendMarkdown("# Title")
	before := m.transcript.String()
	oldRenderer := m.glam

	m.setTheme("dracula")

	if m.sess.Theme != "dracula" {
		t.Fatalf("theme = %q, want dracula", m.sess.Theme)
	}
	if m.glam == oldRenderer {
		t.Error("theme change should rebuild the Markdown renderer")
	}
	if m.transcript.String() != before {
		t.Error("already-printed scrollback must not be rewritten")
	}

	m.appendMarkdown("# Later")
	if !strings.Contains(stripANSI(m.transcript.String()), "Later") {
		t.Error("output after the theme change should still render")
	}
}

func TestRenderAgents(t *testing.T) {
	m := &Model{sess: &Session{Agents: []AgentInfo{{Name: "Explore", Description: "search agent"}}}}
	if got := m.renderAgents(); !strings.Contains(got, "Explore") || !strings.Contains(got, "search agent") {
		t.Errorf("renderAgents = %q", got)
	}
	empty := &Model{sess: &Session{}}
	if got := empty.renderAgents(); !strings.Contains(got, "No sub-agent types") {
		t.Errorf("empty renderAgents = %q", got)
	}
}

func TestThroughput(t *testing.T) {
	if got := throughput(1240, 10*time.Second); !strings.Contains(got, "1.2k tokens") || !strings.Contains(got, "124 tok/s") {
		t.Errorf("throughput = %q", got)
	}
	if got := throughput(0, 10*time.Second); got != "" {
		t.Errorf("zero tokens should yield empty, got %q", got)
	}
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{980, "980"},
		{1500, "1.5k"},
		{999_000, "999.0k"},
		{1_000_000, "1.0M"},
		{200_000, "200.0k"},
	} {
		if got := humanTokens(tc.in); got != tc.want {
			t.Errorf("humanTokens(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := humanTokens(980); got != "980" {
		t.Errorf("humanTokens(980) = %q", got)
	}
}

func TestMarkdownAndFlush(t *testing.T) {
	m := newTestModel()
	m.buildGlamour(80)
	// markdown renders (output differs from raw source and is non-empty).
	out := m.markdown("# Title\n\nsome **bold** text")
	if strings.TrimSpace(out) == "" || out == "# Title\n\nsome **bold** text" {
		t.Errorf("markdown did not render: %q", out)
	}
	// flushAssistant moves the buffered stream into the transcript and clears it.
	m.streamBuf.WriteString("hello world")
	m.flushAssistant()
	if m.streamBuf.Len() != 0 {
		t.Error("streamBuf should be empty after flush")
	}
	// glamour interleaves ANSI colour codes; strip them before checking content.
	if !strings.Contains(stripANSI(m.transcript.String()), "hello world") {
		t.Errorf("transcript missing flushed text: %q", m.transcript.String())
	}
	// A second flush with nothing buffered is a no-op.
	before := m.transcript.Len()
	m.flushAssistant()
	if m.transcript.Len() != before {
		t.Error("empty flush should not change the transcript")
	}
}

func TestIntro(t *testing.T) {
	got := intro("openai/gpt-5.5", "go-port", "your preferred coding agent")
	for _, want := range []string{"Klaudia", "your preferred coding agent", "openai/gpt-5.5", "go-port", "Esc to interrupt"} {
		if !strings.Contains(got, want) {
			t.Errorf("intro missing %q", want)
		}
	}
	// The session id and the manual --resume hint are intentionally absent
	// (interactive auto-resumes; /status surfaces the id on demand).
	for _, unwanted := range []string{"session:", "--resume"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("intro should not include %q", unwanted)
		}
	}
	// No model/branch → still renders the logo + tagline + tip.
	if bare := intro("", "", "the better coding agent"); !strings.Contains(bare, "Klaudia") {
		t.Errorf("bare intro = %q", bare)
	}
}

func TestRenderContext(t *testing.T) {
	m := &Model{sess: &Session{SessionID: "session-123", CWD: "/work/proj", GitBranch: "go-port"}}
	got := m.renderContext()
	for _, want := range []string{"cwd=/work/proj", "git-branch=go-port", "session-id=session-123", "messages=0"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderContext missing %q in:\n%s", want, got)
		}
	}
}

// The status bar must name the mode it is actually in. Autonomous displaying
// "ask" told the user Klaudia would stop and check while it was working
// straight through — the one field on that line that must never be wrong.
func TestShortModeNamesEveryMode(t *testing.T) {
	want := map[permission.Mode]string{
		permission.ModeAutonomous:        "autonomous",
		permission.ModePlan:              "plan",
		permission.ModeBypassPermissions: "bypass",
		permission.ModeAcceptEdits:       "auto-edit",
		permission.ModeDontAsk:           "deny",
		permission.ModeDefault:           "ask",
	}
	seen := map[string]permission.Mode{}
	for mode, label := range want {
		got := shortMode(mode)
		if got != label {
			t.Errorf("shortMode(%s) = %q, want %q", mode, got, label)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s and %s both display as %q — the status bar cannot tell them apart",
				prev, mode, got)
		}
		seen[got] = mode
	}
}

// A refusal turn must not render as a silent "✓ done". Reported from a real
// session: three messages in a row got a completed turn with no answer above
// it, indistinguishable from a bug.
func TestRefusalTurnShowsAnExplanation(t *testing.T) {
	m := newTestModel()
	m.resize(120, 40)
	m.setState(stateRunning)

	m.update(doneMsg{res: agent.Result{StopReason: "refusal", Text: ""}})

	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "declined") {
		t.Errorf("a refusal rendered no explanation:\n%s", out)
	}
	if !strings.Contains(out, "/clear") {
		t.Errorf("the refusal note does not offer the way out:\n%s", out)
	}
	// The bare completion line alone was the bug.
	if strings.Contains(out, "done in") && !strings.Contains(out, "declined") {
		t.Error("only the completion line rendered")
	}
}

// A normal answer is untouched — the note must not fire on ordinary turns.
func TestNormalTurnShowsNoNote(t *testing.T) {
	m := newTestModel()
	m.resize(120, 40)
	m.setState(stateRunning)
	m.update(doneMsg{res: agent.Result{StopReason: "end_turn", Text: "here is your answer"}})
	if out := stripANSI(m.transcript.String()); strings.Contains(out, "declined") || strings.Contains(out, "⚠") {
		t.Errorf("a normal turn showed a warning:\n%s", out)
	}
}

// A truncated turn that DID produce text keeps the text and adds an advisory,
// not an error.
func TestTruncatedTurnKeepsItsPartialAnswer(t *testing.T) {
	m := newTestModel()
	m.resize(120, 40)
	m.setState(stateRunning)
	m.update(doneMsg{res: agent.Result{StopReason: "max_tokens", Text: "a partial answer that got"}})
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "cut off") {
		t.Errorf("a truncated turn was not flagged:\n%s", out)
	}
}
