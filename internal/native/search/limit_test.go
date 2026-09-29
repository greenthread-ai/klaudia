package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepLimitAndContext(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("hit\n", 50)), 0o644)
	}
	got, err := Grep(GrepOptions{Pattern: "hit", Root: dir, Limit: 60})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 61 {
		t.Errorf("matches = %d, want Limit+1 = 61 (stop, and show that it stopped)", len(got))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Grep(GrepOptions{Pattern: "hit", Root: dir, Ctx: ctx}); !errors.Is(err, context.Canceled) {
		t.Errorf("Grep with a cancelled ctx: err = %v, want context.Canceled", err)
	}
	if _, err := Glob(GlobOptions{Root: dir, Pattern: "*.txt", Ctx: ctx}); !errors.Is(err, context.Canceled) {
		t.Errorf("Glob with a cancelled ctx: err = %v, want context.Canceled", err)
	}
}
