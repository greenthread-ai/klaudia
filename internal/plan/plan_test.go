package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/goal"
)

func items(statuses ...string) []Item {
	out := make([]Item, 0, len(statuses))
	for i, st := range statuses {
		out = append(out, Item{Content: string(rune('A' + i)), Status: st})
	}
	return out
}

// TestApprovalIsWhatMakesThePlanDurable: before this package, an approved plan
// survived only as prose in the message history, where compaction could
// summarise it away and --resume never saw it at all.
func TestApprovalIsWhatMakesThePlanDurable(t *testing.T) {
	tests := []struct {
		name      string
		approve   bool
		items     []Item
		wantFile  bool
		wantLines []string
	}{
		{
			name:  "an unapproved list stays in memory",
			items: items(StatusPending, StatusPending),
			// A three-item ad-hoc list the model keeps for its own benefit has
			// no claim on the user's directory.
			wantFile: false,
		},
		{
			name: "approval with no steps yet", approve: true,
			wantFile: true,
			// Not an empty section: goal.CountUnchecked reads "no unchecked
			// boxes" as done, and a plan approved a second ago is the opposite.
			wantLines: []string{"# Plan", "## Progress", "_No steps recorded yet._"},
		},
		{
			name: "approval then steps", approve: true,
			items:     []Item{{Content: "first", Status: StatusCompleted}, {Content: "second", Status: StatusInProgress}, {Content: "third", Status: StatusPending}},
			wantFile:  true,
			wantLines: []string{"- [x] first", "- [ ] second _(in progress)_", "- [ ] third"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := PathFor(t.TempDir(), "sess")
			s := New(path)
			if tc.approve {
				if err := s.Approve("the prose of the plan"); err != nil {
					t.Fatalf("Approve: %v", err)
				}
			}
			if err := s.Replace(tc.items); err != nil {
				t.Fatalf("Replace: %v", err)
			}

			body, err := os.ReadFile(path)
			if tc.wantFile != (err == nil) {
				t.Fatalf("artefact exists = %v, want %v (err %v)", err == nil, tc.wantFile, err)
			}
			if !tc.wantFile {
				// The list still has to work without a file.
				if len(s.Items()) != len(tc.items) {
					t.Errorf("in-memory list = %+v, want %d items", s.Items(), len(tc.items))
				}
				return
			}
			for _, want := range tc.wantLines {
				if !strings.Contains(string(body), want) {
					t.Errorf("artefact is missing %q:\n%s", want, body)
				}
			}
			if !strings.Contains(string(body), "the prose of the plan") {
				t.Errorf("artefact dropped the approved prose:\n%s", body)
			}
		})
	}
}

// TestTheArtefactIsReadableByGoalMode is the consolidation D3 exists for.
//
// Writing the plan in goal mode's checkbox format is not cosmetic: it makes
// goal's existing verification machinery apply to a plan that came from plan
// mode. If the format drifts, the two systems are merely adjacent again.
func TestTheArtefactIsReadableByGoalMode(t *testing.T) {
	s := New(PathFor(t.TempDir(), "sess"))
	if err := s.Approve("Rework the gate."); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace([]Item{
		{Content: "classify commands", Status: StatusCompleted},
		{Content: "gate on the verdict", Status: StatusInProgress},
		{Content: "document it", Status: StatusPending},
	}); err != nil {
		t.Fatal(err)
	}
	path := s.Path()

	n, err := goal.CountUnchecked(path)
	if err != nil {
		t.Fatalf("CountUnchecked: %v", err)
	}
	if n != 2 {
		t.Errorf("CountUnchecked = %d, want 2 — the in-progress and pending steps", n)
	}
	// The in-progress marker must not read as a third checkbox state: `- [~]`
	// is not a checkbox to any Markdown renderer, and goal counts only
	// "- [ ]" and "- [x]".
	if body, _ := os.ReadFile(path); strings.Contains(string(body), "[~]") {
		t.Errorf("artefact uses a non-standard checkbox:\n%s", body)
	}

	// Completing everything is what goal reads as done.
	if err := s.Replace([]Item{
		{Content: "classify commands", Status: StatusCompleted},
		{Content: "gate on the verdict", Status: StatusCompleted},
		{Content: "document it", Status: StatusCompleted},
	}); err != nil {
		t.Fatal(err)
	}
	if n, err = goal.CountUnchecked(path); err != nil || n != 0 {
		t.Errorf("CountUnchecked = %d (err %v), want 0", n, err)
	}

	// ProgressHeading has to be one of the headings goal recognises as a
	// tracker. Asserting "nothing missing" would not prove that: an
	// unrecognised heading means no tracker, and no tracker also reports
	// nothing missing. So the proof is positive — put a phase in the prose
	// that the tracker has no row for, and require goal to notice.
	phased := New(PathFor(t.TempDir(), "sess"))
	if err := phased.Approve("## Phase 1 — gate\n\n## Phase 2 — render"); err != nil {
		t.Fatal(err)
	}
	if err := phased.Replace([]Item{{Content: "Phase 1 — gate", Status: StatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	missing, err := goal.RequiresStubFix(phased.Path())
	if err != nil {
		t.Fatalf("RequiresStubFix: %v", err)
	}
	if len(missing) != 1 || !strings.HasPrefix(missing[0], "Phase 2") {
		t.Errorf("RequiresStubFix = %v, want just Phase 2 — %q is not being read as a tracker heading",
			missing, ProgressHeading)
	}

	// And with every phase tracked, it reports nothing.
	if err := phased.Replace([]Item{
		{Content: "Phase 1 — gate", Status: StatusCompleted},
		{Content: "Phase 2 — render", Status: StatusPending},
	}); err != nil {
		t.Fatal(err)
	}
	if missing, err = goal.RequiresStubFix(phased.Path()); err != nil || len(missing) != 0 {
		t.Errorf("RequiresStubFix = %v (err %v), want none", missing, err)
	}
}

// TestObserveDeliversTheCurrentStateFirst: a frontend must not need a separate
// first read, which it could not do without racing an update that lands between
// subscribing and reading.
func TestObserveDeliversTheCurrentStateFirst(t *testing.T) {
	s := New(PathFor(t.TempDir(), "sess"))
	if err := s.Replace(items(StatusPending)); err != nil {
		t.Fatal(err)
	}

	var got []Snapshot
	s.Observe(func(snap Snapshot) { got = append(got, snap) })
	if len(got) != 1 {
		t.Fatalf("Observe delivered %d snapshots on subscribe, want 1", len(got))
	}
	if len(got[0].Items) != 1 || got[0].Approved {
		t.Errorf("first snapshot = %+v, want the one pending item and not approved", got[0])
	}

	if err := s.Approve("prose"); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace([]Item{{Content: "a", Status: StatusInProgress}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d snapshots, want 3 (subscribe, approve, replace)", len(got))
	}
	last := got[2]
	if !last.Approved {
		t.Error("approval did not reach the observer")
	}
	if last.Path == "" {
		t.Error("an approved snapshot has no artefact path")
	}
	if active, ok := last.Active(); !ok || active.Content != "a" {
		t.Errorf("Active() = %+v, %v", active, ok)
	}
	if last.Remaining() != 1 {
		t.Errorf("Remaining() = %d, want 1", last.Remaining())
	}
}

// TestSnapshotsAreCopies: a frontend holding a snapshot must not see it change
// under it, and must not be able to mutate the store through it.
func TestSnapshotsAreCopies(t *testing.T) {
	s := New("")
	if err := s.Replace([]Item{{Content: "original", Status: StatusPending}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	snap.Items[0].Content = "mutated"
	if got := s.Items(); got[0].Content != "original" {
		t.Errorf("the store was mutated through a snapshot: %+v", got)
	}
}

// TestNoDirectoryDisablesTheArtefact covers the test and --no-project callers:
// the list works, nothing is written, and Approve does not fail.
func TestNoDirectoryDisablesTheArtefact(t *testing.T) {
	s := New("")
	if err := s.Approve("prose"); err != nil {
		t.Fatalf("Approve with no directory: %v", err)
	}
	if err := s.Replace(items(StatusPending)); err != nil {
		t.Fatalf("Replace with no directory: %v", err)
	}
	if s.Path() != "" {
		t.Errorf("Path() = %q, want empty", s.Path())
	}
	if !s.Approved() || len(s.Items()) != 1 {
		t.Error("the in-memory plan should still work without a directory")
	}
}

func TestValidStatus(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{StatusPending, true}, {StatusInProgress, true}, {StatusCompleted, true},
		{"", false}, {"done", false}, {"Pending", false},
	} {
		if got := ValidStatus(tc.status); got != tc.want {
			t.Errorf("ValidStatus(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestConcurrentSessionsKeepTheirOwnPlans: the artefact used to be one
// .klaudia/PLAN.md per project, so a second session approving a plan in the
// same repo silently overwrote the first session's.
func TestConcurrentSessionsKeepTheirOwnPlans(t *testing.T) {
	root := t.TempDir()
	first, second := New(PathFor(root, "session-a")), New(PathFor(root, "session-b"))
	if err := first.Approve("migrate the schema"); err != nil {
		t.Fatal(err)
	}
	if err := second.Approve("fix the flaky test"); err != nil {
		t.Fatal(err)
	}
	if first.Path() == second.Path() {
		t.Fatalf("both sessions write %s", first.Path())
	}
	body, err := os.ReadFile(first.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "migrate the schema") {
		t.Errorf("first session's plan was overwritten:\n%s", body)
	}
	if want := filepath.Join(root, "plans", "session-a.md"); first.Path() != want {
		t.Errorf("Path() = %s, want %s", first.Path(), want)
	}
}

func TestPathForNeedsBothParts(t *testing.T) {
	tests := []struct {
		name, root, id string
	}{
		{name: "no config root", root: "", id: "s"},
		// An empty id would collapse every session onto plans/.md — the very
		// collision this keying exists to prevent.
		{name: "no session id", root: "/r", id: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PathFor(tc.root, tc.id); got != "" {
				t.Errorf("PathFor(%q, %q) = %q, want empty", tc.root, tc.id, got)
			}
		})
	}
}

// The agent loop decides whether a tool batch changed the plan by comparing
// revisions, so a read that bumped it would emit a plan event after every tool
// call, and a change that did not would leave the TUI showing a stale list.
func TestRevMovesOnChangeOnly(t *testing.T) {
	tests := []struct {
		name  string
		act   func(s *Store)
		moved bool
	}{
		{name: "replace", act: func(s *Store) { _ = s.Replace([]Item{{Content: "a", Status: StatusPending}}) }, moved: true},
		{name: "replace with the same list still counts", act: func(s *Store) { _ = s.Replace(nil) }, moved: true},
		{name: "approve", act: func(s *Store) { _ = s.Approve("p") }, moved: true},
		{name: "snapshot", act: func(s *Store) { _ = s.Snapshot() }},
		{name: "items", act: func(s *Store) { _ = s.Items() }},
		{name: "render", act: func(s *Store) { _ = s.Render() }},
		{name: "observe", act: func(s *Store) { s.Observe(func(Snapshot) {}) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New("")
			before := s.Snapshot().Rev
			tc.act(s)
			after := s.Snapshot().Rev
			if (after != before) != tc.moved {
				t.Errorf("rev %d -> %d, want moved=%v", before, after, tc.moved)
			}
		})
	}
}

func TestCompletedCountsDoneItems(t *testing.T) {
	snap := Snapshot{Items: []Item{
		{Content: "a", Status: StatusCompleted},
		{Content: "b", Status: StatusInProgress},
		{Content: "c", Status: StatusPending},
	}}
	if got := snap.Completed(); got != 1 {
		t.Errorf("Completed() = %d, want 1", got)
	}
}
