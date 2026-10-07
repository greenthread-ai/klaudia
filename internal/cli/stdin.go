package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// pipedStdinWait bounds how long -p with a prompt already on the command line
// waits for piped stdin to produce anything. A caller that spawns Klaudia with
// a stdin pipe it never writes to or closes (Node's child_process.spawn does
// this by default) would otherwise hang forever on a run that worked before
// stdin was read. With no prompt there is nothing else to run, so the read
// blocks until EOF.
const pipedStdinWait = 3 * time.Second

// stdinIsPiped reports whether r carries piped or redirected input rather than
// a terminal. A character device — a TTY, or /dev/null — is not piped input. A
// reader that is not a file (a test's buffer) counts as piped.
func stdinIsPiped(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

// resolvePrintPrompt builds the prompt for a single-shot -p run. Piped stdin
// becomes the prompt when none was given, and is appended to one that was,
// after a blank line (`git diff | klaudia -p "review this"`). An empty result
// is a usage error: sending a request with no user message is never what the
// caller meant.
func resolvePrintPrompt(prompt string, stdin io.Reader, wait time.Duration, warn io.Writer) (string, error) {
	if stdin != nil && stdinIsPiped(stdin) {
		limit := wait
		if strings.TrimSpace(prompt) == "" {
			limit = 0
		}
		in, ok, err := readPiped(stdin, limit)
		if err != nil {
			return "", fmt.Errorf("read prompt from stdin: %w", err)
		}
		if !ok {
			fmt.Fprintf(warn, "No stdin data received in %s, proceeding without it. Redirect stdin (< /dev/null) to skip the wait.\n", wait)
		}
		if in = strings.TrimRight(in, "\r\n"); strings.TrimSpace(in) != "" {
			if strings.TrimSpace(prompt) == "" {
				prompt = in
			} else {
				prompt += "\n\n" + in
			}
		}
	}
	if strings.TrimSpace(prompt) == "" {
		return "", usageErrorf("-p needs a prompt: pass it as an argument (klaudia -p \"…\") or pipe it on stdin")
	}
	return prompt, nil
}

// readPiped reads r to EOF. With wait > 0 it gives up when nothing — neither
// data nor EOF — arrives within wait, and reports ok=false; the abandoned read
// goroutine ends with the process.
func readPiped(r io.Reader, wait time.Duration) (string, bool, error) {
	type result struct {
		b   []byte
		err error
	}
	first := make(chan struct{})
	done := make(chan result, 1)
	fr := &firstReadReader{r: r, first: first}
	go func() {
		b, err := io.ReadAll(fr)
		done <- result{b, err}
	}()
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-first:
		case <-t.C:
			return "", false, nil
		}
	}
	res := <-done
	return string(res.b), true, res.err
}

// firstReadReader closes first once the underlying reader has returned
// anything: data, EOF or an error.
type firstReadReader struct {
	r     io.Reader
	first chan struct{}
	once  sync.Once
}

func (f *firstReadReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if n > 0 || err != nil {
		f.once.Do(func() { close(f.first) })
	}
	return n, err
}
