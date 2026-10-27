package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
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
	refused := hostAnswerLine(&hostgate.HostChange{Summary: "install nginx"}, false, false)
	redirect := hostAnswerLine(&hostgate.HostChange{Summary: "install nginx"}, false, true)
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

// The hooks question shares the card but not its sentences. Each of these was
// wrong before the card knew the difference: "caught on the way past" blames the
// model for a config file it never saw, and "for this session only" describes a
// decision that is in fact remembered.
func TestHooksCardDoesNotReadLikeAHostChange(t *testing.T) {
	card := strings.Join(hostCardLines(&hostgate.HostChange{
		Hooks:    true,
		Summary:  "run 2 hooks declared by /repo/.klaudia/config.toml",
		Paths:    []string{"/repo/.klaudia/config.toml"},
		Commands: []string{"PostToolUse(Edit|Write): gofmt -w .", "PreToolUse(Bash): ./scripts/guard.sh"},
	}), "\n")

	for _, wrong := range []string{"caught on the way past", "for this session only", "This changes your machine"} {
		if strings.Contains(card, wrong) {
			t.Errorf("card says %q, which is not true of a hook set:\n%s", wrong, card)
		}
	}
	if !strings.Contains(card, "remembered until these hooks change") {
		t.Errorf("card does not say how long the answer lasts:\n%s", card)
	}
	// The commands are the whole point of the prompt: "hooks: yes" is not a
	// question a person can answer.
	for _, cmd := range []string{"gofmt -w .", "./scripts/guard.sh"} {
		if !strings.Contains(card, cmd) {
			t.Errorf("card does not show %q:\n%s", cmd, card)
		}
	}
	if got := hostPrompt(&hostgate.HostChange{Hooks: true}); strings.Contains(got, "something else") {
		t.Errorf("prompt = %q; there is no task to redirect, only a yes or a no", got)
	}
}

// Each command on its own line. A list of command lines joined with commas is
// what a user skims instead of reading, and commas occur inside commands.
func TestHooksCardListsOneCommandPerLine(t *testing.T) {
	lines := hostCardLines(&hostgate.HostChange{
		Hooks:    true,
		Commands: []string{"PostToolUse: a, b", "PreToolUse: c"},
	})
	count := 0
	for _, l := range lines {
		if strings.Contains(l, "PostToolUse: a, b") || strings.Contains(l, "PreToolUse: c") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("found %d command lines, want one per command:\n%s", count, strings.Join(lines, "\n"))
	}
}
