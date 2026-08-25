package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/agent"
)

// A gate block that Klaudia routes around is not a failure, and the line the
// user sees should not read like one. The model's copy carries instructions
// addressed to the model; none of it belongs on screen.
func TestHostBlockedLineKeepsOnlyWhatTheUserNeeds(t *testing.T) {
	msg := "Changes this machine: writes /dev/null. Take another route if one exists. " +
		"If it has to be done this way, call RequestHostChange to describe the whole operation and why."
	got := hostBlockedLine(msg)
	if strings.Contains(got, "RequestHostChange") {
		t.Errorf("the model's instructions leaked to the user: %q", got)
	}
	if !strings.Contains(got, "writes /dev/null") {
		t.Errorf("what was blocked was dropped: %q", got)
	}
	if !strings.Contains(got, "trying another way") {
		t.Errorf("the line should say what happens next: %q", got)
	}
}

// An unreadable command line has no "effects" sentence to trim, and must still
// produce something rather than an empty line.
func TestHostBlockedLineHandlesNoDetail(t *testing.T) {
	if got := hostBlockedLine(""); got == "" || !strings.Contains(got, "trying another way") {
		t.Errorf("empty refusal produced %q", got)
	}
}

// "Something else" is a redirect, not a refusal: the echoed line has to invite
// the instruction rather than announce that Klaudia is moving on without it.
func TestHostAnswerLineDistinguishesRedirectFromRefusal(t *testing.T) {
	refused := hostAnswerLine(&agent.HostChange{Summary: "install nginx"}, false, false)
	redirect := hostAnswerLine(&agent.HostChange{Summary: "install nginx"}, false, true)
	if refused == redirect {
		t.Fatal("a redirect reads the same as a flat refusal")
	}
	if !strings.Contains(refused, "carry on without it") {
		t.Errorf("refusal lost its meaning: %q", refused)
	}
	if !strings.Contains(redirect, "instead") {
		t.Errorf("redirect does not invite an instruction: %q", redirect)
	}
}
