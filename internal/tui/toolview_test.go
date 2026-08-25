package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestToolSummary(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "go test ./..."}, " go test ./..."},
		{"Edit", map[string]any{"file_path": "internal/lsp/pool.go"}, " internal/lsp/pool.go"},
		{"Grep", map[string]any{"pattern": "func New"}, " func New"},
		{"WebSearch", map[string]any{"query": "go lsp client"}, " go lsp client"},
		{"Read", map[string]any{}, ""}, // missing key → no summary
	}
	for _, c := range cases {
		if got := toolSummary(c.name, c.input); got != c.want {
			t.Errorf("%s summary = %q, want %q", c.name, got, c.want)
		}
	}
	// Agent shows subagent type + quoted description.
	got := toolSummary("Agent", map[string]any{"subagent_type": "Plan", "description": "design the change"})
	if !strings.Contains(got, "(Plan)") || !strings.Contains(got, "design the change") {
		t.Errorf("Agent summary = %q", got)
	}
	// Long input is collapsed and truncated.
	long := toolSummary("Bash", map[string]any{"command": strings.Repeat("x", 200)})
	if len([]rune(long)) > maxSummary+2 || !strings.HasSuffix(long, "…") {
		t.Errorf("long summary not truncated: %d runes", len([]rune(long)))
	}
}

func TestToolDiff(t *testing.T) {
	edit := toolDiff("Edit", map[string]any{"old_string": "a := 1", "new_string": "a := 2"})
	if !strings.Contains(edit, "- ") || !strings.Contains(edit, "+ ") {
		t.Errorf("edit diff missing -/+ hunk: %q", edit)
	}
	write := toolDiff("Write", map[string]any{"content": "line1\nline2\nline3\nline4\nline5"})
	if !strings.Contains(write, "+ ") || !strings.Contains(write, "(5 lines)") {
		t.Errorf("write summary wrong: %q", write)
	}
	if toolDiff("Read", map[string]any{"file_path": "x"}) != "" {
		t.Error("non-mutating tool should have no diff")
	}
}

func TestShortMode(t *testing.T) {
	cases := map[permission.Mode]string{
		permission.ModeDefault:           "ask",
		permission.ModeAcceptEdits:       "auto-edit",
		permission.ModePlan:              "plan",
		permission.ModeDontAsk:           "deny",
		permission.ModeBypassPermissions: "bypass",
	}
	for mode, want := range cases {
		if got := shortMode(mode); got != want {
			t.Errorf("shortMode(%s) = %q, want %q", mode, got, want)
		}
	}
}

func TestStatusLine(t *testing.T) {
	m := &Model{sess: &Session{Model: "openai/gpt-5.5", PermissionMode: "plan"}, statTurns: 3, statIn: 1000, statOut: 240}
	got := stripANSI(m.statusLine())
	for _, want := range []string{"openai/gpt-5.5", "plan", "3 turns", "1.2k tokens"} {
		if !strings.Contains(got, want) {
			t.Errorf("statusLine missing %q in %q", want, got)
		}
	}
}

func TestLooksLineNumbered(t *testing.T) {
	// Read's cat -n format ("%6d\t%s") — even when the file content is Markdown
	// with code fences, the preview must NOT be treated as renderable Markdown.
	readMD := "     1\t# Title\n     2\t```go\n     3\tx := 1\n     4\t```\n"
	if !looksLineNumbered(readMD) {
		t.Errorf("Read cat -n output should be detected as line-numbered:\n%q", readMD)
	}
	// Genuine Markdown tool output (no leading line numbers) must stay eligible
	// for the Markdown render path.
	for _, s := range []string{
		"Here is the result:\n```go\nfunc main() {}\n```",
		"plain text\nwith two lines",
		"",
		"3 blind mice\nran up the clock", // a numbered word ≠ a line number (no tab)
	} {
		if looksLineNumbered(s) {
			t.Errorf("should NOT be line-numbered: %q", s)
		}
	}
}

// The mode is on the status line only when it deviates from working normally.
//
// Once autonomous became the default it appeared in almost every session, and a
// segment that never changes stops being read — while still outranking the
// context percentage, which is the one number here anyone acts on.
func TestStatusLineShowsOnlyDeviatingModes(t *testing.T) {
	for mode, want := range map[permission.Mode]string{
		permission.ModeAutonomous:        "", // the default: silent
		permission.ModeDefault:           "", // the pre-collapse default: also silent
		permission.ModePlan:              "plan",
		permission.ModeBypassPermissions: "bypass",
		permission.ModeAcceptEdits:       "auto-edit",
		permission.ModeDontAsk:           "deny",
	} {
		if got := deviatingMode(mode); got != want {
			t.Errorf("deviatingMode(%s) = %q, want %q", mode, got, want)
		}
	}
}

// Working normally, the line leads with the model and then the numbers.
func TestStatusLineOmitsTheSteadyState(t *testing.T) {
	m := newTestModel()
	m.resize(120, 40)
	m.sess.Model = "claude-opus-5"
	m.sess.PermissionMode = string(permission.ModeAutonomous)

	got := stripANSI(m.statusLine())
	for _, unwanted := range []string{"autonomous", "ask"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("status line still carries %q: %q", unwanted, got)
		}
	}
	if !strings.Contains(got, "claude-opus-5") {
		t.Errorf("the model went missing: %q", got)
	}
	if !strings.Contains(got, "turns") {
		t.Errorf("the turn count went missing: %q", got)
	}
}

// Bypass and plan still announce themselves, and outrank the counters when the
// terminal is narrow — they are the states you must not forget you are in.
func TestDeviatingModeSurvivesANarrowTerminal(t *testing.T) {
	for _, mode := range []permission.Mode{permission.ModePlan, permission.ModeBypassPermissions} {
		m := newTestModel()
		m.resize(120, 40)
		m.sess.Model = "claude-opus-5"
		m.sess.PermissionMode = string(mode)
		got := stripANSI(m.statusLineAt(30))
		if !strings.Contains(got, shortMode(mode)) {
			t.Errorf("%s was dropped from a narrow status line: %q", mode, got)
		}
	}
}

// A goal loop still reports its progress; that was the point of keeping it.
func TestGoalProgressStillShows(t *testing.T) {
	m := newTestModel()
	m.resize(120, 40)
	m.sess.PermissionMode = string(permission.ModeAutonomous)
	m.loopRemaining, m.loopTotal = 7, 10
	if got := stripANSI(m.statusLine()); !strings.Contains(got, "goal 4/10") {
		t.Errorf("goal progress missing: %q", got)
	}
}

// Bypass must not look like the token count. Asserted on the style rather than
// on rendered output, because lipgloss strips colour under a non-TTY and a test
// of the bytes would pass no matter what style was chosen.
func TestBypassIsStyledAsAWarning(t *testing.T) {
	warn := modeSegmentStyle(permission.ModeBypassPermissions)
	if !warn.GetBold() {
		t.Error("bypass is not bold; it reads as ordinary chrome")
	}
	if warn.GetForeground() == modeSegmentStyle(permission.ModePlan).GetForeground() {
		t.Error("bypass has the same colour as plan; nothing distinguishes the mode " +
			"where nothing is being checked")
	}
	// Everything else stays quiet.
	for _, mode := range []permission.Mode{
		permission.ModePlan, permission.ModeAutonomous, permission.ModeAcceptEdits,
	} {
		if modeSegmentStyle(mode).GetBold() {
			t.Errorf("%s is styled as a warning; only bypass should be", mode)
		}
	}
}

// A running job is a fact you forget until the next start collides with it.
func TestStatusLineCountsRunningJobs(t *testing.T) {
	for _, tc := range []struct {
		name string
		jobs []tools.JobStatus
		want string
	}{
		{"none", nil, ""},
		{"one", []tools.JobStatus{{Name: "dev", Running: true}}, "1 job"},
		{"two", []tools.JobStatus{
			{Name: "dev", Running: true}, {Name: "api", Running: true}}, "2 jobs"},
		{"exited ones do not count", []tools.JobStatus{
			{Name: "dev", Running: true}, {Name: "old", Running: false}}, "1 job"},
		{"all exited", []tools.JobStatus{{Name: "old", Running: false}}, ""},
	} {
		m := newTestModel()
		m.resize(120, 40)
		m.sess.Jobs = &fakeJobs{jobs: tc.jobs}
		got := stripANSI(m.statusLine())
		if tc.want == "" {
			if strings.Contains(got, "job") {
				t.Errorf("%s: line mentions jobs when none are running: %q", tc.name, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q in %q", tc.name, tc.want, got)
		}
	}
}

// No job store at all (headless, or a session without one) must not panic or
// invent a segment.
func TestStatusLineWithoutAJobStore(t *testing.T) {
	m := newTestModel()
	m.resize(120, 40)
	if got := stripANSI(m.statusLine()); strings.Contains(got, "job") {
		t.Errorf("a session with no job store reported jobs: %q", got)
	}
}
