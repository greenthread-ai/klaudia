package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// A model that sends "180" where the schema says 180 means exactly one thing.
// Rejecting it cost a turn and, worse, counted as a pre-exec failure: two in a
// row and loop-breaker B tells the model to stop using the tool altogether.
//
// Real case (Read on a 288-line file): `{"limit":130,"offset":"180, "}`.
func TestDispatchCoercesStringEncodedScalars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, "line"+strconv.Itoa(i))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))
	opts := Options{Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)}}

	in, _ := json.Marshal(map[string]any{"file_path": path, "offset": "180, ", "limit": "2"})
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "Read", Input: json.RawMessage(in)}

	var ev []Event
	emit := func(e Event) { ev = append(ev, e) }
	got := resultText(l.dispatch(context.Background(), tu, opts, emit, func(...string) {}, newFailureState()))

	if strings.Contains(got, "validation error") {
		t.Fatalf("coercible call rejected: %q", got)
	}
	if !strings.Contains(got, "line180") || !strings.Contains(got, "line181") {
		t.Fatalf("offset/limit not applied: %q", got)
	}
	if strings.Contains(got, "line182") {
		t.Fatalf("limit not applied: %q", got)
	}
	// The displayed call must be what actually ran, not the string the model
	// sent, or the transcript shows an input the tool never saw.
	for _, e := range ev {
		if e.Type != "tool_use" {
			continue
		}
		shown, _ := json.Marshal(e.Input)
		if strings.Contains(string(shown), `"180`) && strings.Contains(string(shown), `"offset":"`) {
			t.Errorf("tool_use event still shows the uncoerced input: %s", shown)
		}
	}
}
