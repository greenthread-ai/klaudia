package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden request snapshots")

// The corpus is transcripts of the real binary driven by the e2e suite's
// mock model (KLAUDIA_E2E_KEEP; see internal/tui/e2e/main_test.go), chosen to
// cover a multi-tool turn, a failing command, rewind and "summarise up to
// here", an interrupted turn, a question, plan approval, a goal run and a
// background agent. No real session belongs here.
//
// Each must replay with no findings, and the requests the loop builds must
// match the golden snapshot. A diff there is a change in what the model is
// sent: if it is intended, regenerate with
//
//	go test ./internal/replay -run Corpus -update
//
// and the snapshot's diff is the review.
func TestCorpus(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("testdata", "corpus", "*.jsonl"))
	if len(paths) == 0 {
		t.Fatal("no corpus")
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		t.Run(name, func(t *testing.T) {
			steps, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Run(context.Background(), steps, Options{KeepRequests: true})
			if err != nil {
				t.Fatal(err)
			}
			if res.RequestCount == 0 {
				t.Fatal("the replay made no requests")
			}
			for _, f := range res.Findings {
				t.Error(f)
			}
			got, err := json.MarshalIndent(res.Requests, "", " ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := filepath.Join("testdata", "golden", name+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (generate it with -update)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("the requests differ from %s — what the model is sent has changed. "+
					"If that is intended, regenerate with -update and review the diff.\n%s", golden, firstDiff(want, got))
			}
		})
	}
}

// firstDiff shows the first line where two snapshots part.
func firstDiff(want, got []byte) string {
	w, g := strings.Split(string(want), "\n"), strings.Split(string(got), "\n")
	for i := 0; i < len(w) && i < len(g); i++ {
		if w[i] != g[i] {
			return fmt.Sprintf("line %d:\n  want %s\n  got  %s", i+1, w[i], g[i])
		}
	}
	return fmt.Sprintf("lengths differ: want %d lines, got %d", len(w), len(g))
}
