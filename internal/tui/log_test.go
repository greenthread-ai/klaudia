package tui

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A dependency logging to the standard logger is what corrupted a frame in a
// real session (chromedp's default browser logger is log.Printf, so a burst of
// "unhandled node event" CDP noise landed on stderr mid-repaint, tearing the
// spinner line and the input box border).
func TestQuietStandardLoggerSwallowsDependencyOutput(t *testing.T) {
	var seen bytes.Buffer
	log.SetOutput(&seen)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	restore := quietStandardLogger()
	log.Printf("ERROR: unhandled node event %s", "*dom.EventAdRelatedStateUpdated")
	if seen.Len() != 0 {
		t.Fatalf("dependency log reached the terminal writer: %q", seen.String())
	}

	restore()
	log.Printf("after restore")
	if !strings.Contains(seen.String(), "after restore") {
		t.Fatalf("restore did not put the previous writer back, got %q", seen.String())
	}
}

func TestQuietStandardLoggerKeepsOutputWhenLogFileSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "klaudia.log")
	t.Setenv("KLAUDIA_LOG", path)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	restore := quietStandardLogger()
	log.Printf("ERROR: unhandled node event")
	restore()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "unhandled node event") {
		t.Fatalf("log file missing the message, got %q", data)
	}
}
