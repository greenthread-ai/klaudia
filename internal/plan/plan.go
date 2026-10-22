// Package plan holds a session's plan: the step list the model tracks as it
// works, and — once the user has approved a plan — a durable Markdown artefact
// at ~/.klaudia/plans/<session-id>.md.
//
// It exists to collapse three unconnected answers to "where does the plan
// live". Before it there were: plan-mode prose, which survived only as a
// message in history where compaction could summarise it away; goal mode's
// checkbox spec, which was durable but reachable only through `/goal`; and an
// in-memory todo list built inside the tool registry, handed to nobody, which
// only the ACP frontend rendered and then from the tool *input* rather than
// from any store. A fourth parallel list — tasks.Store, behind four Task*
// tools — was registered and read by nothing at all.
//
// The artefact is keyed by session, not by project: a single per-project file
// let two sessions in the same repo silently overwrite each other's plan. It
// lives outside the project, as Claude Code's plans do, because a plan is
// per-request working state and not a spec — sitting next to goal mode's
// .klaudia/GOAL.md invited confusing the two.
//
// The body uses the checkbox format goal mode parses, so goal.CountUnchecked
// and goal.RequiresStubFix *can* read it. Nothing calls them on a plan today;
// that is a latent capability, not wiring.
package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Statuses an Item may hold. Kept as strings rather than an enum because they
// cross the wire in both directions: they are tool input from the model and
// they are rendered to the user and to ACP.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
)

// ValidStatus reports whether s is one of the three statuses.
func ValidStatus(s string) bool {
	switch s {
	case StatusPending, StatusInProgress, StatusCompleted:
		return true
	}
	return false
}

// Item is one step of the plan.
//
// The JSON tags are the stream-json wire shape: the driver writes agent.Event
// verbatim, and without tags this one payload went out PascalCase beside the
// snake_case of every other field.
type Item struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form,omitempty"`
}

// Done reports whether the item needs no more work.
func (i Item) Done() bool { return i.Status == StatusCompleted }

// Store is a session's plan. Safe for concurrent use: tool execution can be
// concurrent, and a frontend reads the list from its own goroutine.
type Store struct {
	mu       sync.Mutex
	items    []Item
	approved bool
	prose    string
	path     string
	// rev counts changes. A frontend that has been sent rev N needs nothing
	// until rev moves, and comparing two integers is how the agent loop decides
	// whether a tool batch changed the plan without diffing item lists.
	rev uint64
	// observers are notified after every change, outside the lock. This is how
	// a frontend learns the plan without polling, and the slice is append-only
	// so a late subscriber is a programming error rather than a race.
	observers []func(Snapshot)
}

// Snapshot is an immutable view of the store, as handed to observers.
type Snapshot struct {
	// Items is never omitted: an empty list is how a plan is cleared, and a
	// consumer must be able to tell "cleared" from "field absent".
	Items []Item `json:"items"`
	// Approved reports whether the user has approved a plan this session.
	Approved bool `json:"approved"`
	// Path is where the artefact lives, or "" when nothing has been written.
	Path string `json:"path,omitempty"`
	// Rev is the store's change count when the snapshot was taken. Zero means
	// the store has never changed.
	Rev uint64 `json:"rev"`
}

// Completed counts the items that are done.
func (s Snapshot) Completed() int { return len(s.Items) - s.Remaining() }

// Active returns the item currently in progress, if any.
func (s Snapshot) Active() (Item, bool) {
	for _, it := range s.Items {
		if it.Status == StatusInProgress {
			return it, true
		}
	}
	return Item{}, false
}

// Remaining counts the items that are not yet completed.
func (s Snapshot) Remaining() int {
	n := 0
	for _, it := range s.Items {
		if !it.Done() {
			n++
		}
	}
	return n
}

// New constructs a store whose approved plan is written to path, normally
// PathFor(configRoot, sessionID). An empty path disables the artefact: the list
// still works in memory, which is what tests and callers with no session want.
func New(path string) *Store { return &Store{path: path} }

// PathFor is where a session's approved plan lives under the config root
// (~/.klaudia): one file per session, so concurrent sessions in one repo — and
// a --resume, which reuses the id — each find their own plan.
func PathFor(configRoot, sessionID string) string {
	if configRoot == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(configRoot, "plans", sessionID+".md")
}

// Observe registers fn to be called after every change. It is called
// immediately with the current state, so a frontend does not need a separate
// first read and cannot miss an update that lands between the two.
func (s *Store) Observe(fn func(Snapshot)) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	s.observers = append(s.observers, fn)
	snap := s.snapshotLocked()
	s.mu.Unlock()
	fn(snap)
}

func (s *Store) snapshotLocked() Snapshot {
	snap := Snapshot{
		// Non-nil even when empty, so a cleared plan marshals as [] not null.
		Items:    append(make([]Item, 0, len(s.items)), s.items...),
		Approved: s.approved,
		Rev:      s.rev,
	}
	if s.approved {
		snap.Path = s.pathLocked()
	}
	return snap
}

// Snapshot returns the current state.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// Items returns a copy of the step list.
func (s *Store) Items() []Item { return s.Snapshot().Items }

func (s *Store) pathLocked() string { return s.path }

// Path returns where the artefact lives, or "" when the store has no path.
func (s *Store) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pathLocked()
}

// Approve records that the user accepted prose as the plan for this session and
// writes the artefact. It is the point at which the plan becomes durable.
//
// Approval is the line rather than every change because an approved plan is by
// definition multi-step work the user has blessed, where an ad-hoc three-item
// list the model keeps for its own benefit has no claim on disk. The cost of
// being wrong about that is one stale file.
func (s *Store) Approve(prose string) error {
	s.mu.Lock()
	s.approved = true
	s.prose = prose
	s.rev++
	path := s.pathLocked()
	body := s.renderLocked()
	snap := s.snapshotLocked()
	observers := append([]func(Snapshot){}, s.observers...)
	s.mu.Unlock()

	notify(observers, snap)
	return write(path, body)
}

// Approved reports whether a plan has been approved this session.
func (s *Store) Approved() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.approved
}

// Replace sets the step list, rewriting the artefact when a plan has been
// approved.
//
// Whole-list replacement rather than per-item mutation is the model-facing
// contract (TodoWrite's, and Claude Code's before it), and it is the reason the
// four Task* tools were retired rather than kept alongside: two task systems
// with different update shapes and no shared state is how the plan ends up
// disagreeing with itself.
//
// A failure to write is returned but does not roll back the in-memory list. An
// unwritable plans directory should not cost the model the plan it is working from.
func (s *Store) Replace(items []Item) error {
	s.mu.Lock()
	s.items = append([]Item(nil), items...)
	s.rev++
	path, approved := s.pathLocked(), s.approved
	body := s.renderLocked()
	snap := s.snapshotLocked()
	observers := append([]func(Snapshot){}, s.observers...)
	s.mu.Unlock()

	notify(observers, snap)
	if !approved {
		return nil
	}
	return write(path, body)
}

func notify(observers []func(Snapshot), snap Snapshot) {
	for _, fn := range observers {
		fn(snap)
	}
}

// ProgressHeading is the section the step list is written under. It is one of
// the headings goal.RequiresStubFix recognises; any other heading and goal's
// parsers would see no tracker at all and report nothing missing.
const ProgressHeading = "## Progress"

// renderLocked builds the artefact: the approved prose, then the step list as a
// checkbox section.
func (s *Store) renderLocked() string {
	var b strings.Builder
	b.WriteString("# Plan\n\n")
	if prose := strings.TrimSpace(s.prose); prose != "" {
		b.WriteString(prose)
		b.WriteString("\n\n")
	}
	b.WriteString(ProgressHeading)
	b.WriteString("\n\n")
	if len(s.items) == 0 {
		// An explicit note rather than an empty section: goal.CountUnchecked
		// reads "no unchecked boxes" as done, and a plan approved seconds ago
		// with no steps recorded yet is the opposite of done.
		b.WriteString("_No steps recorded yet._\n")
		return b.String()
	}
	for _, it := range s.items {
		box := "[ ]"
		if it.Done() {
			box = "[x]"
		}
		fmt.Fprintf(&b, "- %s %s", box, it.Content)
		if it.Status == StatusInProgress {
			// Marked in the text, not with a third box state: `- [~]` is not a
			// checkbox to any Markdown renderer, and goal's parsers count
			// "- [ ]" and "- [x]" only.
			b.WriteString(" _(in progress)_")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Render returns the artefact body as it would be written.
func (s *Store) Render() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renderLocked()
}

func write(path, body string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("plan: create %s: %w", filepath.Dir(path), err)
	}
	// Written whole every time, not appended: the list is a replacement and a
	// partially rewritten plan is worse than a stale one.
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("plan: write %s: %w", path, err)
	}
	return nil
}
