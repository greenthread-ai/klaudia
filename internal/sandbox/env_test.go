package sandbox

import (
	"context"
	"strings"
	"testing"
)

// §13's promise: a command that works in the user's shell works here. The parts
// that make a shell theirs — PATH, the ssh agent, credential helpers — have to
// arrive untouched.
func TestUserEnvironmentReachesTheChild(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/klaudia-agent-probe")
	t.Setenv("KLAUDIA_ENV_PROBE", "carried")

	resp, err := NewLocal().Run(context.Background(), Request{
		Command: "printf '%s|%s|%s' \"$PATH\" \"$SSH_AUTH_SOCK\" \"$KLAUDIA_ENV_PROBE\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(resp.Stdout, "|")
	if len(parts) != 3 {
		t.Fatalf("unexpected output %q", resp.Stdout)
	}
	if parts[0] == "" {
		t.Error("PATH did not reach the child")
	}
	if parts[1] != "/tmp/klaudia-agent-probe" {
		t.Errorf("SSH_AUTH_SOCK = %q", parts[1])
	}
	if parts[2] != "carried" {
		t.Errorf("an exported variable did not reach the child: %q", parts[2])
	}
}

// The hints that stop a child waiting for a keypress nobody can give.
func TestNonInteractiveHintsAreSet(t *testing.T) {
	resp, err := NewLocal().Run(context.Background(), Request{
		Command: "printf '%s|%s|%s' \"$GIT_TERMINAL_PROMPT\" \"$GIT_PAGER\" \"$PAGER\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stdout != "0|cat|cat" {
		t.Errorf("non-interactive hints = %q, want \"0|cat|cat\"", resp.Stdout)
	}
}

// The user's interactive PAGER is theirs, and setting it must not leak into a
// child that has no terminal to page with.
func TestUserPagerDoesNotReachTheChild(t *testing.T) {
	t.Setenv("PAGER", "less -R")
	resp, err := NewLocal().Run(context.Background(), Request{Command: "printf '%s' \"$PAGER\""})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stdout != "cat" {
		t.Errorf("PAGER = %q in the child; the user's pager would hang a captured command", resp.Stdout)
	}
}

func TestTerminalSizeReachesTheChild(t *testing.T) {
	SetTerminalSize(123, 45)
	t.Cleanup(func() { SetTerminalSize(0, 0) })
	resp, err := NewLocal().Run(context.Background(), Request{Command: "printf '%s|%s' \"$COLUMNS\" \"$LINES\""})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stdout != "123|45" {
		t.Errorf("COLUMNS|LINES = %q, want \"123|45\"", resp.Stdout)
	}
}

// Request.Env still wins: it is the caller being explicit.
func TestRequestEnvOverridesTheHints(t *testing.T) {
	resp, err := NewLocal().Run(context.Background(), Request{
		Command: "printf '%s' \"$GIT_PAGER\"",
		Env:     []string{"GIT_PAGER=delta"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stdout != "delta" {
		t.Errorf("GIT_PAGER = %q, want the request's value", resp.Stdout)
	}
}

// Hanging until the two-minute timeout tells the model nothing. Refusing
// immediately with the flag that would have worked tells it what to do.
func TestTTYRequiredDetection(t *testing.T) {
	blocked := map[string]string{
		"vim src/main.go":          "Edit tool",
		"git rebase -i HEAD~3":     "rebase --onto",
		"git add -p":               "git add --",
		"git commit":               "-m",
		"less /var/log/system.log": "Read tool",
		"top":                      "ps aux",
		"ssh staging":              "command to run",
		"docker run -it alpine":    "interactive container",
		"/usr/local/bin/htop":      "ps aux",
		"man git":                  "--help",
	}
	for cmd, want := range blocked {
		reason, isBlocked := TTYRequired(cmd)
		if !isBlocked {
			t.Errorf("%q was allowed; it needs a terminal", cmd)
			continue
		}
		if !strings.Contains(reason, want) {
			t.Errorf("%q: reason %q does not suggest %q", cmd, reason, want)
		}
	}

	// The far more important half: ordinary commands must not be refused.
	for _, cmd := range []string{
		"git commit -m 'fix'",
		"git commit --amend --no-edit",
		"git commit -F /tmp/msg",
		"git log --oneline",
		"git add -A",
		"git add -- src/",
		"go test ./...",
		"npm run dev",
		"cat /etc/hosts",
		"ssh staging systemctl status nginx",
		"ssh -i ~/.ssh/key -p 2222 deploy@prod uptime",
		"docker run --rm alpine true",
		"docker compose up -d",
		"grep -r topic ./src",     // starts with "top" but is not top
		"./scripts/less-noisy.sh", // contains "less" but is not less
		"vimdiff-report --help",   // starts with "vim" but is not vim
	} {
		if reason, blocked := TTYRequired(cmd); blocked {
			t.Errorf("%q was refused as needing a terminal: %s", cmd, reason)
		}
	}
}

// Issue #105: the message check used to look for the substring "-m", so a
// combined short-flag cluster like -am never matched and the model was told to
// "pass the message with -m" when it already had — a retry loop. The check now
// parses git's option syntax (clusters, attached values, long forms, the end
// of options), so it also stops mistaking a path, another option's value, or a
// later command's argument for a message flag.
func TestTTYRequiredGitCommitMessageFlags(t *testing.T) {
	for _, cmd := range []string{
		`git commit -am "fix the build"`,
		`git commit -sm "fix"`,
		`git commit -asm "fix"`,
		`git commit -a -s -m fix`,
		`git commit -mfix`,
		`git commit -am"fix"`,
		`git commit -vm 'fix'`,
		`git commit --message="fix"`,
		`git commit --message fix`,
		`git commit -aF /tmp/msg`,
		`git commit -F/tmp/msg`,
		`git commit --file=/tmp/msg`,
		`git commit -aC HEAD`,
		`git commit -CHEAD`,
		`git commit --reuse-message=HEAD`,
		`git commit --amend --no-edit`,
		`git commit --fixup=HEAD~1`,
		`git commit -m "$(printf 'fix\n\nbody')"`,
		`git commit -am "fix" && git push`,
		`git commit -c HEAD --no-edit`,
	} {
		if reason, blocked := TTYRequired(cmd); blocked {
			t.Errorf("%q supplies a message but was refused: %s", cmd, reason)
		}
	}

	// None of these supplies a message, so each would open an editor. Most
	// used to pass because "-m" or "-F" appeared somewhere in the text.
	for _, cmd := range []string{
		`git commit -a`,
		`git commit -as`,
		`git commit src/my-module`,          // a path containing "-m"
		`git commit -- -m`,                  // a pathspec after --
		`git commit --author "Jane -m Doe"`, // another option's value
		`git commit --trailer "Note: -F x"`,
		`git commit --template=/tmp/x-m`,
		`git commit -t /tmp/tmpl -a`,
		`git commit -uno`,             // -u's attached value, not an m flag
		`git commit -a && echo " -m"`, // a later command's argument
		`git commit -a; git log -m`,
		`git commit -c HEAD`, // --reedit-message opens the editor on it
		`git commit --reedit-message=HEAD`,
	} {
		if _, blocked := TTYRequired(cmd); !blocked {
			t.Errorf("%q supplies no message but was allowed; it would open an editor", cmd)
		}
	}
}
