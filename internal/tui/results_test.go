package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The regression: only the newest tool result used to be recoverable, so a big
// build log became unreachable the moment any later tool ran.
func TestEarlierToolOutputStaysRecoverable(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.renderEvent(mkResult("Bash", "IMPORTANT FIRST LOG\n"+strings.Repeat("x\n", 300), false))
	m.renderEvent(mkResult("Read", "something trivial", false))

	first, ok := m.results.find("1")
	if !ok {
		t.Fatal("the first result should still be in the ring")
	}
	if !strings.Contains(first.content, "IMPORTANT FIRST LOG") {
		t.Error("stored content should be the full, untruncated result")
	}
	if latest, _ := m.results.latest(); latest.tool != "Read" {
		t.Errorf("latest = %q, want Read", latest.tool)
	}
}

func TestPreviewNamesTheSequenceNumber(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.renderEvent(mkResult("Bash", "small", false))
	m.renderEvent(mkResult("Bash", strings.Repeat("long line of output\n", 200), false))

	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "/last 2") {
		t.Errorf("the preview should name the number to ask for:\n%s", out)
	}
}

func TestResultRingEvictsOldestButNeverNewest(t *testing.T) {
	var r resultRing
	for i := 0; i < resultRingMaxItems+50; i++ {
		r.add(toolResult{tool: "Bash", content: fmt.Sprintf("result %d", i)})
	}
	if len(r.items) > resultRingMaxItems {
		t.Errorf("ring holds %d items, over the %d cap", len(r.items), resultRingMaxItems)
	}
	latest, _ := r.latest()
	if !strings.Contains(latest.content, fmt.Sprintf("result %d", resultRingMaxItems+49)) {
		t.Error("the newest result must survive eviction")
	}
	if _, ok := r.find("1"); ok {
		t.Error("the oldest results should have been evicted")
	}
}

func TestResultRingKeepsOversizedNewest(t *testing.T) {
	var r resultRing
	r.add(toolResult{tool: "Bash", content: strings.Repeat("y", resultRingMaxBytes+1)})
	if len(r.items) != 1 {
		t.Fatalf("a single oversized result should still be kept, got %d items", len(r.items))
	}
}

func TestResultLookupBySeqAndID(t *testing.T) {
	var r resultRing
	r.add(toolResult{id: "toolu_abc123", tool: "Bash", content: "one"})
	r.add(toolResult{id: "toolu_def456", tool: "Read", content: "two"})

	if got, ok := r.find("2"); !ok || got.content != "two" {
		t.Errorf("lookup by seq failed: %+v ok=%v", got, ok)
	}
	if got, ok := r.find("toolu_abc"); !ok || got.content != "one" {
		t.Errorf("lookup by tool_use_id prefix failed: %+v ok=%v", got, ok)
	}
	if _, ok := r.find("nope"); ok {
		t.Error("an unknown reference should not resolve")
	}
}

func TestLastListRendersIndex(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.renderEvent(mkResult("Bash", "ok\nsecond line", false))
	m.renderEvent(mkResult("Grep", "no matches", true))

	m.showResult([]string{"list"})
	out := visibleText(m.transcript.String())
	for _, want := range []string{"Bash", "Grep", "✗", "/last"} {
		if !strings.Contains(out, want) {
			t.Errorf("index missing %q:\n%s", want, out)
		}
	}
}

func TestLastUnknownReferenceReportsClearly(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.showResult([]string{"99"})
	if !strings.Contains(visibleText(m.transcript.String()), "/last list") {
		t.Error("an unknown reference should point at the index")
	}
}

func TestPagerCommandHonoursEnv(t *testing.T) {
	env := func(k string) string {
		if k == "PAGER" {
			return "cat"
		}
		return ""
	}
	cmd, ok := pagerCommand(env, "/tmp/x.txt")
	if !ok {
		t.Fatal("expected cat to resolve as a pager")
	}
	if !strings.HasSuffix(cmd.Path, "cat") {
		t.Errorf("pager = %q, want cat", cmd.Path)
	}
	if cmd.Args[len(cmd.Args)-1] != "/tmp/x.txt" {
		t.Errorf("the file should be the final argument, got %v", cmd.Args)
	}
}

func TestPagerAddsColourFlagForLess(t *testing.T) {
	env := func(k string) string {
		if k == "PAGER" {
			return "less"
		}
		return "" // $LESS unset
	}
	cmd, ok := pagerCommand(env, "/tmp/x.txt")
	if !ok {
		t.Skip("less not installed")
	}
	if !contains(cmd.Args, "-R") {
		t.Errorf("less should get -R so ANSI survives, got %v", cmd.Args)
	}
}

func TestPagerRespectsExistingLESS(t *testing.T) {
	env := func(k string) string {
		switch k {
		case "PAGER":
			return "less"
		case "LESS":
			return "-R -S"
		}
		return ""
	}
	cmd, ok := pagerCommand(env, "/tmp/x.txt")
	if !ok {
		t.Skip("less not installed")
	}
	if contains(cmd.Args, "-R") {
		t.Errorf("-R should not be added when $LESS already has it, got %v", cmd.Args)
	}
}

func TestShortOutputPrintsInsteadOfPaging(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	if cmd := m.showLong("Bash", "just a couple\nof lines"); cmd != nil {
		t.Error("short output should print inline, not launch a pager")
	}
	if !strings.Contains(m.transcript.String(), "just a couple") {
		t.Error("short output should reach the transcript")
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// A clamped Bash result names a spill file; /last should show that, not the
// truncated text the model was given.
func TestLastPrefersTheUntruncatedSpillFile(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "bash-x.log")
	full := "THE COMPLETE LOG\n" + strings.Repeat("middle\n", 100) + "THE VERY END\n"
	if err := os.WriteFile(spill, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel()
	m.resize(80, 400) // tall, so showLong prints instead of paging
	m.renderEvent(mkResult("Bash", "head only…\n[full output: "+spill+"]", false))

	m.showResult(nil)
	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "THE COMPLETE LOG") || !strings.Contains(out, "THE VERY END") {
		t.Errorf("/last should show the untruncated spill file:\n%s", out)
	}
	if !strings.Contains(out, "untruncated") {
		t.Error("the header should say this is the full output")
	}
}

func TestLastFallsBackWhenSpillIsGone(t *testing.T) {
	m := newTestModel()
	m.resize(80, 400)
	m.renderEvent(mkResult("Bash", "what the model saw\n[full output: /nonexistent/gone.log]", false))

	m.showResult(nil)
	if !strings.Contains(visibleText(m.transcript.String()), "what the model saw") {
		t.Error("a missing spill file should fall back to the stored content, not fail")
	}
}
