package agent

import "github.com/greenthread-ai/klaudia/pkg/plan"

// planTracker remembers which plan revision the frontend was last sent, so a
// plan event goes out when the plan changed and at no other time.
//
// The loop emits it rather than the frontend subscribing with plan.Store.Observe
// for three reasons. Observe has no unsubscribe and a TUI turn is a Run, so
// a per-run subscription would pile up for the whole session. An observer fires
// on whichever goroutine called TodoWrite, which inside a concurrent tool
// group is not the one emitting the batch's events. And an event in the stream
// lands immediately after the TodoWrite tool_result it belongs to, in every
// frontend, where a side channel would arrive in whatever order the scheduler
// picked.
type planTracker struct {
	sent uint64
}

// announcePlan emits a plan event when the store has moved since the last one.
//
// A fresh store sits at revision zero and so emits nothing: a session with no
// plan should not make every frontend draw an empty panel. The tracker is
// per Run, so a later turn starts at zero again and re-sends the current plan
// once. That costs one redundant snapshot per turn, and it is what lets a
// frontend that missed or discarded the last one (ACP after session/load)
// catch up without asking.
func announcePlan(store *plan.Store, emit Emitter, t *planTracker) {
	if store == nil || emit == nil {
		return
	}
	snap := store.Snapshot()
	if snap.Rev == t.sent {
		return
	}
	t.sent = snap.Rev
	emit(Event{Type: "plan", Plan: &snap})
}
