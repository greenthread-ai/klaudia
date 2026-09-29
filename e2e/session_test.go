package e2e

import (
	"strings"
	"testing"
)

// --continue resumes the previous transcript, and a run without it starts
// fresh. smoke.sh checks this by asking a live model to recall a word, which
// tests recall as much as resume; here the check is what klaudia actually sent.
func TestContinueSendsPriorTranscript(t *testing.T) {
	m := NewFakeModel(t, Say("ok"), Say("PINEAPPLE"), Say("SECOND"))
	e := NewEnv(t, m)

	if r := e.Headless("Remember this secret word: PINEAPPLE."); r.ExitCode != 0 {
		t.Fatalf("first run: exit %d\n%s", r.ExitCode, r.dump())
	}
	if r := e.Headless("What was the secret word?", "--continue"); r.ExitCode != 0 {
		t.Fatalf("--continue run: exit %d\n%s", r.ExitCode, r.dump())
	}
	if r := e.Headless("Reply with just: SECOND"); r.ExitCode != 0 {
		t.Fatalf("fresh run: exit %d\n%s", r.ExitCode, r.dump())
	}

	reqs := m.Requests()
	if len(reqs) != 3 {
		t.Fatalf("model called %d times, want 3", len(reqs))
	}

	resumed := reqs[1]
	if len(resumed.Messages) != 3 {
		t.Errorf("--continue sent %d messages, want 3 (prior user, prior assistant, new user)", len(resumed.Messages))
	}
	if !strings.Contains(resumed.Raw(), "PINEAPPLE") {
		t.Errorf("--continue did not send the prior transcript")
	}

	fresh := reqs[2]
	if len(fresh.Messages) != 1 {
		t.Errorf("a run without --continue sent %d messages, want 1", len(fresh.Messages))
	}
	if strings.Contains(fresh.Raw(), "PINEAPPLE") {
		t.Errorf("a run without --continue carried the old transcript")
	}
}
