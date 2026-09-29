package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/session"
)

// firstSession runs one headless turn and returns its session id.
func firstSession(t *testing.T, e *cliEnv, prompt string) string {
	t.Helper()
	r := e.run(nil, "-p", prompt, "--output-format", "json")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	id, _ := r.result()["session_id"].(string)
	if id == "" {
		t.Fatalf("no session id\n%s", r.dump())
	}
	return id
}

// --resume <id> sends the earlier transcript and keeps appending to the same
// session.
func TestRunResumeByIDSendsTranscriptAndKeepsSession(t *testing.T) {
	m := newFakeModel(t, say("noted"), say("MANGO"))
	e := newCLIEnv(t, m)
	id := firstSession(t, e, "remember MANGO")

	r := e.run(nil, "-p", "what was it?", "--resume", id, "--output-format", "json")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if got := r.result()["session_id"]; got != id {
		t.Errorf("resumed session id = %v, want %s", got, id)
	}
	reqs := m.Requests()
	if len(reqs[1].Messages) != 3 || !strings.Contains(reqs[1].Raw(), "remember MANGO") {
		t.Errorf("resume sent %d messages, want the prior pair plus the new prompt", len(reqs[1].Messages))
	}
}

// --fork-session resumes the history under a new id, leaving the original
// transcript as it was.
func TestRunForkSessionResumesUnderNewID(t *testing.T) {
	m := newFakeModel(t, say("noted"), say("forked"))
	e := newCLIEnv(t, m)
	id := firstSession(t, e, "remember KIWI")
	orig := session.ExistingPath(e.Dir, id)
	before, err := os.ReadFile(orig)
	if err != nil {
		t.Fatal(err)
	}

	r := e.run(nil, "-p", "next", "--resume", id, "--fork-session", "--output-format", "json")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	forked, _ := r.result()["session_id"].(string)
	if forked == "" || forked == id {
		t.Errorf("fork session id = %q, want a new id (original %s)", forked, id)
	}
	if !strings.Contains(m.Requests()[1].Raw(), "remember KIWI") {
		t.Error("the fork did not carry the original history")
	}
	after, _ := os.ReadFile(orig)
	if string(after) != string(before) {
		t.Error("forking modified the original transcript")
	}
}

// A persisted compaction summary replaces the transcript on resume; --full
// replays the transcript instead.
func TestRunResumePrefersSummaryUnlessFull(t *testing.T) {
	m := newFakeModel(t, say("noted"), say("from summary"), say("from full"))
	e := newCLIEnv(t, m)
	id := firstSession(t, e, "ORIGINAL-TRANSCRIPT-TEXT")
	if err := session.WriteSummary(e.Dir, id, "SUMMARY-TEXT", ""); err != nil {
		t.Fatal(err)
	}

	r := e.run(nil, "-p", "go on", "--resume", id)
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if !strings.Contains(r.Stderr, "resuming from compacted summary") {
		t.Errorf("stderr does not say the summary was used:\n%s", r.Stderr)
	}
	summaryReq := m.Requests()[1]
	if !strings.Contains(summaryReq.Raw(), "SUMMARY-TEXT") || strings.Contains(summaryReq.Raw(), "ORIGINAL-TRANSCRIPT-TEXT") {
		t.Errorf("summary resume sent the wrong history: %s", summaryReq.Raw())
	}

	if r := e.run(nil, "-p", "again", "--resume", id, "--full"); r.Err != nil {
		t.Fatal(r.dump())
	}
	if full := m.Requests()[2]; !strings.Contains(full.Raw(), "ORIGINAL-TRANSCRIPT-TEXT") {
		t.Error("--full did not replay the transcript")
	}
}

// Resuming a session that does not exist fails without calling the model.
func TestRunResumeUnknownSessionFails(t *testing.T) {
	m := newFakeModel(t)
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "x", "--resume", "no-such-session")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "resume no-such-session") {
		t.Fatalf("err = %v\n%s", r.Err, r.dump())
	}
	if len(m.Requests()) != 0 {
		t.Error("model called for a missing session")
	}
}

// --continue with nothing to continue says so.
func TestRunContinueWithoutPriorSessionFails(t *testing.T) {
	m := newFakeModel(t)
	e := newCLIEnv(t, m)
	r := e.run(nil, "-p", "x", "--continue")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "no previous session") {
		t.Fatalf("err = %v\n%s", r.Err, r.dump())
	}
}
