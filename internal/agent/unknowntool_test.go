package agent

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestUnknownToolMsgSteersToAgent(t *testing.T) {
	// A registry whose Agent tool knows the "Plan" sub-agent type.
	agentTool, err := tools.NewAgent(nil, []tools.AgentTypeInfo{{Name: "Plan", Description: "plan things"}})
	if err != nil {
		t.Fatal(err)
	}
	l := New(nil, tools.NewRegistry(agentTool))

	// A sub-agent type called as a tool → steering message.
	msg := l.unknownToolMsg("Plan")
	if !strings.Contains(msg, "sub-agent") || !strings.Contains(msg, "Agent(subagent_type=\"Plan\"") {
		t.Errorf("expected a steering message, got %q", msg)
	}

	// A genuinely unknown name → the plain message + the list of real tools so
	// the model can pick one (here only the Agent tool is registered).
	if got := l.unknownToolMsg("Frobnicate"); !strings.Contains(got, "No such tool available: Frobnicate") || !strings.Contains(got, "Available tools: Agent") {
		t.Errorf("unknown-tool message = %q", got)
	}
}

func TestUnknownToolMsgNoAgent(t *testing.T) {
	// No Agent tool in the registry → no sub-agent steering, but still the
	// available-tools hint (Read is the only registered tool here).
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))
	got := l.unknownToolMsg("Plan")
	if !strings.Contains(got, "No such tool available: Plan") || !strings.Contains(got, "Available tools: Read") {
		t.Errorf("without an Agent tool = %q", got)
	}
}
