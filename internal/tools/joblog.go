package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A job's output goes to a file, not a slice.
//
// Before this, background output accumulated in a byte slice that only grew:
// a dev server left running for an afternoon would hold every line it ever
// printed in Klaudia's heap, and none of it could be paged, searched, or read
// after the session ended. A file fixes all three at once, and it is what makes
// `/logs` able to hand the user's own $PAGER something real.

// jobLogDir is where job logs live, alongside the existing output spills.
func jobLogDir(session string) string {
	base := os.Getenv("KLAUDIA_CONFIG_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".klaudia")
	}
	if session == "" {
		session = "default"
	}
	return filepath.Join(base, "jobs", session)
}

// jobLog is an append-only log file with a byte cursor for incremental reads.
type jobLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	n    int64 // bytes written
	// memOnly holds output when no file could be opened. Falling back to memory
	// keeps a job working on a read-only home directory; it is bounded so the
	// degraded path cannot become the unbounded growth this type exists to fix.
	memOnly []byte
}

// maxMemLog bounds the in-memory fallback (256 KiB), keeping the tail.
const maxMemLog = 256 << 10

func newJobLog(dir, name string) *jobLog {
	l := &jobLog{}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return l
	}
	path := filepath.Join(dir, name+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return l
	}
	l.path, l.f = path, f
	return l
}

func (l *jobLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		l.memOnly = append(l.memOnly, b...)
		if len(l.memOnly) > maxMemLog {
			l.memOnly = l.memOnly[len(l.memOnly)-maxMemLog:]
		}
		l.n = int64(len(l.memOnly))
		return len(b), nil
	}
	n, err := l.f.Write(b)
	l.n += int64(n)
	return n, err
}

// note writes a Klaudia-generated line into the log, marked so it cannot be
// mistaken for the program's own output.
func (l *jobLog) note(format string, args ...any) {
	_, _ = l.Write([]byte(fmt.Sprintf("\n── klaudia: "+format+" ──\n", args...)))
}

// ReadFrom returns the bytes written since offset, and the new offset.
func (l *jobLog) ReadFrom(offset int64) (string, int64) {
	l.mu.Lock()
	path, size, mem := l.path, l.n, l.memOnly
	l.mu.Unlock()

	if offset < 0 || offset > size {
		offset = size
	}
	if path == "" {
		return string(mem[offset:]), size
	}
	f, err := os.Open(path)
	if err != nil {
		return "", size
	}
	defer f.Close()
	if _, err := f.Seek(offset, 0); err != nil {
		return "", size
	}
	buf := make([]byte, size-offset)
	n, _ := f.Read(buf)
	return string(buf[:n]), size
}

// Size is the number of bytes written so far.
func (l *jobLog) Size() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

// Path is the log file, or "" when the log is memory-only.
func (l *jobLog) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Tail returns the last n lines.
func (l *jobLog) Tail(n int) string {
	all, _ := l.ReadFrom(0)
	lines := strings.Split(strings.TrimRight(all, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (l *jobLog) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}

// pruneJobLogs removes job-log directories older than maxAge. Session
// directories are left behind by every run, and nothing else would ever clean
// them up.
func pruneJobLogs(root string, maxAge time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}
