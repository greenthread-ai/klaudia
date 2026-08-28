package browser

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/chromedp/chromedp"
)

// logOptions keeps Chrome's protocol chatter off the terminal.
//
// chromedp defaults its browser logger to log.Printf, so every message lands on
// stderr — which for us is the same terminal the inline TUI is redrawing, and
// nothing coordinates the two. A raw write mid-frame overwrites whatever the
// renderer just painted: in one real case fetching a news page produced a burst
// of "ERROR: unhandled node event *dom.EventAdRelatedStateUpdated" that tore
// through the spinner line and the input box border.
//
// That message is a good example of why this is noise rather than a signal:
// EventAdRelatedStateUpdated is a CDP DOM event newer than the type switch in
// chromedp's target.go, so any ad-carrying page logs one per update. Nothing is
// broken and there is nothing for the user to do. Set KLAUDIA_BROWSER_LOG to a
// file path to capture it while debugging; otherwise it is dropped.
func logOptions() []chromedp.ContextOption {
	sink := browserLogf()
	return []chromedp.ContextOption{
		chromedp.WithLogf(sink),
		chromedp.WithErrorf(sink),
	}
}

var (
	browserLogOnce sync.Once
	browserLogger  *log.Logger
)

func browserLogf() func(string, ...any) {
	browserLogOnce.Do(func() {
		path := getenv("KLAUDIA_BROWSER_LOG")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			// Deliberately silent: a bad debug-log path must not become
			// terminal noise, which is the very thing this file prevents.
			return
		}
		browserLogger = log.New(f, "chrome ", log.LstdFlags)
	})
	if browserLogger == nil {
		return func(string, ...any) {}
	}
	return func(format string, v ...any) {
		browserLogger.Print(fmt.Sprintf(format, v...))
	}
}
