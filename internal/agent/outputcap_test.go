package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// gusherTool returns far more text than any context window should absorb, and
// — unlike Bash — makes no attempt to clamp itself. It stands in for Grep on a
// big repo, a verbose MCP server, or any tool added later by someone who did
// not think about the window.
type gusherTool struct {
	out    string
	images []tools.ResultImage
}

func (g *gusherTool) Name() string                                { return "Gusher" }
func (g *gusherTool) Description(context.Context) (string, error) { return "", nil }
func (g *gusherTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (g *gusherTool) ValidateInput(json.RawMessage) error { return nil }
func (g *gusherTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (g *gusherTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (g *gusherTool) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	return []tools.Result{{Content: g.out, Images: g.images}}, nil
}

// toolResultText pulls the model-facing text back out of a dispatched
// tool_result. (resultText in hostgate_test.go marshals the whole block to
// JSON, which is fine for Contains but not for the exact comparisons here.)
func toolResultText(t *testing.T, blk anthropic.BetaContentBlockParamUnion) string {
	t.Helper()
	tr := blk.OfToolResult
	if tr == nil {
		t.Fatal("dispatch did not return a tool_result block")
	}
	var b strings.Builder
	for _, c := range tr.Content {
		if c.OfText != nil {
			b.WriteString(c.OfText.Text)
		}
	}
	return b.String()
}

// dispatchGusher runs g through dispatch with spills going to a fresh temp dir.
func dispatchGusher(t *testing.T, g *gusherTool, emit Emitter) anthropic.BetaContentBlockParamUnion {
	t.Helper()
	return dispatchGusherSpilling(t, t.TempDir(), g, emit)
}

func dispatchGusherSpilling(t *testing.T, spillDir string, g *gusherTool, emit Emitter) anthropic.BetaContentBlockParamUnion {
	t.Helper()
	l := New(nil, tools.NewRegistry(g))
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "Gusher", Input: map[string]any{}}
	return l.dispatch(context.Background(), tu, Options{Spill: tools.SpillStore{Dir: spillDir}}, emit,
		func(...string) {}, newFailureState())
}

// The gap this closes: clampOutput and spillOutput existed, but Bash was the
// only caller. Every other tool — including every MCP tool — put its entire
// output into the context window.
func TestOversizedToolOutputIsCapped(t *testing.T) {
	full := strings.Repeat("a matching line of some considerable length here\n", 20000)
	got := toolResultText(t, dispatchGusher(t, &gusherTool{out: full}, nil))

	if len(got) >= len(full) {
		t.Fatalf("output was not capped: %d bytes through, %d produced", len(got), len(full))
	}
	if !strings.Contains(got, "bytes elided") {
		t.Error("the capped output should say that something was removed")
	}
}

// Capping without a way back is just data loss. The model must be able to read
// what was cut.
func TestCappedOutputNamesARecoverableSpillFile(t *testing.T) {
	full := strings.Repeat("a matching line of some considerable length here\n", 20000)
	got := toolResultText(t, dispatchGusher(t, &gusherTool{out: full}, nil))

	path := spillPathFrom(got)
	if path == "" {
		t.Fatalf("capped output should name a spill file:\n%s", shorten(got))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable: %v", err)
	}
	if string(data) != full {
		t.Errorf("spill should hold the complete output: got %d bytes, want %d", len(data), len(full))
	}
	// Named for the tool that produced it, so a directory of spills is
	// diagnosable rather than a pile of identical-looking logs.
	if !strings.Contains(path, "Gusher") {
		t.Errorf("spill file %q should be named after the tool", path)
	}
}

// The UI must keep showing everything the tool actually produced; only the
// model's copy is cut.
func TestCappingKeepsTheFullTextForTheUI(t *testing.T) {
	full := strings.Repeat("a matching line of some considerable length here\n", 20000)
	var ev Event
	blk := dispatchGusher(t, &gusherTool{out: full}, func(e Event) {
		if e.Type == "tool_result" {
			ev = e
		}
	})

	if ev.FullContent != full {
		t.Errorf("the tool_result event should carry the untruncated %d bytes, got %d",
			len(full), len(ev.FullContent))
	}
	if got := toolResultText(t, blk); len(got) >= len(ev.FullContent) {
		t.Error("the model's copy should be the shorter one")
	}
}

func TestSmallToolOutputIsUntouched(t *testing.T) {
	dir := t.TempDir()

	const out = "3 files matched\n"
	got := toolResultText(t, dispatchGusherSpilling(t, dir, &gusherTool{out: out}, nil))

	if got != out {
		t.Errorf("small output should pass through untouched, got %q", got)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Errorf("no spill file should have been written, found %d", len(entries))
	}
}

// Bash already clamps itself, head-and-tail, because it knows the verdict is at
// the end. The backstop must not cut that work a second time.
func TestBackstopLeavesSelfClampedToolsAlone(t *testing.T) {
	// A tool that clamped itself reports the untruncated text in Full and a
	// within-budget Content — exactly Bash's shape.
	self := strings.Repeat("already trimmed to size\n", 50)
	l := New(nil, tools.NewRegistry(&clampedTool{content: self, full: strings.Repeat("x\n", 100000)}))
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "SelfClamped", Input: map[string]any{}}
	blk := l.dispatch(context.Background(), tu, Options{}, nil,
		func(...string) {}, newFailureState())

	if got := toolResultText(t, blk); got != self {
		t.Errorf("a self-clamped result was re-cut by the backstop:\n%s", shorten(got))
	}
}

type clampedTool struct{ content, full string }

func (c *clampedTool) Name() string                                { return "SelfClamped" }
func (c *clampedTool) Description(context.Context) (string, error) { return "", nil }
func (c *clampedTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (c *clampedTool) ValidateInput(json.RawMessage) error { return nil }
func (c *clampedTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (c *clampedTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (c *clampedTool) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	return []tools.Result{{Content: c.content, Full: c.full}}, nil
}

// Images are counted and returned separately from text; capping the text must
// not drop them.
func TestCappingPreservesImages(t *testing.T) {
	g := &gusherTool{
		out:    strings.Repeat("a matching line of some considerable length here\n", 20000),
		images: []tools.ResultImage{{MediaType: "image/png", Base64: "aGk="}},
	}
	blk := dispatchGusher(t, g, nil)

	n := 0
	for _, c := range blk.OfToolResult.Content {
		if c.OfImage != nil {
			n++
		}
	}
	if n != 1 {
		t.Errorf("capping dropped the image block: got %d images, want 1", n)
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

func shorten(s string) string {
	if len(s) > 300 {
		return s[:150] + " … " + s[len(s)-150:]
	}
	return s
}
