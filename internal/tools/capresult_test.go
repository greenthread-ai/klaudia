package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Grep output used to go to the model whole: a broad pattern over a large
// tree could fill the context in one call.
func TestGrepOutputIsCapped(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	line := strings.Repeat("x", 200) + " needle\n"
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat(line, maxSearchResults+10)), 0o644)

	g, err := NewGrep()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"pattern": "needle", "path": dir, "output_mode": "content"})
	res, err := g.Execute(context.Background(), Context{WorkingDir: dir}, raw)
	if err != nil {
		t.Fatal(err)
	}
	r := res[0]
	if len(r.Content) > bashMaxOutput+2000 {
		t.Errorf("content is %d bytes, want it capped near %d", len(r.Content), bashMaxOutput)
	}
	if !strings.Contains(r.Content, spillMarker) || r.Full == "" {
		t.Error("capped output should name the spill file and keep the full text for display")
	}
	if !strings.Contains(r.Full, "stopped after") {
		t.Error("a search that hit the limit should say it stopped")
	}
}

func TestCapResultLeavesSmallResultsAlone(t *testing.T) {
	r := Result{Content: "small", IsError: true}
	if got := CapResult(r); got.Content != "small" || got.Full != "" || !got.IsError {
		t.Errorf("CapResult changed a small result: %+v", got)
	}
}
