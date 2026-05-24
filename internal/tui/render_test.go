package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

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

func TestRenderConfigDefaults(t *testing.T) {
	m := &Model{sess: &Session{}}
	got := m.renderConfig()
	for _, want := range []string{"provider=anthropic", "model=(default)", "sandbox=local", "permission-mode=default"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderConfig missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderConfigResolved(t *testing.T) {
	m := &Model{sess: &Session{Provider: "openai", ResolvedModel: "openai/gpt-5.5", SandboxMode: "os", PermissionMode: "acceptEdits"}}
	got := m.renderConfig()
	for _, want := range []string{"provider=openai", "model=openai/gpt-5.5", "sandbox=os", "permission-mode=acceptEdits"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderConfig missing %q in:\n%s", want, got)
		}
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
