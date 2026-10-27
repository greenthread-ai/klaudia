package replay

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestLocalSessions replays every transcript under KLAUDIA_REPLAY_DIR — your
// own ~/.klaudia/sessions, say — and fails on any finding. Off by default:
// the corpus is whatever is on the machine, which CI does not have and which
// must never be checked in.
//
//	KLAUDIA_REPLAY_DIR=~/.klaudia/sessions go test ./internal/replay -run Local -v -timeout 60m
//
// A long session is slow to replay — tens of thousands of requests, each
// re-checked whole — so sessions run in parallel and the timeout is long.
func TestLocalSessions(t *testing.T) {
	dir := os.Getenv("KLAUDIA_REPLAY_DIR")
	if dir == "" {
		t.Skip("set KLAUDIA_REPLAY_DIR to replay a local session corpus")
	}
	var paths []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			paths = append(paths, p)
		}
		return nil
	})
	type outcome struct {
		path string
		res  Result
		err  error
	}
	jobs, results := make(chan string), make(chan outcome)
	for range runtime.GOMAXPROCS(0) {
		go func() {
			for p := range jobs {
				steps, err := Load(p)
				var res Result
				if err == nil {
					res, err = Run(context.Background(), steps, Options{})
				}
				results <- outcome{p, res, err}
			}
		}()
	}
	go func() {
		for _, p := range paths {
			jobs <- p
		}
		close(jobs)
	}()
	byKind := map[string]int{}
	sessions, requests, failed := 0, 0, 0
	for range paths {
		o := <-results
		if o.err != nil {
			t.Errorf("%s: %v", o.path, o.err)
			continue
		}
		sessions++
		requests += o.res.RequestCount
		if len(o.res.Findings) > 0 {
			failed++
		}
		for i, f := range o.res.Findings {
			byKind[f.Kind]++
			if i < 3 {
				t.Errorf("%s: %s", strings.TrimSuffix(filepath.Base(o.path), ".jsonl"), f)
			}
		}
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	t.Logf("%d sessions, %d requests replayed; %d sessions with findings", sessions, requests, failed)
	for _, k := range kinds {
		t.Logf("  %-24s %d", k, byKind[k])
	}
}
