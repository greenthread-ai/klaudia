package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	cdpdom "github.com/chromedp/cdproto/dom"
)

func TestProtocolLogOptionsSetsBothSinks(t *testing.T) {
	// chromedp keeps its log.Printf default for logf when only errf is
	// supplied, so both must be present or Chrome noise reaches stderr —
	// which for the inline TUI is the frame it is repainting.
	if got := len(newProtocolLog().options()); got != 2 {
		t.Fatalf("want logf and errf overridden, got %d options", got)
	}
}

func TestProtocolLogDropsUnmodelledEvents(t *testing.T) {
	resetBrowserLog(t)
	l := newProtocolLog()

	// The four cdproto DOM events chromedp's type switch predates. Recorded
	// through chromedp's own format string ("unhandled node event %T",
	// target.go) with the real event types, so this fails if either side
	// changes the wording or the types are handled upstream at last.
	for _, ev := range []any{
		&cdpdom.EventAdRelatedStateUpdated{},
		&cdpdom.EventAdoptedStyleSheetsModified{},
		&cdpdom.EventAffectedByStartingStylesFlagUpdated{},
		&cdpdom.EventTopLayerElementsUpdated{},
	} {
		l.record("unhandled node event %T", ev)
	}
	l.record("unhandled page event %T", struct{}{})

	if got := l.take(); len(got) != 0 {
		t.Fatalf("unmodelled-event noise kept: %q", got)
	}
	if l.dropped != 5 {
		t.Fatalf("dropped count = %d, want 5", l.dropped)
	}
}

func TestProtocolLogKeepsRealErrors(t *testing.T) {
	resetBrowserLog(t)
	l := newProtocolLog()

	l.record("unhandled node event %s", "*dom.EventAdRelatedStateUpdated")
	l.record("could not retrieve document root for %s: %v", "frame-1", errors.New("boom"))

	got := l.take()
	if len(got) != 1 || !strings.Contains(got[0], "could not retrieve document root") {
		t.Fatalf("want the real error only, got %q", got)
	}
	if left := l.take(); len(left) != 0 {
		t.Fatalf("take did not drain: %q", left)
	}
}

func TestProtocolLogStripsErrfPrefixAndRepeats(t *testing.T) {
	resetBrowserLog(t)
	l := newProtocolLog()

	// chromedp prefixes "ERROR: " only when it derives errf from logf; ours
	// are separate sinks, so the prefix is stripped for consistency.
	l.record("ERROR: %s", "websocket: close 1006")
	l.record("ERROR: %s", "websocket: close 1006")

	got := l.take()
	if len(got) != 1 || got[0] != "websocket: close 1006" {
		t.Fatalf("want one unprefixed message, got %q", got)
	}
}

func TestProtocolLogRingIsBounded(t *testing.T) {
	resetBrowserLog(t)
	l := newProtocolLog()

	for i := 0; i < maxRecentProtocolMessages*2; i++ {
		l.record("failure %d", i)
	}

	got := l.take()
	if len(got) != maxRecentProtocolMessages {
		t.Fatalf("ring held %d messages, want %d", len(got), maxRecentProtocolMessages)
	}
	// Oldest evicted, newest kept: a failure is explained by what just
	// happened, not by the start of the session.
	if want := fmt.Sprintf("failure %d", maxRecentProtocolMessages*2-1); got[len(got)-1] != want {
		t.Fatalf("last kept = %q, want %q", got[len(got)-1], want)
	}
}

func TestProtocolLogAnnotateAttachesBoundedContext(t *testing.T) {
	resetBrowserLog(t)
	l := newProtocolLog()
	for i := 0; i < maxRecentProtocolMessages; i++ {
		l.record("failure %d", i)
	}

	base := errors.New("navigate failed")
	err := l.annotate(base)
	if !errors.Is(err, base) {
		t.Fatalf("annotate broke the error chain: %v", err)
	}
	if n := strings.Count(err.Error(), "failure "); n != maxAttachedProtocolMessages {
		t.Fatalf("attached %d messages, want %d: %v", n, maxAttachedProtocolMessages, err)
	}
	if got := l.annotate(errors.New("later failure")); got.Error() != "later failure" {
		t.Fatalf("messages reported twice: %v", got)
	}
}

func TestProtocolLogAnnotateLeavesSuccessAlone(t *testing.T) {
	resetBrowserLog(t)
	l := newProtocolLog()
	l.record("something odd")
	if err := l.annotate(nil); err != nil {
		t.Fatalf("annotate(nil) = %v", err)
	}
}

func TestBrowserLogFileCapturesEverything(t *testing.T) {
	resetBrowserLog(t)
	path := filepath.Join(t.TempDir(), "chrome.log")
	t.Setenv("KLAUDIA_BROWSER_LOG", path)

	l := newProtocolLog()
	l.record("unhandled node event %s", "*dom.EventAdRelatedStateUpdated")
	l.record("%s", "websocket: close 1006")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	// The debug log is the unfiltered stream: what the ring drops still
	// lands here, which is the point of setting it.
	for _, want := range []string{"*dom.EventAdRelatedStateUpdated", "websocket: close 1006"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("%q missing from log, got %q", want, data)
		}
	}
}

func TestBrowserLogFileIgnoresUnwritablePath(t *testing.T) {
	resetBrowserLog(t)
	t.Setenv("KLAUDIA_BROWSER_LOG", filepath.Join(t.TempDir(), "no-such-dir", "chrome.log"))

	// A bad debug-log path must degrade to silence rather than become the
	// terminal noise this package exists to prevent.
	if logger := browserLogFile(); logger != nil {
		t.Fatal("want no logger for an unopenable path")
	}
	newProtocolLog().record("unhandled node event %s", "*dom.EventAdRelatedStateUpdated")
}

func TestBrowserWithoutProtocolLogPassesErrorThrough(t *testing.T) {
	// Browsers built by tests (and any future constructor) may have no log.
	base := errors.New("boom")
	if got := (&Browser{}).Diagnostics(base); !errors.Is(got, base) {
		t.Fatalf("Diagnostics dropped the error: %v", got)
	}
}

func resetBrowserLog(t *testing.T) {
	t.Helper()
	browserLogOnce = sync.Once{}
	browserLogger = nil
	t.Cleanup(func() {
		browserLogOnce = sync.Once{}
		browserLogger = nil
	})
}
