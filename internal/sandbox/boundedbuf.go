package sandbox

import (
	"fmt"
	"sync"
)

// streamKeep is how much of each end of a command's stdout or stderr is kept
// in memory. Everything between is counted and dropped.
const streamKeep = 8 << 20

// headTailBuffer is an io.Writer that keeps the first and the last n bytes
// written to it and counts what it dropped between them.
//
// Command output was collected in a bytes.Buffer and only trimmed afterwards,
// so `yes`, a verbose build or a runaway log grew the process without limit
// before the trimming ever happened. The tool shows the model a head and a
// tail of the output anyway; this keeps the same shape at capture time, with
// memory bounded at 2n per stream.
type headTailBuffer struct {
	mu      sync.Mutex
	n       int
	head    []byte
	tail    []byte // ring of the last n bytes once head is full
	pos     int    // next write position in tail when it is full
	full    bool   // tail has wrapped
	dropped int64
}

func newHeadTailBuffer(n int) *headTailBuffer { return &headTailBuffer{n: n} }

func (b *headTailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	if room := b.n - len(b.head); room > 0 {
		take := min(room, len(p))
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	for len(p) > 0 {
		if !b.full {
			take := min(b.n-len(b.tail), len(p))
			b.tail = append(b.tail, p[:take]...)
			p = p[take:]
			if len(b.tail) == b.n {
				b.full = true
				b.pos = 0
			}
			continue
		}
		// Overwrite the oldest tail bytes; each one overwritten is dropped.
		take := min(b.n-b.pos, len(p))
		copy(b.tail[b.pos:], p[:take])
		b.dropped += int64(take)
		b.pos = (b.pos + take) % b.n
		p = p[take:]
	}
	return written, nil
}

// String is the kept output: head, a line saying how much was dropped (when
// anything was), and tail, in the order they were written.
func (b *headTailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	tail := b.tail
	if b.full {
		tail = append(append([]byte{}, b.tail[b.pos:]...), b.tail[:b.pos]...)
	}
	if b.dropped == 0 {
		return string(b.head) + string(tail)
	}
	return string(b.head) + fmt.Sprintf("\n... [%d bytes of output dropped while the command ran] ...\n", b.dropped) + string(tail)
}
