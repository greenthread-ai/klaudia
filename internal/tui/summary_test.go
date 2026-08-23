package tui

import (
	"strings"
	"testing"
)

func bashResult(cmd, output string, failed bool) toolResult {
	return toolResult{tool: "Bash", command: cmd, content: output, isError: failed}
}

// A turn that changed nothing and checked nothing prints nothing. Ceremony for
// its own sake is exactly the narration §9 wants less of.
func TestEmptyTurnPrintsNothing(t *testing.T) {
	s := buildSummary(nil, nil, nil)
	if !s.Empty() {
		t.Errorf("summary is not empty: %+v", s)
	}
	if s.Render() != "" {
		t.Errorf("rendered %q for an empty turn", s.Render())
	}
	// Reading files is not a result.
	reads := []toolResult{{tool: "Read"}, {tool: "Grep"}, bashResult("ls -la", "…", false)}
	if !buildSummary(nil, reads, nil).Empty() {
		t.Error("a read-only turn produced a summary")
	}
}

// "Changed" and "verified" are different claims, and the block has to keep them
// apart — conflating them is how "I fixed it" comes to mean "it compiles".
func TestChangedAndVerifiedAreSeparate(t *testing.T) {
	touched := map[string]bool{"src/auth/session.ts": true, "test/auth/session.test.ts": true}
	results := []toolResult{
		bashResult("npm run build", "built ok", false), // building is not verifying
		bashResult("npx vitest run test/auth", "Tests: 83 passed, 83 total", false),
		bashResult("npx tsc --noEmit", "", false),
	}
	s := buildSummary(touched, results, func() (int, int, bool) { return 11, 7, true })
	out := s.Render()

	for _, want := range []string{
		"tests", "83 / 83 passing", "typecheck", "passed",
		"2 files changed", "+11 -7", "src/auth/session.ts",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "build") {
		t.Errorf("a successful build was presented as verification:\n%s", out)
	}
}

// A failed check has to stay visible. Reporting only the successes is the
// failure mode this block is meant to prevent.
func TestFailedChecksAreShown(t *testing.T) {
	results := []toolResult{
		bashResult("go test ./...", "--- FAIL: TestA\n--- FAIL: TestB\nFAIL", true),
	}
	s := buildSummary(map[string]bool{"a.go": true}, results, nil)
	out := s.Render()
	if !strings.Contains(out, "✗") {
		t.Errorf("a failing check is not marked as failing:\n%s", out)
	}
	if !strings.Contains(out, "2 failing") {
		t.Errorf("the failure count was not extracted:\n%s", out)
	}
}

func TestVerifyLabels(t *testing.T) {
	for cmd, want := range map[string]string{
		"go test ./internal/...": "tests",
		"npm test":               "tests",
		"npx vitest run":         "tests",
		"pytest -q tests/":       "tests",
		"cargo test --all":       "tests",
		"go vet ./...":           "vet",
		"npx tsc --noEmit":       "typecheck",
		"mypy src/":              "typecheck",
		"npx eslint .":           "lint",
		"golangci-lint run":      "lint",
		"npm run lint":           "lint",
	} {
		got, ok := verifyLabel(cmd)
		if !ok || got != want {
			t.Errorf("verifyLabel(%q) = %q,%v want %q", cmd, got, ok, want)
		}
	}
	// Doing work is not checking work.
	for _, cmd := range []string{
		"go build ./...", "npm run build", "git commit -m x", "ls", "make",
		"docker compose up -d", "npm install",
	} {
		if label, ok := verifyLabel(cmd); ok {
			t.Errorf("%q was counted as verification (%q)", cmd, label)
		}
	}
}

// "go vet" must not be read as "go", and "npm run typecheck" must not be read
// as "npm test" merely because it is longer.
func TestLongestVerifyMatchWins(t *testing.T) {
	if got, _ := verifyLabel("go vet ./..."); got != "vet" {
		t.Errorf("go vet → %q", got)
	}
	if got, _ := verifyLabel("npm run typecheck"); got != "typecheck" {
		t.Errorf("npm run typecheck → %q", got)
	}
}

func TestVerifyDetail(t *testing.T) {
	for _, tc := range []struct {
		output string
		ok     bool
		want   string
	}{
		{"Tests: 83 passed, 83 total", true, "83 / 83 passing"},
		{"12 passed, 14 total", true, "12 / 14 passing"},
		{"ok  	pkg	0.4s", true, "passed"},
		{"", true, "passed"},
		{"--- FAIL: TestX\n--- FAIL: TestY\n--- FAIL: TestZ", false, "3 failing"},
		{"1 failed, 2 passed", false, "1 failing"},
		{"something broke\n[exit code 2]", false, "failed (exit 2)"},
		{"unclear", false, "failed"},
	} {
		if got := verifyDetail(tc.output, tc.ok); got != tc.want {
			t.Errorf("verifyDetail(%q, %v) = %q, want %q", tc.output, tc.ok, got, tc.want)
		}
	}
}

// The same check run twice in a turn is one line, not two.
func TestRepeatedChecksCollapse(t *testing.T) {
	results := []toolResult{
		bashResult("go test ./internal/tui", "ok", false),
		bashResult("go test ./internal/agent", "ok", false),
	}
	s := buildSummary(nil, results, nil)
	if n := len(s.Checks); n != 1 {
		t.Errorf("%d checks, want 1 collapsed line", n)
	}
}

func TestNumstat(t *testing.T) {
	added, removed := numstat("11\t7\tsrc/a.go\n3\t0\tsrc/b.go\n-\t-\tlogo.png\n")
	if added != 14 || removed != 7 {
		t.Errorf("numstat = +%d -%d, want +14 -7", added, removed)
	}
	if a, r := numstat(""); a != 0 || r != 0 {
		t.Errorf("empty numstat = +%d -%d", a, r)
	}
}

// The file list is useful even outside a git repo, where line counts are not
// available.
func TestSummaryWithoutGit(t *testing.T) {
	s := buildSummary(map[string]bool{"notes.md": true}, nil, func() (int, int, bool) { return 0, 0, false })
	out := s.Render()
	if !strings.Contains(out, "1 file changed") {
		t.Errorf("output = %q", out)
	}
	if strings.Contains(out, "+0") {
		t.Errorf("a repo-less summary claimed line counts: %q", out)
	}
}

func TestResultRingSince(t *testing.T) {
	var r resultRing
	r.add(toolResult{tool: "Read"})
	mark := r.seq
	r.add(toolResult{tool: "Bash", command: "go test ./..."})
	r.add(toolResult{tool: "Edit"})

	got := r.since(mark)
	if len(got) != 2 {
		t.Fatalf("since(%d) returned %d results, want 2", mark, len(got))
	}
	if got[0].tool != "Bash" {
		t.Errorf("since returned results from before the mark: %+v", got)
	}
	if len(r.since(0)) != 3 {
		t.Error("since(0) should return everything")
	}
}
