package sandbox

import (
	"strings"
	"testing"
)

// Issue #112: a refusal's advice has to be something the model can then run.
// Only the first word used to be checked, so `man ls | cat` was refused with
// "use `man <page> | cat`" and `crontab -l` with "use `crontab -l`" — the model
// was told to do exactly what it had just been refused for. Each row is a
// refused command, the words of its advice that name the alternative, and
// concrete instances of that alternative, every one of which must run.
func TestTTYRefusalAlternativesAreAccepted(t *testing.T) {
	for _, tc := range []struct {
		refused string
		advice  string
		alts    []string
	}{
		{"man ls", "man <page> | cat", []string{"man ls | cat", "man 5 crontab | cat", "ls --help"}},
		{"less /var/log/syslog", "pipe through `cat`", []string{"less /var/log/syslog | cat", "cat /var/log/syslog"}},
		{"more notes.txt", "pipe through `cat`", []string{"more notes.txt | cat"}},
		{"most notes.txt", "pipe through `cat`", []string{"most notes.txt | cat"}},
		{"top", "top -b -n 1", []string{"top -b -n 1", "ps aux"}},
		{"top", "top -l 1", []string{"top -l 1"}},
		{"htop", "top -b -n 1", []string{"top -b -n 1", "ps aux"}},
		{"btop", "ps aux", []string{"ps aux"}},
		{"crontab -e", "crontab -l", []string{"crontab -l"}},
		{"crontab -e", "crontab <file>", []string{"crontab /tmp/cron.txt"}},
		{"crontab", "crontab -l", []string{"crontab -l", "crontab /tmp/cron.txt"}},
		{"tmux", "tmux new -d -s <name> <command>", []string{"tmux new -d -s dev 'npm run dev'"}},
		{"screen", "screen -dmS <name> <command>", []string{"screen -dmS dev npm run dev"}},
		{"visudo", "visudo -c", []string{"visudo -c"}},
		{"tig", "git log --oneline", []string{"git log --oneline", "git show"}},
		{"lazygit", "plain git commands", []string{"git status"}},
		{"ncdu", "du -sh *", []string{"du -sh *"}},
		{"git rebase -i HEAD~3", "git rebase --onto", []string{"git rebase --onto main HEAD~3"}},
		{"git add -p", "git add -- <paths>", []string{"git add -- src/ docs/"}},
		{"git commit", "with -m", []string{"git commit -m 'fix the build'"}},
		{"ssh staging", "ssh <host> 'systemctl status nginx'", []string{"ssh staging 'systemctl status nginx'"}},
		{"docker run -it alpine sh", "Drop -t and keep -i", []string{"docker run -i alpine sh"}},
		{"docker run -it alpine sh", "pass the command to run directly", []string{"docker run --rm alpine ls /"}},
	} {
		reason, blocked := TTYRequired(tc.refused)
		if !blocked {
			t.Errorf("%q was allowed; it needs a terminal", tc.refused)
			continue
		}
		if !strings.Contains(reason, tc.advice) {
			t.Errorf("%q: reason %q does not suggest %q", tc.refused, reason, tc.advice)
		}
		for _, alt := range tc.alts {
			if r, b := TTYRequired(alt); b {
				t.Errorf("%q is refused, but it is what the refusal of %q suggests: %s", alt, tc.refused, r)
			}
		}
	}
}

// The forms that never touch a terminal, beyond the ones the advice names.
func TestTTYRequiredAllowsNonInteractiveForms(t *testing.T) {
	for _, cmd := range []string{
		// Output not reaching a terminal: the pagers act as cat, man skips its pager.
		"less f | head",
		"less f |& grep x",
		"man git-commit | col -b",
		"man ls > /tmp/ls.txt",
		"man ls 1>/tmp/ls.txt",
		"man ls >>/tmp/ls.txt 2>&1",
		"man ls &> /tmp/ls.txt",
		"man 'ls' 2>/dev/null | head -5",
		// Prints and exits.
		"vim --version",
		"htop --version",
		"less --help",
		// Batch and query forms.
		"top -bn1",
		"top -n 1 -b",
		"top -b -n1 -o %MEM",
		"crontab -l -u deploy",
		"crontab -u deploy -l",
		"crontab -r",
		"crontab -",
		"crontab -u deploy /tmp/cron.txt",
		"tmux ls",
		"tmux list-sessions",
		"tmux -L work ls",
		"tmux send-keys -t dev 'make' Enter",
		"tmux capture-pane -p -t dev",
		"tmux kill-session -t dev",
		"tmux new-session -d -s dev",
		"tmux new -ds dev",
		"tmux -c 'echo hi'",
		"screen -ls",
		"screen -list",
		"screen -d -m -S dev make",
		"screen -S dev -X quit",
		"screen -d dev",
		"visudo -c -f /etc/sudoers.d/deploy",
		"visudo --check",
		// Not refused before either, and must stay that way.
		"git --no-pager log --oneline",
		"git log -p",
	} {
		if reason, blocked := TTYRequired(cmd); blocked {
			t.Errorf("%q was refused as needing a terminal: %s", cmd, reason)
		}
	}
}

// The exemptions must not open a door for the forms that do hang.
func TestTTYRequiredStillRefusesInteractiveForms(t *testing.T) {
	for _, cmd := range []string{
		"man ls",
		"man ls 2>/dev/null",
		"man ls 2>&1",
		"man ls || true",
		"man ls && echo done",
		"man ls; echo done",
		"man ls & wait",
		"less 'a|b'",
		`less a\|b`,
		"less f # | cat",
		"top",
		"top | head", // measured: procps top fails with "failed tty get" unless -b
		"top -d 1",
		"top -u bob", // the b is the user's name, not batch mode
		"top -p 1234",
		"htop | cat",
		"crontab",
		"crontab -e",
		"crontab -u deploy -e",
		"crontab -ri",
		"crontab -u deploy",
		"tmux",
		"tmux -L work",
		"tmux attach",
		"tmux a -t dev",
		"tmux attach-session -t dev",
		"tmux new",
		"tmux new -s dev",
		"tmux new-session -s dev -n editor",
		"tmux new -s dev -c /tmp",
		"screen",
		"screen -S dev",
		"screen -r dev",
		"screen -d -r dev",
		"screen -D -R",
		"screen vim notes.txt",
		"visudo",
		"visudo -f /etc/sudoers.d/deploy",
		"vim f | cat", // vim still waits for keystrokes whatever stdout is
		"nano f > /dev/null",
	} {
		if _, blocked := TTYRequired(cmd); !blocked {
			t.Errorf("%q was allowed; it needs a terminal", cmd)
		}
	}
}

func TestFirstSimpleCommand(t *testing.T) {
	for _, tc := range []struct {
		line  string
		words string
		out   bool
	}{
		{"man ls", "man ls", false},
		{"man ls | cat", "man ls", true},
		{"man ls|cat", "man ls", true},
		{"man ls || cat", "man ls", false},
		{"man ls > out", "man ls", true},
		{"man ls >out 2>&1", "man ls", true},
		{"man ls 2>/dev/null", "man ls", false},
		{"man ls 2>&1", "man ls", false},
		{"man ls >&2", "man ls", true},
		{"man ls &>out", "man ls", true},
		{"man ls < in", "man ls", false},
		{`less "a | b"`, "less a | b", false},
		{`less 'x;y' ; ls`, "less x;y", false},
		{`less a\ b | cat`, "less a b", true},
		{"less f # | cat", "less f", false},
		{"less f\nls | cat", "less f", false},
	} {
		words, out := firstSimpleCommand(tc.line)
		if got := strings.Join(words, " "); got != tc.words || out != tc.out {
			t.Errorf("firstSimpleCommand(%q) = %q, %v; want %q, %v", tc.line, got, out, tc.words, tc.out)
		}
	}
}
