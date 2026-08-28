package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// paste drives a bracketed paste exactly as bubbletea delivers one.
func paste(m *Model, text string) *Model {
	model, _ := m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(text)})
	return model.(*Model)
}

func newPasteModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(80, 24)
	return m
}

// The headline regression: runeutil's sanitizer maps '\r' and '\n' to "\n"
// independently, so a CRLF paste used to arrive double-spaced.
func TestPasteCRLFIsNotDoubled(t *testing.T) {
	m := paste(newPasteModel(t), "one\r\ntwo\r\nthree")
	if got := m.promptValue(); got != "one\ntwo\nthree" {
		t.Fatalf("got %q, want %q", got, "one\ntwo\nthree")
	}
}

func TestPasteWithTabsIsByteExact(t *testing.T) {
	code := "func main() {\n\tfmt.Println(\"hi\")\n}"
	m := paste(newPasteModel(t), code)
	if got := m.promptValue(); got != code {
		t.Fatalf("tabs not preserved:\n got %q\nwant %q", got, code)
	}
	if strings.Contains(m.input.Value(), "\t") {
		t.Error("tab-bearing paste should be chipped, not inlined")
	}
}

func TestPasteSmallStaysInline(t *testing.T) {
	m := paste(newPasteModel(t), "just a short line")
	if got := m.input.Value(); got != "just a short line" {
		t.Fatalf("short paste should insert verbatim, got %q", got)
	}
	if len(m.pastes.items) != 0 {
		t.Error("short paste should not allocate a chip")
	}
}

func TestPasteLargeLogIsChippedAndExpandsExactly(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "2024-01-01 INFO line %d of build output\n", i)
	}
	want := strings.TrimRight(b.String(), "\n")

	m := paste(newPasteModel(t), want)
	chip := m.input.Value()
	if !strings.Contains(chip, "1000 lines") {
		t.Fatalf("expected a chip summarising the paste, got %q", chip)
	}
	// The input box must stay small — that is the point of chipping.
	if rows := wrappedRowCount(chip, m.input.Width()); rows > 2 {
		t.Errorf("chip occupies %d rows; a 1000-line paste should not grow the box", rows)
	}
	if got := m.promptValue(); got != want {
		t.Fatalf("expansion not byte-exact: got %d bytes, want %d", len(got), len(want))
	}
}

func TestPasteUnicodeSurvives(t *testing.T) {
	s := "emoji 🎉 accents éàü CJK 日本語 box ┌─┐\nsecond line\nthird\nfourth"
	m := paste(newPasteModel(t), s)
	if got := m.promptValue(); got != s {
		t.Fatalf("unicode mangled:\n got %q\nwant %q", got, s)
	}
}

func TestPasteTypedTextAroundChipIsPreserved(t *testing.T) {
	m := newPasteModel(t)
	m.input.SetValue("explain this: ")
	m.input.CursorEnd()
	m = paste(m, "line one\nline two\nline three\nline four")

	got := m.promptValue()
	if !strings.HasPrefix(got, "explain this: ") {
		t.Fatalf("leading text lost: %q", got)
	}
	if !strings.HasSuffix(got, "line four") {
		t.Fatalf("payload not expanded at the cursor: %q", got)
	}
}

func TestTwoPastesExpandIndependently(t *testing.T) {
	m := newPasteModel(t)
	m = paste(m, "AAA\nAAA\nAAA\nAAA")
	m.input.InsertString(" and ")
	m = paste(m, "BBB\nBBB\nBBB\nBBB")

	got := m.promptValue()
	if !strings.Contains(got, "AAA\nAAA\nAAA\nAAA") || !strings.Contains(got, "BBB\nBBB\nBBB\nBBB") {
		t.Fatalf("both payloads should expand, got %q", got)
	}
	if strings.Contains(got, "[#") {
		t.Errorf("no chip should survive expansion: %q", got)
	}
}

// An edited-beyond-recognition or evicted chip must degrade to its literal
// text, never to silence.
func TestUnknownChipExpandsToItself(t *testing.T) {
	m := newPasteModel(t)
	m = paste(m, "a\nb\nc\nd")
	m.pastes.reset()
	m.input.SetValue("[#1 pasted · 4 lines]")
	if got := m.promptValue(); got != "[#1 pasted · 4 lines]" {
		t.Fatalf("unknown chip should stay literal, got %q", got)
	}
}

func TestSubmitEchoesChipButSendsPayload(t *testing.T) {
	m := newPasteModel(t)
	big := strings.Repeat("stack frame\n", 40)
	m = paste(m, big)

	sent := make(chan string, 1)
	m.ctx = context.Background()
	m.events = make(chan tea.Msg, 8)
	m.run = func(_ context.Context, prompt string, _ []anthropic.BetaMessageParam,
		_ agent.Approver, _ tools.Asker, _ tools.Planner, _ agent.Emitter, _ func() agent.Interjection, _ func(string, []string)) (agent.Result, error) {
		sent <- prompt
		return agent.Result{}, nil
	}
	model, _ := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(*Model)

	select {
	case got := <-sent:
		if !strings.Contains(got, "stack frame\nstack frame") {
			t.Errorf("the model should receive the expanded payload, got %q", oneline(got, 80))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn never started")
	}

	// Scrollback shows the chip, not forty lines of payload.
	echoed := m.transcript.String()
	if strings.Count(echoed, "stack frame") > 0 {
		t.Error("submitting a chipped paste should not dump the payload into scrollback")
	}
	if !strings.Contains(echoed, "pasted") {
		t.Errorf("expected the chip to be echoed, got:\n%s", echoed)
	}
	// History remembers the chip, so ↑ recall stays usable.
	if last := m.inputHistory[len(m.inputHistory)-1]; strings.Contains(last, "stack frame") {
		t.Error("history should remember the chip, not the payload")
	}
}

func TestHistoryRecallReExpandsChip(t *testing.T) {
	m := newPasteModel(t)
	payload := "x\ny\nz\nw"
	m = paste(m, payload)
	chip := m.input.Value()
	m.pushHistory(chip)
	m.input.Reset()

	m.navigateHistory(true)
	if m.input.Value() != chip {
		t.Fatalf("recall should restore the chip, got %q", m.input.Value())
	}
	if got := m.promptValue(); got != payload {
		t.Fatalf("recalled chip should still expand, got %q", got)
	}
}

func TestPasteIgnoredAtPrompts(t *testing.T) {
	m := newPasteModel(t)
	m.setState(stateAwaitingConfirm)
	m = paste(m, "y")
	if m.state != stateAwaitingConfirm {
		t.Fatal("a paste must not be read as an answer at a y/n prompt")
	}
	if m.input.Value() != "" {
		t.Fatalf("paste should be swallowed at prompts, input = %q", m.input.Value())
	}
}

func TestClearDropsPasteRegistry(t *testing.T) {
	m := newPasteModel(t)
	m = paste(m, "a\nb\nc\nd")
	m.input.Reset()
	if _, cmd := m.handleSlash("/clear"); cmd != nil {
		_ = cmd()
	}
	if len(m.pastes.items) != 0 {
		t.Error("/clear should drop stored paste payloads")
	}
}

func TestNormalizeNewlines(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a\r\nb", "a\nb"},
		{"a\rb", "a\nb"},
		{"a\nb", "a\nb"},
		{"none", "none"},
		{"a\r\n\r\nb", "a\n\nb"},
	} {
		if got := normalizeNewlines(tc.in); got != tc.want {
			t.Errorf("normalizeNewlines(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPasteStoreEvictsOldestOverBudget(t *testing.T) {
	var p pasteStore
	big := strings.Repeat("x", pasteStoreMaxBytes/2+1)
	first := p.add(big)
	p.add(big)
	p.add(big)
	if got := p.expand(first); got != first {
		t.Error("the oldest payload should have been evicted, leaving its chip literal")
	}
	if p.bytes > pasteStoreMaxBytes {
		t.Errorf("store holds %d bytes, over the %d budget", p.bytes, pasteStoreMaxBytes)
	}
}

// bigPaste is a chip-worthy payload (over the inline byte bound).
func bigPaste(tag string) string { return tag + strings.Repeat("x", 500) }

// The reported bug: paste, delete the chip, paste again — the number climbed
// (#1 → #2 → …) and the stored payload lingered for the whole session with no
// way to drop it. reconcile prunes a chip that no longer appears anywhere the
// user can recall it, and lets the counter fall back to #1.
func TestPasteReconcileResetsCounterWhenCleared(t *testing.T) {
	m := newPasteModel(t)
	m = paste(m, bigPaste("a"))
	if !strings.Contains(m.input.Value(), "[#1 pasted") {
		t.Fatalf("first paste chip = %q, want #1", m.input.Value())
	}
	// User deletes the chip out of the input.
	m.input.SetValue("")
	m.reconcilePastes()
	if len(m.pastes.items) != 0 {
		t.Errorf("orphaned paste not pruned: %d item(s) remain", len(m.pastes.items))
	}
	if m.pastes.next != 0 {
		t.Errorf("counter did not reset: next = %d, want 0", m.pastes.next)
	}
	// The next paste starts back at #1 rather than climbing.
	m = paste(m, bigPaste("b"))
	if !strings.Contains(m.input.Value(), "[#1 pasted") {
		t.Fatalf("second paste chip = %q, want #1 again", m.input.Value())
	}
}

// Deleting one chip must not evict a sibling still present in the input.
func TestPasteReconcilePrunesOnlyTheDeletedChip(t *testing.T) {
	m := newPasteModel(t)
	m = paste(m, bigPaste("A"))
	m = paste(m, bigPaste("B"))
	if _, ok := m.pastes.items[1]; !ok {
		t.Fatal("first paste not stored")
	}
	// Keep only the second chip in the input, then reconcile.
	second := fmt.Sprintf("[#2 pasted · %s]", pasteSummary(bigPaste("B")))
	m.input.SetValue(second)
	m.reconcilePastes()
	if _, ok := m.pastes.items[1]; ok {
		t.Error("deleted chip #1 was not pruned")
	}
	if _, ok := m.pastes.items[2]; !ok {
		t.Fatal("surviving chip #2 was wrongly pruned")
	}
	if got := m.pastes.expand(second); got != bigPaste("B") {
		t.Errorf("chip #2 no longer expands to its payload")
	}
}

// A submitted paste is referenced by the history entry (which keeps the chip
// form), so reconcile must NOT prune it — ↑ recall has to re-expand it.
func TestPasteSurvivesSubmitForHistoryRecall(t *testing.T) {
	m := newPasteModel(t)
	payload := bigPaste("secret")
	m = paste(m, payload)
	display := m.input.Value() // chip form, what pushHistory stores

	m.pushHistory(display)
	m.input.Reset()
	m.reconcilePastes()

	if _, ok := m.pastes.items[1]; !ok {
		t.Fatal("submitted paste was pruned; ↑ recall would break")
	}
	if got := m.pastes.expand(display); got != payload {
		t.Errorf("recalled chip did not expand to the original payload")
	}
}
