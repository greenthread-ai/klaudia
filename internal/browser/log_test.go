package browser

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLogOptionsSetsBothSinks(t *testing.T) {
	// chromedp only keeps its log.Printf default when neither option is
	// supplied, so both must be present or Chrome noise reaches stderr —
	// which for the inline TUI is the frame it is repainting.
	if got := len(logOptions()); got != 2 {
		t.Fatalf("want logf and errf overridden, got %d options", got)
	}
}

func TestBrowserLogfDiscardsWithoutEnv(t *testing.T) {
	resetBrowserLog(t)
	t.Setenv("KLAUDIA_BROWSER_LOG", "")

	// Must not panic or write anywhere; there is nothing to assert but the
	// absence of a destination.
	browserLogf()("unhandled node event %T", struct{}{})
}

func TestBrowserLogfWritesToFileWhenSet(t *testing.T) {
	resetBrowserLog(t)
	path := filepath.Join(t.TempDir(), "chrome.log")
	t.Setenv("KLAUDIA_BROWSER_LOG", path)

	browserLogf()("unhandled node event %s", "*dom.EventAdRelatedStateUpdated")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "unhandled node event *dom.EventAdRelatedStateUpdated") {
		t.Fatalf("message not captured, got %q", data)
	}
}

func TestBrowserLogfIgnoresUnwritablePath(t *testing.T) {
	resetBrowserLog(t)
	t.Setenv("KLAUDIA_BROWSER_LOG", filepath.Join(t.TempDir(), "no-such-dir", "chrome.log"))

	// A bad debug-log path must degrade to silence rather than become the
	// terminal noise this package exists to prevent.
	browserLogf()("unhandled node event")
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
