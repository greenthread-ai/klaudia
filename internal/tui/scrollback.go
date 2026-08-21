package tui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// printQueue buffers finished output on its way into the terminal's real
// scrollback.
//
// The subtlety this type exists to handle: tea.Batch does NOT run its commands
// in order. Program.execBatchMsg spawns a goroutine per command and only waits
// on a WaitGroup, so N batched tea.Println commands would deliver their
// printLineMessages in whatever order the scheduler picked — silently
// interleaving the transcript. Batching one Println per Update cycle is no
// safer, since consecutive cycles can overlap the same way.
//
// So nothing captures text in a closure. drainCmd captures the *queue*, and the
// goroutine that wins the mutex emits every line queued so far as a single
// printLineMessage in FIFO order; losers find the queue empty and return nil,
// which Bubble Tea discards. Ordering is then a property of the lock rather
// than of goroutine scheduling, and a multi-line block can never be split.
type printQueue struct {
	mu    sync.Mutex
	lines []string
}

// push enqueues an already-rendered block. Called from the Update goroutine.
func (q *printQueue) push(s string) {
	q.mu.Lock()
	q.lines = append(q.lines, s)
	q.mu.Unlock()
}

func (q *printQueue) pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.lines)
}

// drainText atomically removes everything queued and returns it as one block.
// ok is false when another caller got there first.
func (q *printQueue) drainText() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.lines) == 0 {
		return "", false
	}
	body := strings.Join(q.lines, "\n")
	q.lines = q.lines[:0]
	return body, true
}

// drainCmd returns a command that prints everything queued, or nil when there
// is nothing to print.
func (q *printQueue) drainCmd() tea.Cmd {
	if q.pending() == 0 {
		return nil
	}
	return func() tea.Msg {
		body, ok := q.drainText()
		if !ok {
			return nil // a racing sibling drained first
		}
		return tea.Println(body)()
	}
}

// transcriptBlock is one committed unit of scrollback: the source text, whether
// it was Markdown, and what we actually printed.
type transcriptBlock struct {
	text     string
	markdown bool
	rendered string
}

// transcriptLog is the in-memory record of everything printed. Once output goes
// to the terminal's scrollback we can't re-read it, so this is what /export and
// (later) conversation search work from. It keeps a rendered-text builder so
// String and Len stay O(1) rather than re-joining on every call.
type transcriptLog struct {
	blocks []transcriptBlock
	buf    strings.Builder
}

func (l *transcriptLog) add(b transcriptBlock) {
	l.blocks = append(l.blocks, b)
	l.buf.WriteString(b.rendered + "\n")
}

func (l *transcriptLog) String() string { return l.buf.String() }
func (l *transcriptLog) Len() int       { return l.buf.Len() }

func (l *transcriptLog) Reset() {
	l.blocks = nil
	l.buf.Reset()
}

// streamBuffer accumulates the in-progress assistant message. It supports
// dropping a already-flushed prefix, which strings.Builder cannot do.
type streamBuffer struct{ b []byte }

func (s *streamBuffer) WriteString(t string) { s.b = append(s.b, t...) }
func (s *streamBuffer) String() string       { return string(s.b) }
func (s *streamBuffer) bytes() []byte        { return s.b }
func (s *streamBuffer) Len() int             { return len(s.b) }
func (s *streamBuffer) Reset()               { s.b = s.b[:0] }

// trim drops the first n bytes, copying down so a long message can't pin the
// whole original backing array.
func (s *streamBuffer) trim(n int) {
	if n <= 0 {
		return
	}
	if n >= len(s.b) {
		s.b = s.b[:0]
		return
	}
	s.b = append(s.b[:0], s.b[n:]...)
}
