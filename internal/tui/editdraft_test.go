package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// envFrom builds an env lookup func from a map, so resolution tests never touch
// the real process environment.
func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDraftEditorCommandResolution(t *testing.T) {
	// "cat" and "echo" exist on any Unix box the tests run on and stand in for
	// real editors; we only assert which one was chosen and how args are built.
	if runtime.GOOS == "windows" {
		t.Skip("resolution test assumes POSIX cat/echo on PATH")
	}

	cases := []struct {
		name     string
		env      map[string]string
		wantBase string   // expected argv[0] basename
		wantTail []string // expected trailing args before the path
	}{
		{
			name:     "VISUAL wins over EDITOR",
			env:      map[string]string{"VISUAL": "cat", "EDITOR": "echo"},
			wantBase: "cat",
		},
		{
			name:     "EDITOR used when VISUAL empty",
			env:      map[string]string{"EDITOR": "echo"},
			wantBase: "echo",
		},
		{
			name:     "flags in EDITOR are preserved",
			env:      map[string]string{"EDITOR": "cat -n"},
			wantBase: "cat",
			wantTail: []string{"-n"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := draftEditorCommand(envFrom(tc.env), "/tmp/draft.md")
			if !ok {
				t.Fatalf("expected a command, got none")
			}
			if base := filepath.Base(cmd.Args[0]); base != tc.wantBase {
				t.Fatalf("argv[0] = %q, want basename %q", cmd.Args[0], tc.wantBase)
			}
			// argv is [bin, tail..., path].
			gotTail := cmd.Args[1 : len(cmd.Args)-1]
			if strings.Join(gotTail, " ") != strings.Join(tc.wantTail, " ") {
				t.Fatalf("middle args = %v, want %v", gotTail, tc.wantTail)
			}
			if got := cmd.Args[len(cmd.Args)-1]; got != "/tmp/draft.md" {
				t.Fatalf("path arg = %q, want /tmp/draft.md", got)
			}
		})
	}
}

func TestDraftEditorCommandFallback(t *testing.T) {
	// Neither variable set: resolution must fall back to something on PATH.
	// Point PATH at a dir holding a single stub "vi" and the fallback list at
	// it, so the test does not depend on a real vi being installed.
	dir := t.TempDir()
	stub := filepath.Join(dir, "vi")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	old := draftEditorFallbacks
	draftEditorFallbacks = []string{"vi"}
	defer func() { draftEditorFallbacks = old }()

	cmd, ok := draftEditorCommand(envFrom(nil), "/tmp/draft.md")
	if !ok {
		t.Fatal("expected fallback command, got none")
	}
	if filepath.Base(cmd.Args[0]) != "vi" {
		t.Fatalf("fallback argv[0] = %q, want vi", cmd.Args[0])
	}
}

func TestDraftEditorCommandNoneAvailable(t *testing.T) {
	// Empty env and a PATH with no fallback editor: resolution fails cleanly.
	t.Setenv("PATH", t.TempDir())
	old := draftEditorFallbacks
	draftEditorFallbacks = []string{"vi", "nano"}
	defer func() { draftEditorFallbacks = old }()

	if _, ok := draftEditorCommand(envFrom(nil), "/tmp/draft.md"); ok {
		t.Fatal("expected no command when nothing is available")
	}
}

func TestDraftFileRoundTrip(t *testing.T) {
	const content = "line one\nline two"
	path, err := writeDraftFile(content)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	got, err := readDraftFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != content {
		t.Fatalf("round-trip = %q, want %q", got, content)
	}
}

func TestReadDraftFileTrimsTrailingNewlines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.md")
	if err := os.WriteFile(path, []byte("hello\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readDraftFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("trimmed = %q, want %q", got, "hello")
	}
}

func TestApplyEditedDraftReplacesInputAndRemovesFile(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("original draft")

	path, err := writeDraftFile("edited by the user")
	if err != nil {
		t.Fatal(err)
	}
	m.applyEditedDraft(editDraftDoneMsg{path: path})

	if m.input.Value() != "edited by the user" {
		t.Fatalf("input = %q, want %q", m.input.Value(), "edited by the user")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, stat err = %v", err)
	}
}

func TestApplyEditedDraftKeepsDraftOnEditorError(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("keep me")

	// A path is still supplied (the temp file exists) but the editor errored;
	// the draft must be untouched and the file cleaned up.
	path, err := writeDraftFile("would-be replacement")
	if err != nil {
		t.Fatal(err)
	}
	m.applyEditedDraft(editDraftDoneMsg{path: path, err: os.ErrClosed})

	if m.input.Value() != "keep me" {
		t.Fatalf("input = %q, want unchanged %q", m.input.Value(), "keep me")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed even on error, stat err = %v", err)
	}
}
