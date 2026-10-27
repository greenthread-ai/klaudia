package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

var oscLink = regexp.MustCompile("\x1b\\]8;([^;]*);([^\x1b]*)\x1b\\\\")

// links returns the URL of every opening OSC 8 in s, in order.
func links(s string) []string {
	var out []string
	for _, m := range oscLink.FindAllStringSubmatch(s, -1) {
		if m[2] != "" {
			out = append(out, m[2])
		}
	}
	return out
}

// linkedText returns the visible text inside each link segment.
func linkedText(s string) []string {
	var out []string
	for _, seg := range strings.Split(s, "\x1b]8;;\x1b\\")[:max(0, strings.Count(s, "\x1b]8;;\x1b\\"))] {
		if i := strings.LastIndex(seg, "\x1b]8;id="); i >= 0 {
			out = append(out, ansi.Strip(seg[i:]))
		}
	}
	return out
}

func render(t *testing.T, md string, width int) string {
	t.Helper()
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width))
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	return trimRenderedPadding(out)
}

// The bug this exists for: glamour wraps a long URL across rows, and the
// terminal's own detection then opens "https://example." Every row of the URL
// must carry the whole URL, under one id so the terminal treats it as one link.
func TestLinkifyWrappedURL(t *testing.T) {
	const u = "https://example.com/a/very/long/path/that/goes/on/and/on/forever?x=1"
	src := "See [the docs](" + u + ") for more."
	out := linkify(render(t, src, 40), linkTargets(src, "", ""))

	got := links(out)
	if len(got) < 2 {
		t.Fatalf("want the wrapped URL linked on each row, got %d links:\n%q", len(got), out)
	}
	ids := map[string]bool{}
	for _, m := range oscLink.FindAllStringSubmatch(out, -1) {
		if m[2] != "" {
			if m[2] != u {
				t.Errorf("segment links to %q, want the whole URL", m[2])
			}
			ids[m[1]] = true
		}
	}
	if len(ids) != 1 {
		t.Errorf("segments of one URL carry %d ids, want 1", len(ids))
	}
	if joined := strings.Join(linkedText(out), ""); joined != u {
		t.Errorf("linked text = %q, want %q", joined, u)
	}
	for _, txt := range linkedText(out) {
		if strings.Contains(txt, "the docs") {
			t.Errorf("the label was linked (%q); only the visible URL should be", txt)
		}
	}
}

func TestLinkifyKeepsVisibleTextAndClosesEveryRow(t *testing.T) {
	src := "Visit https://go.dev/doc, then https://example.com/a/very/long/path/that/goes/on/and/on/forever.\n\n- item https://github.com/foo/bar\n"
	before := render(t, src, 40)
	after := linkify(before, linkTargets(src, "", ""))
	if ansi.Strip(after) != ansi.Strip(before) {
		t.Fatalf("visible text changed:\n%q\n%q", ansi.Strip(before), ansi.Strip(after))
	}
	for i, ln := range strings.Split(after, "\n") {
		if open, closed := strings.Count(ln, "\x1b]8;id="), strings.Count(ln, "\x1b]8;;\x1b\\"); open != closed {
			t.Errorf("row %d opens %d links and closes %d — a link must not stay open across a newline: %q", i, open, closed, ln)
		}
	}
	want := []string{"https://go.dev/doc", "https://github.com/foo/bar"}
	for _, w := range want {
		found := false
		for _, l := range links(after) {
			found = found || l == w
		}
		if !found {
			t.Errorf("no link to %s; trailing punctuation should be trimmed. links: %v", w, links(after))
		}
	}
}

func TestLinkifyFilePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "pkg", "main.go")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	src := "Edited pkg/main.go:12 and pkg/missing.go; see pkg/main.go."
	out := linkify(src, linkTargets(src, dir, ""))
	got := links(out)
	if len(got) != 2 {
		t.Fatalf("want both mentions of the existing file linked, got %v in %q", got, out)
	}
	if want := "file://" + filepath.ToSlash(file); got[0] != want || got[1] != want {
		t.Errorf("links = %v, want %s", got, want)
	}
	if lt := linkedText(out); lt[0] != "pkg/main.go:12" || lt[1] != "pkg/main.go" {
		t.Errorf("linked text = %q; want the :line kept and the full stop left out", lt)
	}

	out = linkify(src, linkTargets(src, dir, "vscode://file/{path}:{line}:{col}"))
	got = links(out)
	if want := "vscode://file/" + filepath.ToSlash(file) + ":12:1"; len(got) == 0 || got[0] != want {
		t.Errorf("editor link = %v, want %s first", got, want)
	}
}

// A file name inside a longer token is not the file: "xpkg/main.go" names
// something else, and linking the tail would open the wrong thing.
func TestLinkifyNeedsTokenBoundaries(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "main.go"), nil, 0o644)
	out := linkify("xpkg/main.go pkg/main.gox", []linkTarget{{text: "pkg/main.go", url: "file:///x"}})
	if l := links(out); len(l) != 0 {
		t.Errorf("linked inside a longer token: %q", out)
	}
}

func TestTrimURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.dev/a.":                    "https://x.dev/a",
		"https://x.dev/a),":                   "https://x.dev/a",
		"https://en.wikipedia.org/wiki/A_(b)": "https://en.wikipedia.org/wiki/A_(b)",
		"https://x.dev/a]":                    "https://x.dev/a",
		"https://x.dev/**":                    "https://x.dev/",
	} {
		if got := trimURL(in); got != want {
			t.Errorf("trimURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHyperlinksEnabled(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name    string
		env     map[string]string
		setting *bool
		want    bool
	}{
		{"unknown terminal", map[string]string{"TERM": "xterm-256color"}, nil, false},
		{"iTerm", map[string]string{"TERM_PROGRAM": "iTerm.app"}, nil, true},
		{"iTerm inside tmux", map[string]string{"TERM_PROGRAM": "tmux", "LC_TERMINAL": "iTerm2"}, nil, true},
		{"ghostty", map[string]string{"TERM": "xterm-ghostty"}, nil, true},
		{"new VTE", map[string]string{"VTE_VERSION": "7600"}, nil, true},
		{"old VTE", map[string]string{"VTE_VERSION": "4200"}, nil, false},
		{"config on", map[string]string{"TERM": "xterm"}, &on, true},
		{"config off beats detection", map[string]string{"TERM_PROGRAM": "iTerm.app"}, &off, false},
		{"env beats config", map[string]string{"KLAUDIA_HYPERLINKS": "1"}, &off, true},
		{"env off", map[string]string{"TERM_PROGRAM": "iTerm.app", "KLAUDIA_HYPERLINKS": "0"}, nil, false},
		{"FORCE_HYPERLINK", map[string]string{"FORCE_HYPERLINK": "1"}, nil, true},
		{"dumb", map[string]string{"TERM": "dumb", "KITTY_WINDOW_ID": "1"}, nil, false},
	} {
		got := HyperlinksEnabled(tc.setting, func(k string) string { return tc.env[k] })
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Links go to the screen only: the transcript feeds /copy, /export and $PAGER.
func TestCommitLinksScreenNotTranscript(t *testing.T) {
	m := New(t.Context(), nil, nil, &Session{Hyperlinks: true})
	m.resize(100, 30)
	m.appendLine("see https://go.dev/doc")
	if strings.Contains(m.transcript.String(), "\x1b]8;") {
		t.Errorf("transcript holds OSC 8: %q", m.transcript.String())
	}
	body, _ := m.out.drainText()
	if l := links(body); len(l) != 1 || l[0] != "https://go.dev/doc" {
		t.Errorf("screen links = %v, want the URL", l)
	}
}

// /doctor's links row: the decision and the reason, which is the fix.
func TestHyperlinksDecisionSaysWhy(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	for _, tc := range []struct {
		kv   map[string]string
		on   bool
		want string
	}{
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, true, "detected iTerm.app"},
		{map[string]string{"KLAUDIA_HYPERLINKS": "0", "TERM_PROGRAM": "iTerm.app"}, false, "KLAUDIA_HYPERLINKS=0"},
		{map[string]string{"TERM": "xterm-256color"}, false, "KLAUDIA_HYPERLINKS=1"},
	} {
		on, why := HyperlinksDecision(nil, env(tc.kv))
		if on != tc.on || !strings.Contains(why, tc.want) {
			t.Errorf("%v: %v %q", tc.kv, on, why)
		}
	}
}
