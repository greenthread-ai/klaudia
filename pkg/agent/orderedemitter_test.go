package agent

import (
	"reflect"
	"testing"
)

// The case a parallel batch of Reads hit: the second call has emitted its
// tool_use (buffered, not at the head) and is still running when the first
// finishes. It becomes the head and streams live, so its buffered tool_use
// must go out before its result — before, it stayed buffered until the call
// finished, and the transcript showed the result above its own tool line.
func TestOrderedEmitterFlushesTheNewHeadsBuffer(t *testing.T) {
	var got []string
	o := newOrderedEmitter(func(ev Event) { got = append(got, ev.Type+":"+ev.ToolUseID) }, 2)
	a, b := o.emitter(0), o.emitter(1)

	a(Event{Type: EventToolUse, ToolUseID: "a"})
	b(Event{Type: EventToolUse, ToolUseID: "b"}) // buffered: a is the head
	a(Event{Type: EventToolResult, ToolUseID: "a"})
	o.done(0)                                       // b is now the head, still running
	b(Event{Type: EventToolResult, ToolUseID: "b"}) // live
	o.done(1)

	want := []string{"tool_use:a", "tool_result:a", "tool_use:b", "tool_result:b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}
