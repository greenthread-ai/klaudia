package agent

import (
	"strings"
	"testing"

	"github.com/greenthread/klaudia/internal/tools"
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

	// A genuinely unknown name → the plain message.
	if got := l.unknownToolMsg("Frobnicate"); got != "No such tool available: Frobnicate" {
		t.Errorf("plain message = %q", got)
	}
}

func TestUnknownToolMsgNoAgent(t *testing.T) {
	// No Agent tool in the registry → always the plain message.
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))
	if got := l.unknownToolMsg("Plan"); got != "No such tool available: Plan" {
		t.Errorf("without an Agent tool = %q", got)
	}
}
