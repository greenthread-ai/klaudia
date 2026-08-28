package browser

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/chromedp/chromedp"
)

// This file decides what happens to everything chromedp's CDP client has to say
// about a session.
//
// It has to go somewhere other than the terminal. chromedp defaults its browser
// logger to log.Printf, so every message lands on stderr — which for us is the
// same terminal the inline TUI is repainting, with nothing coordinating the two.
// A raw write mid-frame overwrites whatever the renderer just painted: in one
// real case fetching a news page produced a burst of "ERROR: unhandled node
// event *dom.EventAdRelatedStateUpdated" that tore through the spinner line and
// the input box border.
//
// But "somewhere other than the terminal" was, briefly, /dev/null for the whole
// stream, and that threw away real errors along with the noise. So messages are
// split in two:
//
//   - Unmodelled-event complaints are dropped. chromedp's DOM/Page event
//     handling is a hand-written type switch (target.go), while cdproto's event
//     types are generated from the protocol, so the switch structurally trails
//     Chrome. Every event newer than it reaches the default branch and is
//     logged as an error. Today that is four DOM events cdproto knows and
//     chromedp does not: AdRelatedStateUpdated, AdoptedStyleSheetsModified,
//     AffectedByStartingStylesFlagUpdated and TopLayerElementsUpdated. All four
//     carry state chromedp's node tree doesn't track, nothing is broken, and
//     there is nothing for a user to do — which is why the fix belongs upstream
//     (a case that ignores them) and the local answer is to stop calling it an
//     error. Ad-carrying pages emit the first one per update, so it is also the
//     highest-volume message chromedp produces.
//
//   - Everything else is kept: websocket read failures, malformed protocol
//     messages, "could not retrieve document root", executor bookkeeping. Those
//     are held in a small per-browser ring and attached to the error of whatever
//     browser operation fails next (see Engine.RunWithTimeout), which is where
//     they are actually worth reading.
//
// Set KLAUDIA_BROWSER_LOG to a file path to capture the unfiltered stream,
// dropped messages included, while debugging chromedp itself.

// maxRecentProtocolMessages bounds the per-browser ring. A handful is enough to
// explain a failure; the file log is there for the full stream.
const maxRecentProtocolMessages = 10

// maxAttachedProtocolMessages bounds how much of the ring is spliced into one
// error string, so a tool result stays readable.
const maxAttachedProtocolMessages = 3

// protocolLog is the sink for one browser's chromedp logf/errf output.
type protocolLog struct {
	mu      sync.Mutex
	recent  []string
	dropped int
}

func newProtocolLog() *protocolLog { return &protocolLog{} }

// options wires the log in as chromedp's logf and errf. Both must be supplied:
// chromedp derives errf from logf when only logf is given, but keeps its
// log.Printf default for logf when only errf is given.
func (l *protocolLog) options() []chromedp.ContextOption {
	return []chromedp.ContextOption{
		chromedp.WithLogf(l.record),
		chromedp.WithErrorf(l.record),
	}
}

func (l *protocolLog) record(format string, v ...any) {
	msg := strings.TrimPrefix(fmt.Sprintf(format, v...), "ERROR: ")
	if logger := browserLogFile(); logger != nil {
		logger.Print(msg)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if unmodelledEvent(msg) {
		l.dropped++
		return
	}
	// Chrome repeats itself (one message per failing frame, per retry); the
	// second copy adds nothing and would evict something that does.
	if n := len(l.recent); n > 0 && l.recent[n-1] == msg {
		return
	}
	l.recent = append(l.recent, msg)
	if len(l.recent) > maxRecentProtocolMessages {
		l.recent = l.recent[len(l.recent)-maxRecentProtocolMessages:]
	}
}

// take returns the kept messages and clears them, so they are reported against
// the operation they preceded and not against every later one.
func (l *protocolLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.recent
	l.recent = nil
	return out
}

// unmodelledEvent reports whether msg is chromedp complaining that a CDP event
// is newer than its own type switch. Matched as a class rather than by event
// name: the next protocol release adds more, and they are noise for the same
// reason. Both default branches in chromedp's target.go are covered.
func unmodelledEvent(msg string) bool {
	return strings.HasPrefix(msg, "unhandled node event ") ||
		strings.HasPrefix(msg, "unhandled page event ")
}

// annotate adds the kept protocol messages to a failed operation's error.
func (l *protocolLog) annotate(err error) error {
	if err == nil {
		return nil
	}
	notes := l.take()
	if len(notes) == 0 {
		return err
	}
	if len(notes) > maxAttachedProtocolMessages {
		notes = notes[len(notes)-maxAttachedProtocolMessages:]
	}
	return fmt.Errorf("%w (chrome: %s)", err, strings.Join(notes, "; "))
}

var (
	browserLogOnce sync.Once
	browserLogger  *log.Logger
)

// browserLogFile returns the KLAUDIA_BROWSER_LOG logger, or nil when the
// variable is unset or the path cannot be opened. A bad debug-log path is
// deliberately silent: reporting it would be the terminal noise this file
// exists to prevent.
func browserLogFile() *log.Logger {
	browserLogOnce.Do(func() {
		path := getenv("KLAUDIA_BROWSER_LOG")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		browserLogger = log.New(f, "chrome ", log.LstdFlags)
	})
	return browserLogger
}
