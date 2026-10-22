package trust

import (
	"strings"
	"testing"
)

// TestInspectCommand is the plan-mode gate's whole decision surface.
//
// The first group is the measured regression: these are the commands a real
// planning session in this repository was refused, which left the plan it
// produced carrying the caveat "`main` is behind 13, I have not looked at what
// landed". Every one of them must now run.
func TestInspectCommand(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		want   Mutation
		reason string // substring of the explanation; "" skips the check
	}{
		// Refused in the measured session. All research, all read-only.
		{name: "git status", cmd: "git status --short", want: MutationNone},
		{name: "git log", cmd: "git log --oneline -13", want: MutationNone},
		{name: "git diff against a ref", cmd: "git diff HEAD~13 --stat", want: MutationNone},
		{name: "sed as a ranged read", cmd: "sed -n '1,40p' internal/trust/trust.go", want: MutationNone},
		{name: "grep", cmd: "grep -rn ClassifyCommand internal", want: MutationNone},
		{name: "go list", cmd: "go list -m all", want: MutationNone},
		{name: "go doc", cmd: "go doc permission.Check", want: MutationNone},

		// Plainly read-only, via the shared program catalog.
		{name: "cat", cmd: "cat go.mod", want: MutationNone},
		{name: "a read-only pipeline", cmd: "git log --format=%an | sort | uniq -c", want: MutationNone},
		{name: "a read-only chain", cmd: "git rev-parse HEAD && git status", want: MutationNone},
		{name: "find as a glob", cmd: "find . -name '*.go' -newer go.mod", want: MutationNone},
		{name: "go mod why reads", cmd: "go mod why mvdan.cc/sh/v3", want: MutationNone},
		{name: "a wrapper around a read", cmd: "timeout 5 git log -1", want: MutationNone},
		{name: "an inline shell that only reads", cmd: `bash -c 'git log -1 && cat go.mod'`, want: MutationNone},

		// Known to change the project. Denied, and the reason has to name it.
		{name: "rm", cmd: "rm -rf build", want: MutationPresent, reason: "rm writes"},
		{name: "git commit", cmd: "git commit -m x", want: MutationPresent, reason: "writes a commit"},
		{name: "git checkout", cmd: "git checkout main", want: MutationPresent, reason: "working tree"},
		{name: "go mod tidy", cmd: "go mod tidy", want: MutationPresent, reason: "go.sum"},
		{name: "go generate", cmd: "go generate ./...", want: MutationPresent, reason: "write files"},
		{name: "sed in place", cmd: "sed -i '' s/a/b/ main.go", want: MutationPresent, reason: "which makes it write"},
		{name: "sed in place, bundled", cmd: "sed -ni s/a/b/ main.go", want: MutationPresent, reason: "which makes it write"},
		{name: "sed in place with a backup suffix", cmd: "sed -i.bak s/a/b/ main.go", want: MutationPresent, reason: "which makes it write"},
		{name: "find that deletes", cmd: "find . -name '*.tmp' -delete", want: MutationPresent, reason: "which makes it write"},
		{name: "find that execs", cmd: "find . -name '*.go' -exec touch {} ;", want: MutationPresent, reason: "which makes it write"},
		{name: "sort writing its output", cmd: "sort -o sorted.txt in.txt", want: MutationPresent, reason: "which makes it write"},

		// The build-and-test class. Denied on the user's call: plan mode is for
		// anything that does not modify files, and these can both land one.
		{name: "go build", cmd: "go build ./...", want: MutationPresent, reason: "binary"},
		{name: "go test", cmd: "go test ./internal/...", want: MutationPresent, reason: "test code"},
		{name: "go run", cmd: "go run ./cmd/klaudia", want: MutationPresent, reason: "executes"},
		{name: "go install", cmd: "go install ./cmd/klaudia", want: MutationPresent, reason: "GOBIN"},

		// Redirections disqualify the line whatever the program runs.
		{name: "a redirection off a read-only program", cmd: "echo hi > f.txt", want: MutationPresent, reason: "f.txt"},
		{name: "an append off a read-only program", cmd: "git log >> log.txt", want: MutationPresent, reason: "log.txt"},
		{name: "a redirection at the end of a chain", cmd: "git status && git log > out", want: MutationPresent, reason: "out"},
		{name: "a redirection to an unreadable target", cmd: "git log > $OUT", want: MutationPresent, reason: "redirects output"},

		// Changing directory writes nothing. Its absence made plan mode ask
		// about `cd <repo> && grep …`, the commonest research line there is.
		{name: "cd before a read", cmd: `cd /repo && grep -n "^T1" docs/BACKLOG.md | cut -c1-230; grep -n Kind internal/*.go | head`, want: MutationNone},
		{name: "pushd and popd", cmd: "pushd internal && git log -1 && popd", want: MutationNone},
		{name: "cd does not launder a write", cmd: "cd /etc && echo x > passwd", want: MutationPresent, reason: "passwd"},
		{name: "cd before a mutating program", cmd: "cd build && rm -rf out", want: MutationPresent, reason: "rm writes"},

		// Discarding output writes nothing. `2>/dev/null` was refused in a real
		// planning session; /tmp is a real file and stays a write, and a
		// traversal out of /dev/fd must not pass as a descriptor.
		{name: "stderr discarded", cmd: "grep -rn x internal 2>/dev/null", want: MutationNone},
		{name: "all output discarded", cmd: "git log &>/dev/null", want: MutationNone},
		{name: "stderr folded into a pipe", cmd: "git log 2>&1 | head", want: MutationNone},
		{name: "a redirection into /tmp", cmd: "git log > /tmp/x", want: MutationPresent, reason: "/tmp/x"},
		{name: "a traversal out of /dev/fd", cmd: "git log > /dev/fd/../../etc/x", want: MutationPresent, reason: "/dev/fd/../../etc/x"},
		{name: "a block device", cmd: "git log > /dev/sda", want: MutationPresent, reason: "/dev/sda"},

		// One mutating component condemns the whole line.
		{name: "a read piped into a writer", cmd: "git log | tee log.txt", want: MutationPresent, reason: "tee writes"},
		{name: "a read chained before a write", cmd: "git status && rm -rf build", want: MutationPresent, reason: "rm writes"},
		{name: "an inline shell that writes", cmd: `bash -c 'rm -rf build'`, want: MutationPresent, reason: "rm writes"},

		// Not determinable. These ask the user rather than being refused — the
		// property that keeps a stale allowlist from recreating the original
		// bug, where an unlisted program meant research was impossible.
		{name: "an unclassified program", cmd: "cargo check", want: MutationUnclear, reason: "classifies cargo"},
		{name: "an unclassified git subcommand", cmd: "git branch -d old", want: MutationUnclear, reason: "git branch"},
		{name: "an unclassified go subcommand", cmd: "go telemetry off", want: MutationUnclear, reason: "go telemetry"},
		{name: "an interpreter", cmd: "python3 script.py", want: MutationUnclear, reason: "classifies python3"},
		{name: "a program name from an expansion", cmd: "$TOOL --version", want: MutationUnclear, reason: "not statically known"},
		{name: "a subcommand from an expansion", cmd: "git $SUB", want: MutationUnclear, reason: "not statically known"},
		{name: "an inline script assembled at runtime", cmd: `bash -c "$SCRIPT"`, want: MutationUnclear, reason: "assembled at runtime"},
		{name: "an unparseable line", cmd: "git log 'unterminated", want: MutationUnclear, reason: "could not be parsed"},
		{name: "an empty line", cmd: "   ", want: MutationUnclear, reason: "no command"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := InspectCommand(tc.cmd)
			if got != tc.want {
				t.Fatalf("InspectCommand(%q) = %s (%q), want %s", tc.cmd, got, reason, tc.want)
			}
			if tc.reason != "" && !strings.Contains(reason, tc.reason) {
				t.Errorf("InspectCommand(%q) reason = %q, want it to mention %q", tc.cmd, reason, tc.reason)
			}
			// A refusal the user cannot act on is a dead end, and plan mode's
			// original message — identical for every command — is exactly what
			// that looks like.
			if got != MutationNone && strings.TrimSpace(reason) == "" {
				t.Errorf("InspectCommand(%q) = %s with no explanation", tc.cmd, got)
			}
		})
	}
}

// TestInspectCommandAgreesWithTheHostGateOnWrites is a cross-check between the
// two classifiers, in the one place they overlap.
//
// They answer different questions — ClassifyCommandIn guards the machine, this
// guards the working tree — and the whole reason this function exists is that
// the first cannot be used for the second. But a command the host gate has
// positively identified as writing a project path is not something this may
// call read-only. Disagreement there means one of the two catalogs has drifted.
func TestInspectCommandAgreesWithTheHostGateOnWrites(t *testing.T) {
	roots := Roots{Project: []string{"/repo"}}
	for _, cmd := range []string{
		"echo hi > /repo/f.txt",
		"rm -rf /repo/build",
		"cp a.txt /repo/b.txt",
		"mkdir /repo/new",
		"chmod +x /repo/script.sh",
	} {
		t.Run(cmd, func(t *testing.T) {
			as := ClassifyCommandIn(cmd, "/repo", roots)
			sawWrite := false
			for _, e := range as.Effects {
				if e.Kind.Mutating() {
					sawWrite = true
				}
			}
			m, reason := InspectCommand(cmd)
			if m == MutationNone {
				t.Fatalf("InspectCommand called %q read-only (host gate saw a write: %v)", cmd, sawWrite)
			}
			if m != MutationPresent {
				t.Errorf("InspectCommand(%q) = %s (%q), want %s", cmd, m, reason, MutationPresent)
			}
		})
	}
}
