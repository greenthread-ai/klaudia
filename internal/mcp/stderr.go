package mcp

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// A stdio server's stderr is where it logs. os/exec connects an unset
// cmd.Stderr to the null device, and neither Klaudia nor the SDK set it, so
// every server's diagnostics were discarded: a failing tool call reached the
// model as a bare "Error executing tool" and nobody had a record of why.
//
// Each spawned server now gets a serverStderr that splits its output into
// lines and
//   - forwards them, prefixed "mcp[<name>]: ", to the writer set with
//     SetStderr (the CLI passes its own stderr for non-interactive runs; the
//     TUI owns the terminal, so interactive runs leave it unset);
//   - appends them to <dir>/<name>.log when KLAUDIA_MCP_STDERR=<dir> is set,
//     in any mode, for post-mortem;
//   - keeps the last few lines, which a failed connect appends to its error.

const (
	// stderrTailLines is how many trailing lines a connect error carries.
	stderrTailLines = 20
	// maxStderrLine caps one buffered line; a longer run without a newline is
	// emitted in pieces, so a server that never writes '\n' cannot grow the
	// buffer without bound.
	maxStderrLine = 16 * 1024
)

var (
	stderrMu  sync.Mutex // guards stderrOut and serializes writes to it
	stderrOut io.Writer
)

// SetStderr sets where stdio servers' stderr lines are forwarded (prefixed
// with the server name). Nil, the default, forwards nothing; the per-server
// log files and connect-error tails are unaffected.
func SetStderr(w io.Writer) {
	stderrMu.Lock()
	defer stderrMu.Unlock()
	stderrOut = w
}

func forward(line string) {
	stderrMu.Lock()
	defer stderrMu.Unlock()
	if stderrOut != nil {
		_, _ = io.WriteString(stderrOut, line)
	}
}

var unsafeLogName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// serverStderr is the io.Writer given to a stdio server as cmd.Stderr.
type serverStderr struct {
	name    string
	logPath string // "" = no log file

	mu      sync.Mutex
	partial []byte
	tail    []string
}

func newServerStderr(name string) *serverStderr {
	s := &serverStderr{name: name}
	if dir := strings.TrimSpace(os.Getenv("KLAUDIA_MCP_STDERR")); dir != "" {
		s.logPath = filepath.Join(dir, unsafeLogName.ReplaceAllString(name, "_")+".log")
	}
	return s
}

func (s *serverStderr) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partial = append(s.partial, p...)
	for {
		i := bytes.IndexByte(s.partial, '\n')
		if i < 0 {
			break
		}
		s.emit(string(s.partial[:i]))
		s.partial = s.partial[i+1:]
	}
	for len(s.partial) > maxStderrLine {
		s.emit(string(s.partial[:maxStderrLine]))
		s.partial = s.partial[maxStderrLine:]
	}
	s.partial = append([]byte(nil), s.partial...) // drop the consumed prefix
	return len(p), nil
}

// emit records one line. Called with s.mu held.
func (s *serverStderr) emit(line string) {
	line = strings.TrimRight(line, "\r")
	s.tail = append(s.tail, line)
	if len(s.tail) > stderrTailLines {
		s.tail = s.tail[len(s.tail)-stderrTailLines:]
	}
	forward("mcp[" + s.name + "]: " + line + "\n")
	if s.logPath != "" {
		// Opened per line: server stderr is low volume, and a reconnect makes
		// a new serverStderr, so a held handle would leak one fd per restart.
		if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err == nil {
			if f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				_, _ = f.WriteString(line + "\n")
				_ = f.Close()
			}
		}
	}
}

// Tail returns the last lines the server wrote, including an unterminated
// final line, joined by newlines.
func (s *serverStderr) Tail() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := append([]string(nil), s.tail...)
	if len(s.partial) > 0 {
		lines = append(lines, strings.TrimRight(string(s.partial), "\r"))
	}
	return strings.Join(lines, "\n")
}
