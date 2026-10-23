package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// A silent non-zero exit is just "[exit code 1]" — identical for every silent
// failure — so two different ones read as "same error across different inputs"
// and loop-breaker B declared the shell wedged and refused the next command.
// In the real case that was `git status`, run to see which edits had landed
// after a script failed. Different silent failures must not trip it; the same
// command failing repeatedly still must.
func TestSilentExitsDoNotReadAsAWedgedShell(t *testing.T) {
	tests := []struct {
		name    string
		calls   []string // ids of successive Bash calls; "fail-*" exit 1 silently
		wantRan []string
		blocked string // substring the refused call's result carries, "" for none
	}{
		{
			name:    "two different silent failures, then a check",
			calls:   []string{"fail-grep", "fail-script", "git-status"},
			wantRan: []string{"fail-grep", "fail-script", "git-status"},
		},
		{
			name:    "the same silent failure repeated is still stopped",
			calls:   []string{"fail-grep", "fail-grep", "fail-grep"},
			wantRan: []string{"fail-grep", "fail-grep"},
			blocked: "already tried this exact",
		},
		{
			// Real output that repeats across inputs is still environmental
			// evidence; only the content-free exit line is set aside.
			name:    "an identical error with text across inputs still trips",
			calls:   []string{"fail-noperm-a", "fail-noperm-b", "git-status"},
			wantRan: []string{"fail-noperm-a", "fail-noperm-b"},
			blocked: "environment issue",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := &runLog{}
			bash := &probeTool{name: "Bash", log: log, run: func(_ context.Context, id string) (string, bool) {
				switch {
				case strings.HasPrefix(id, "fail-noperm"):
					return "bash: /x: Permission denied\n[exit code 1]", true
				case strings.HasPrefix(id, "fail-"):
					return "[exit code 1]", true
				}
				return "ok", false
			}}
			var turns []anthropic.BetaMessage
			for i, id := range tc.calls {
				turns = append(turns, toolUseTurn(t, "tu"+string(rune('a'+i)), "Bash", map[string]any{"id": id}))
			}
			var results []string
			_, err := New(&scriptedProvider{turns: turns}, tools.NewRegistry(bash)).Run(context.Background(), Options{
				Prompt:     "go",
				Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
			}, func(e Event) {
				if e.Type == "tool_result" {
					results = append(results, e.Content)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			var ran []string
			for _, o := range log.order() {
				ran = append(ran, strings.TrimPrefix(o, "Bash:"))
			}
			if strings.Join(ran, ",") != strings.Join(tc.wantRan, ",") {
				t.Errorf("ran %v, want %v", ran, tc.wantRan)
			}
			last := results[len(results)-1]
			if tc.blocked == "" && strings.Contains(last, "failed") {
				t.Errorf("the check was refused: %.120s", last)
			}
			if tc.blocked != "" && !strings.Contains(last, tc.blocked) {
				t.Errorf("last result %.120q lacks %q", last, tc.blocked)
			}
		})
	}
}
