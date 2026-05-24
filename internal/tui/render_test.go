package tui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/permission"
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
	m := &Model{sess: &Session{PermissionMode: "plan"}}
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

func TestSetThemeRerendersMarkdownTranscript(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.width = 80
	m.buildGlamour(80)
	m.appendMarkdown("# Title")
	before := m.transcript.String()

	m.setTheme("dracula")
	after := m.transcript.String()

	if m.sess.Theme != "dracula" {
		t.Fatalf("theme = %q, want dracula", m.sess.Theme)
	}
	if before == after {
		t.Fatal("expected transcript to be rerendered after theme change")
	}
	if !strings.Contains(stripANSI(after), "Title") {
		t.Fatalf("rerendered transcript missing markdown text: %q", after)
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
	got := intro("openai/gpt-5.5", "go-port")
	for _, want := range []string{"Klaudia", "openai/gpt-5.5", "go-port", "Esc to interrupt"} {
		if !strings.Contains(got, want) {
			t.Errorf("intro missing %q", want)
		}
	}
	// No model/branch → still renders the logo + tip.
	if bare := intro("", ""); !strings.Contains(bare, "Klaudia") {
		t.Errorf("bare intro = %q", bare)
	}
}

func TestRenderContext(t *testing.T) {
	m := &Model{sess: &Session{CWD: "/work/proj", GitBranch: "go-port"}}
	got := m.renderContext()
	for _, want := range []string{"cwd=/work/proj", "git-branch=go-port", "messages=0"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderContext missing %q in:\n%s", want, got)
		}
	}
}
