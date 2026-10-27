package cli

import (
	"bytes"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

func TestNoticeWriter(t *testing.T) {
	tests := []struct {
		name string
		ev   agent.Event
		want string
	}{
		{
			name: "a hook notice reaches the operator",
			ev:   agent.Event{Type: "notice", Content: "hook: project hooks will not run"},
			want: "hook: project hooks will not run\n",
		},
		{
			name: "tool events are left to the renderer",
			ev:   agent.Event{Type: "tool_use", ToolName: "Read", Content: "x"},
		},
		{
			name: "assistant text is not duplicated onto stderr",
			ev:   agent.Event{Type: "assistant", Content: "hello"},
		},
		{
			name: "an empty notice is not a blank line",
			ev:   agent.Event{Type: "notice", Content: "  "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			noticeWriter(&buf)(tt.ev)
			if got := buf.String(); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTextFormatDropsNotices is the reason noticeWriter exists: the renderer a
// -p run writes through emits nothing for an intermediate event, so a notice
// routed only through it is lost.
func TestTextFormatDropsNotices(t *testing.T) {
	var stdout bytes.Buffer
	r := NewRenderer(FormatText, &stdout)
	if err := r.Event(agent.Event{Type: "notice", Content: "hook: something is wrong"}); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("text renderer wrote %q; the stderr path is what carries notices", stdout.String())
	}
}
