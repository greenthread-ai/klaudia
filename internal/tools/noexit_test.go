package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

func TestNoMatchExit(t *testing.T) {
	one := sandbox.Response{ExitCode: 1}
	for cmd, want := range map[string]bool{
		"grep -r TODO src":     true,
		"cat log | grep ERROR": true,
		"cd src && rg needle":  true,
		"diff a.txt b.txt":     true,
		"/usr/bin/cmp a b":     true,
		"[ -f go.mod ]":        true,
		"grep x f | wc -l":     false, // wc's status, not grep's
		"go test ./...":        false,
		"grep x f; false":      false,
	} {
		if got := noMatchExit(cmd, one); got != want {
			t.Errorf("noMatchExit(%q) = %v, want %v", cmd, got, want)
		}
	}
	if noMatchExit("grep x f", sandbox.Response{ExitCode: 2}) {
		t.Error("grep exit 2 is an error (bad pattern, missing file)")
	}
	if noMatchExit("grep x f", sandbox.Response{ExitCode: 1, TimedOut: true}) {
		t.Error("a timeout is not a no-match")
	}
}

// Through the real tool: a grep that finds nothing is an answer.
func TestBashGrepNoMatchIsNotError(t *testing.T) {
	b, err := NewBash(sandbox.NewLocal())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := b.Execute(context.Background(), Context{WorkingDir: dir}, json.RawMessage(`{"command":"echo hello > f.txt; grep absent f.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res[0].IsError || !strings.Contains(res[0].Content, "no match") {
		t.Errorf("result = %+v, want a non-error no-match", res[0])
	}
	res, _ = b.Execute(context.Background(), Context{WorkingDir: dir}, json.RawMessage(`{"command":"grep absent missing.txt"}`))
	if !res[0].IsError {
		t.Error("grep on a missing file (exit 2) should still be an error")
	}
}
