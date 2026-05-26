package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummaryRoundTrip(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/proj"
	if err := WriteSummary(cwd, "sess-1", "The user refactored the API client and all tests pass.", "abc1234"); err != nil {
		t.Fatal(err)
	}
	body, ok := ReadSummary(cwd, "sess-1")
	if !ok {
		t.Fatal("ReadSummary returned !ok")
	}
	if body != "The user refactored the API client and all tests pass." {
		t.Errorf("body = %q (header not stripped?)", body)
	}
	if got, want := SummaryPath(cwd, "sess-1"), filepath.Join(Dir(cwd), "sess-1.summary.md"); got != want {
		t.Errorf("SummaryPath = %q, want %q", got, want)
	}
	// The on-disk file keeps the stamped header.
	raw, _ := os.ReadFile(SummaryPath(cwd, "sess-1"))
	if !strings.Contains(string(raw), "# Session summary sess-1") || !strings.Contains(string(raw), "abc1234") {
		t.Errorf("raw missing header/commit: %q", raw)
	}
}

func TestReadSummaryLegacyProjectsRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", root)
	cwd := "/work/proj"
	path := filepath.Join(root, "projects", EncodePath(cwd), "sess-legacy.summary.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Session summary sess-legacy\n\n_Compacted yesterday_\n\nlegacy summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, ok := ReadSummary(cwd, "sess-legacy")
	if !ok {
		t.Fatal("ReadSummary returned !ok")
	}
	if body != "legacy summary" {
		t.Errorf("body = %q, want legacy summary", body)
	}
}

func TestReadSummaryLocalLegacyPath(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	path := localLegacySummaryPath(cwd, "sess-legacy")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Session summary sess-legacy\n\n_Compacted yesterday_\n\nlegacy summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, ok := ReadSummary(cwd, "sess-legacy")
	if !ok {
		t.Fatal("ReadSummary returned !ok")
	}
	if body != "legacy summary" {
		t.Errorf("body = %q, want legacy summary", body)
	}
}

func TestReadSummaryMissing(t *testing.T) {
	if _, ok := ReadSummary(t.TempDir(), "nope"); ok {
		t.Error("expected !ok for missing summary")
	}
}
