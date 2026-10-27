package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

func TestSpillWritesFullOutputAndFormatNamesIt(t *testing.T) {
	full := strings.Repeat("build output line\n", 5000)
	out, fullOut := formatBashOutput(sandbox.Response{Stdout: full}, "make build", SpillStore{Dir: t.TempDir()})

	if !strings.Contains(out, "full output:") {
		t.Fatalf("truncated output should name the spill file:\n%s", oneLineOf(out))
	}
	path := spillPathFrom(out)
	if path == "" {
		t.Fatal("could not parse the spill path out of the notice")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable: %v", err)
	}
	if string(data) != full {
		t.Errorf("spill should hold the complete output: got %d bytes, want %d", len(data), len(full))
	}
	// The UI channel carries the same untruncated text, without a disk read.
	if fullOut != full {
		t.Errorf("Full = %d bytes, want the untruncated %d", len(fullOut), len(full))
	}
	if len(out) >= len(fullOut) {
		t.Error("the model-facing copy should be shorter than the full one")
	}
}

func TestNoSpillForShortOutput(t *testing.T) {
	dir := t.TempDir()

	out, _ := formatBashOutput(sandbox.Response{Stdout: "all good\n"}, "echo hi", SpillStore{Dir: dir})
	if strings.Contains(out, "full output:") {
		t.Error("short output needs no spill file")
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Errorf("no spill file should have been written, found %d", len(entries))
	}
}

// Exit status and timeout annotations must still land after clamping.
func TestExitAnnotationSurvivesClamping(t *testing.T) {
	out, fullOut := formatBashOutput(sandbox.Response{
		Stdout: strings.Repeat("noise\n", 20000), ExitCode: 1,
	}, "go test ./...", SpillStore{Dir: t.TempDir()})
	if !strings.Contains(out, "[exit code 1]") {
		t.Errorf("exit annotation lost:\n%s", oneLineOf(out))
	}
	// A reader of the full output still needs to know the command failed.
	if !strings.Contains(fullOut, "[exit code 1]") {
		t.Error("exit annotation missing from the full output")
	}
}

// End to end through the real Bash tool with a real command.
func TestBashEndToEndKeepsHeadAndTail(t *testing.T) {
	b, err := NewBash(sandbox.NewLocal())
	if err != nil {
		t.Fatal(err)
	}
	res, err := b.Execute(context.Background(), Context{Spill: SpillStore{Dir: t.TempDir()}}, []byte(`{"command":"echo FIRST_LINE_MARKER; for i in $(seq 1 6000); do echo filler line $i padding padding padding; done; echo LAST_LINE_MARKER; exit 3"}`))
	if err != nil {
		t.Fatal(err)
	}
	out := res[0].Content
	t.Logf("model sees %d bytes, UI gets %d", len(out), len(res[0].Display()))
	if res[0].Full == "" {
		t.Error("a clamped result must carry the untruncated text for the UI")
	}
	if len(res[0].Display()) <= len(out) {
		t.Error("Display() should be the longer, untruncated variant")
	}
	for _, want := range []string{"FIRST_LINE_MARKER", "LAST_LINE_MARKER", "bytes elided", "[exit code 3]", "full output:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if path := spillPathFrom(out); path == "" {
		t.Fatal("the model-facing output should name the spill file it can read")
	}
}
func spillPathFrom(out string) string {
	const marker = "[full output: "
	i := strings.Index(out, marker)
	if i < 0 {
		return ""
	}
	rest := out[i+len(marker):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func oneLineOf(s string) string {
	if len(s) > 300 {
		s = s[:150] + " … " + s[len(s)-150:]
	}
	return s
}
