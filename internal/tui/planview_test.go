package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

func items(statuses ...string) []plan.Item {
	out := make([]plan.Item, len(statuses))
	for i, s := range statuses {
		out[i] = plan.Item{Content: fmt.Sprintf("step %d", i+1), Status: s, ActiveForm: fmt.Sprintf("Doing step %d", i+1)}
	}
	return out
}

const (
	pend = plan.StatusPending
	prog = plan.StatusInProgress
	done = plan.StatusCompleted
)

func TestRenderPlan(t *testing.T) {
	twelve := append(items(done, done, done, prog), items(pend, pend, pend, pend, pend, pend, pend, pend)...)
	for i := 4; i < 12; i++ {
		twelve[i].Content = fmt.Sprintf("later %d", i+1)
	}
	tests := []struct {
		name      string
		snap      plan.Snapshot
		collapsed bool
		width     int
		// contains and absent are checked against the plain text.
		contains []string
		absent   []string
		// rows is the exact line count; 0 means the panel is hidden.
		rows int
	}{
		{name: "no plan", snap: plan.Snapshot{}, width: 80, rows: 0},
		{
			// A finished list is history; the scrollback line records it.
			name: "everything done hides it", width: 80, rows: 0,
			snap: plan.Snapshot{Items: items(done, done)},
		},
		{
			name: "short list shows every step in order", width: 80, rows: 4,
			snap:     plan.Snapshot{Approved: true, Items: items(done, prog, pend)},
			contains: []string{"Plan 1/3", "✓ step 1", "● Doing step 2", "○ step 3"},
		},
		{
			// The step being worked on reads in its active form; falling back
			// to the imperative beats a blank row when the model omits it.
			name: "missing active form falls back to the content", width: 80, rows: 2,
			snap:     plan.Snapshot{Items: []plan.Item{{Content: "write it", Status: prog}}},
			contains: []string{"● write it"},
		},
		{
			// The panel must not grow with the plan: it shares the live
			// region with the input, and clampBottom trims from the top.
			name: "long list folds and windows", width: 80, rows: planMaxRows,
			snap:     plan.Snapshot{Approved: true, Items: twelve},
			contains: []string{"Plan 3/12", "✓ 3 done", "● Doing step 4", "○ later 5", "+4 more"},
			absent:   []string{"step 1", "later 12"},
		},
		{
			// Before approval it is the model's working list, not "the
			// plan": a research checklist titled Plan read as the approved
			// plan's steps.
			name: "an unapproved list is titled Steps", width: 80, rows: 4,
			snap:     plan.Snapshot{Items: items(done, prog, pend)},
			contains: []string{"Steps 1/3"},
			absent:   []string{"Plan 1/3"},
		},
		{
			name: "collapsed is one line naming the active step", width: 80, rows: 1, collapsed: true,
			snap:     plan.Snapshot{Approved: true, Items: items(done, prog, pend)},
			contains: []string{"▸ Plan 1/3 · Doing step 2", "ctrl+t"},
		},
		{
			// A long step must not wrap: a wrapped row makes the panel's
			// height depend on prose, which is what the row cap prevents.
			name: "long step is truncated, not wrapped", width: 40, rows: 2,
			snap:     plan.Snapshot{Items: []plan.Item{{Content: strings.Repeat("x", 200), Status: pend}}},
			contains: []string{"…"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderPlan(tc.snap, tc.collapsed, tc.width)
			plain := ansi.Strip(got)
			if tc.rows == 0 {
				if got != "" {
					t.Fatalf("want hidden, got:\n%s", plain)
				}
				return
			}
			lines := strings.Split(plain, "\n")
			if len(lines) != tc.rows {
				t.Errorf("rows = %d, want %d:\n%s", len(lines), tc.rows, plain)
			}
			for _, ln := range lines {
				// The panel is never what puts a line in the last column.
				if w := ansi.StringWidth(ln); w >= tc.width {
					t.Errorf("line %q is %d wide at width %d", ln, w, tc.width)
				}
			}
			for _, want := range tc.contains {
				if !strings.Contains(plain, want) {
					t.Errorf("missing %q in:\n%s", want, plain)
				}
			}
			for _, no := range tc.absent {
				if strings.Contains(plain, no) {
					t.Errorf("unexpected %q in:\n%s", no, plain)
				}
			}
		})
	}
}

func TestTodoResultLine(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{name: "counts", in: "Todos updated:\n[x] a\n[~] b\n[ ] c", want: "plan updated · 1/3 done"},
		{
			// TodoWrite appends this when the plan file cannot be written.
			// Folding the list must not fold the warning away with it.
			name: "keeps a write-failure note",
			in:   "Todos updated:\n[x] a\n\nnote: could not save the plan: permission denied",
			want: "plan updated · 1/1 done\nnote: could not save the plan: permission denied",
		},
		{name: "cleared list is left alone", in: "Todo list cleared.", want: "Todo list cleared."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := todoResultLine(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// End to end through the model: the event reaches the live region, the panel
// is above the input while idle and running and absent at a y/n prompt, and
// Ctrl+T collapses it without reaching the text box.
func TestPlanPanelInTheLiveRegion(t *testing.T) {
	newModel := func() *Model {
		m := newTestModel()
		m.width, m.height, m.ready = 100, 40, true
		m.renderEvent(agent.Event{Type: "plan", Plan: &plan.Snapshot{Items: items(done, prog, pend), Rev: 2}})
		return m
	}

	tests := []struct {
		name  string
		state uiState
		want  bool
	}{
		{name: "idle", state: stateIdle, want: true},
		{name: "running", state: stateRunning, want: true},
		{name: "permission prompt", state: stateAwaitingPermission, want: false},
		{name: "plan approval", state: stateAwaitingPlan, want: false},
		{name: "question", state: stateAwaitingAnswer, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel()
			m.state = tc.state
			view := ansi.Strip(m.bottomView())
			if got := strings.Contains(view, "Plan 1/3"); got != tc.want {
				t.Errorf("panel shown = %v, want %v:\n%s", got, tc.want, view)
			}
			if tc.want && strings.Index(view, "Plan 1/3") > strings.Index(view, "›") {
				t.Errorf("panel is below the input:\n%s", view)
			}
		})
	}

	t.Run("ctrl+t collapses and does not reach the text box", func(t *testing.T) {
		m := newModel()
		m.input.SetValue("ab")
		_, _ = m.onKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
		view := ansi.Strip(m.bottomView())
		if !strings.Contains(view, "▸ Plan 1/3") || strings.Contains(view, "○ step 3") {
			t.Errorf("not collapsed:\n%s", view)
		}
		if m.input.Value() != "ab" {
			t.Errorf("input = %q: ctrl+t transposed instead of toggling", m.input.Value())
		}
	})

	// With no panel, Ctrl+T is the text box's transpose. Taking the key
	// unconditionally would remove that from everyone who never plans.
	t.Run("ctrl+t without a plan still transposes", func(t *testing.T) {
		m := newTestModel()
		m.width, m.height, m.ready = 100, 40, true
		m.input.Focus()
		m.input.SetValue("ab")
		_, _ = m.onKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
		if m.input.Value() != "ba" {
			t.Errorf("input = %q, want the transpose", m.input.Value())
		}
	})
}

// Battletest: approving a plan printed ExitPlanMode's six-line, model-facing
// result. The user sees that it was approved and where it was saved.
func TestExitPlanResultFoldsToOneLine(t *testing.T) {
	full := "Plan approved. Plan mode is now off — proceed with the implementation.\n\n" +
		"Record the plan's steps with TodoWrite now, before starting the first one.\n\n" +
		"The approved plan is saved at ~/.klaudia/plans/abc.md, and TodoWrite keeps its checklist in step with your list."
	if got := exitPlanResultLine(full); got != "plan approved · saved to ~/.klaudia/plans/abc.md" {
		t.Errorf("got %q", got)
	}
	refused := "The user did not approve the plan. Keep refining it in plan mode; do not modify anything yet."
	if got := exitPlanResultLine(refused); got != refused {
		t.Errorf("a refusal was folded: %q", got)
	}
	failed := "Plan approved. Plan mode is now off.\n\n(The plan could not be saved: disk full)"
	if got := exitPlanResultLine(failed); got != "plan approved (The plan could not be saved: disk full)" {
		t.Errorf("save error lost: %q", got)
	}
}
