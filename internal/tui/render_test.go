package tui

import (
	"strings"
	"testing"
)

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

func TestRenderCost(t *testing.T) {
	// Known model → estimate present; 1M in @ $3 + 1M out @ $15 = $18.
	m := &Model{sess: &Session{ResolvedModel: "claude-sonnet-4-5"}, statIn: 1_000_000, statOut: 1_000_000}
	got := m.renderCost()
	if !strings.Contains(got, "input_tokens=1000000") || !strings.Contains(got, "$18.0000") {
		t.Errorf("renderCost = %q", got)
	}
	// Unknown model → no pricing.
	u := &Model{sess: &Session{ResolvedModel: "mystery-model"}, statIn: 10}
	if got := u.renderCost(); !strings.Contains(got, "unknown") {
		t.Errorf("unknown renderCost = %q", got)
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
