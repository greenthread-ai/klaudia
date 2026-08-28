package tui

import (
	"io"
	"log"
	"os"
)

// quietStandardLogger stops the standard logger writing over the frame.
//
// The TUI renders inline on stdout and coordinates every write it makes. The
// standard logger writes to stderr, which is the same terminal and coordinates
// with nothing, so a single log.Printf from anywhere in the process lands in the
// middle of a repaint and corrupts it. Dependencies do exactly that: chromedp
// defaults its browser logger to log.Printf (handled at source in
// internal/browser, but it is not the only library that could).
//
// Set KLAUDIA_LOG to a file path to keep the output; otherwise it is discarded
// for the lifetime of the program. The returned func restores the previous
// destination, so a caller that runs the TUI and then prints diagnostics still
// gets them.
func quietStandardLogger() func() {
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	restore := func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
	}

	if path := os.Getenv("KLAUDIA_LOG"); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			log.SetOutput(f)
			return func() {
				restore()
				f.Close()
			}
		}
	}

	log.SetOutput(io.Discard)
	return restore
}
