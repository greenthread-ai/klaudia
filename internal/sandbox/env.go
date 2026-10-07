package sandbox

import (
	"os"
	"strconv"
	"strings"
)

// A command Klaudia runs should behave like the same command run from the
// user's own shell — with one honest exception: there is no terminal attached.
//
// Everything that makes the user's shell theirs is inherited for free, because
// cmd.Env stays nil unless we set it: PATH, SSH_AUTH_SOCK, git credential
// helpers, language version managers, proxy settings. What has to be added is
// the small set of hints that tell programs "nobody is watching, do not wait
// for a keypress". Without them, `git commit` with no -m opens an editor that
// nothing can type into and the turn hangs until the two-minute timeout.

// Terminal reports the terminal size to pass to children. Zero means unknown.
type Terminal struct{ Cols, Rows int }

// termSize is the size children are told about. Set by the frontend when it
// knows; zero otherwise, in which case COLUMNS/LINES are left alone.
var termSize Terminal

// SetTerminalSize records the terminal dimensions passed to child processes.
// Called by the TUI on resize, so a command's output wraps to the width the
// user is actually looking at rather than the 80 columns a pipe implies.
func SetTerminalSize(cols, rows int) { termSize = Terminal{Cols: cols, Rows: rows} }

// nonInteractiveEnv are the hints added to every child.
//
// These are forced rather than merely defaulted. A user who exported
// GIT_PAGER=less did so for their interactive shell, where a pager is useful;
// inheriting it here would hang a command that nobody can page. The user's own
// $PAGER still drives Klaudia's own long-output view — that path goes through
// tea.ExecProcess with a real terminal, and is where "$PAGER works" actually
// means something.
func nonInteractiveEnv() []string {
	env := []string{
		// Fail a credential prompt fast instead of blocking on a TTY that does
		// not exist. Credential *helpers* are untouched, and they are the path
		// that matters for `git push`.
		"GIT_TERMINAL_PROMPT=0",
		// git disables its pager when stdout is not a TTY, but only for its own
		// commands; being explicit also covers aliases that pipe through less.
		"GIT_PAGER=cat",
		"PAGER=cat",
		// Some tools consult this before deciding to prompt.
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
	}
	if termSize.Cols > 0 {
		env = append(env, "COLUMNS="+strconv.Itoa(termSize.Cols))
	}
	if termSize.Rows > 0 {
		env = append(env, "LINES="+strconv.Itoa(termSize.Rows))
	}
	return env
}

// childEnv builds the environment for a command: the user's, then our
// non-interactive hints, then anything the request set explicitly.
//
// Returns nil when there is nothing to add, so the common case keeps exec's
// inherit-everything behaviour rather than materialising a copy of os.Environ.
func childEnv(req Request) []string {
	extra := append(nonInteractiveEnv(), req.Env...)
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}

// ttyRequired reports whether a command needs a terminal Klaudia cannot give
// it, and what to do instead.
//
// The alternative would be to allocate a PTY. That would make `vim` and `top`
// "work" in a surface the model cannot drive and the user cannot see, which is
// worse than not running them: the turn would appear to succeed while sitting
// in an editor forever. Failing immediately with the flag that would have
// worked is something the model can act on.
func ttyRequired(command string) (reason string, blocked bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", false
	}
	prog := fields[0]
	if i := strings.LastIndexByte(prog, '/'); i >= 0 {
		prog = prog[i+1:]
	}
	rest := strings.Join(fields[1:], " ")

	if alt, ok := interactivePrograms[prog]; ok {
		// The program is judged by its name, but several of them have forms
		// that never touch the terminal — and some of those forms are the very
		// alternative the refusal suggests. Refusing `man ls | cat` with "use
		// `man <page> | cat`" leaves the model nowhere to go.
		if runsWithoutTerminal(prog, command) {
			return "", false
		}
		return prog + " needs a terminal, and this command has none. " + alt, true
	}
	// Subcommand-level cases: the program is fine, the flag is not.
	switch prog {
	case "git":
		switch {
		case strings.Contains(rest, "rebase -i"), strings.Contains(rest, "rebase --interactive"):
			return "interactive rebase needs an editor. Use `git rebase --onto` with explicit commits, " +
				"or rewrite the history non-interactively.", true
		case strings.Contains(rest, "add -i"), strings.Contains(rest, "add -p"), strings.Contains(rest, "add --patch"):
			return "interactive staging needs a terminal. Stage whole paths with `git add -- <paths>`.", true
		case strings.HasPrefix(rest, "commit") && !hasCommitMessage(rest):
			return "`git commit` with no -m opens an editor. Pass the message with -m.", true
		}
	case "ssh":
		// `ssh host` with no command opens an interactive login shell.
		if remoteHasNoCommand(fields[1:]) {
			return "`ssh <host>` with no command opens an interactive shell. " +
				"Pass the command to run, e.g. `ssh <host> 'systemctl status nginx'`.", true
		}
	case "docker", "podman":
		if strings.Contains(rest, " -it") || strings.Contains(rest, " -ti") {
			return "an interactive container session needs a terminal. Drop -t and keep -i, " +
				"or pass the command to run directly.", true
		}
	}
	return "", false
}

// interactivePrograms are full-screen or prompt-driven programs, with the
// non-interactive thing to reach for instead.
var interactivePrograms = map[string]string{
	"vim":      "Edit the file with the Edit tool instead.",
	"vi":       "Edit the file with the Edit tool instead.",
	"nvim":     "Edit the file with the Edit tool instead.",
	"emacs":    "Edit the file with the Edit tool instead.",
	"nano":     "Edit the file with the Edit tool instead.",
	"pico":     "Edit the file with the Edit tool instead.",
	"less":     "Read the file with the Read tool, or pipe through `cat`.",
	"more":     "Read the file with the Read tool, or pipe through `cat`.",
	"most":     "Read the file with the Read tool, or pipe through `cat`.",
	"top":      "Use `ps aux` for a one-shot snapshot, or `top -b -n 1` (`top -l 1` on macOS).",
	"htop":     "Use `ps aux` for a one-shot snapshot, or `top -b -n 1` (`top -l 1` on macOS).",
	"btop":     "Use `ps aux` for a one-shot snapshot, or `top -b -n 1` (`top -l 1` on macOS).",
	"man":      "Use `man <page> | cat`, or `<prog> --help`.",
	"tig":      "Use `git log --oneline` or `git show`.",
	"lazygit":  "Use plain git commands.",
	"gitui":    "Use plain git commands.",
	"ncdu":     "Use `du -sh *`.",
	"watch":    "Run the command once; use a background job if it needs to repeat.",
	"screen":   "Start it as a background job instead, or detached with `screen -dmS <name> <command>`.",
	"tmux":     "Start it as a background job instead, or detached with `tmux new -d -s <name> <command>`.",
	"mc":       "Use ls/find and the file tools.",
	"crontab":  "Use `crontab -l` to read; write with `crontab <file>`.",
	"visudo":   "Not something to run unattended. `visudo -c` checks the file without editing it.",
	"passwd":   "Not something to run unattended.",
	"sudoedit": "Edit the file with the Edit tool instead.",
}

// hasCommitMessage reports whether a `git commit` line supplies its message
// non-interactively.
func hasCommitMessage(rest string) bool {
	for _, flag := range []string{"-m", "--message", "-F", "--file", "-C", "--reuse-message",
		"--amend --no-edit", "--no-edit", "-c ", "--fixup", "--squash"} {
		if strings.Contains(rest, flag) {
			return true
		}
	}
	return false
}

// remoteHasNoCommand reports whether an ssh invocation names a destination but
// no command to run there.
func remoteHasNoCommand(args []string) bool {
	// Flags that consume the following word, so their value is not mistaken for
	// the destination or for a command.
	valued := map[string]bool{
		"-b": true, "-c": true, "-D": true, "-E": true, "-e": true, "-F": true,
		"-I": true, "-i": true, "-J": true, "-L": true, "-l": true, "-m": true,
		"-O": true, "-o": true, "-p": true, "-Q": true, "-R": true, "-S": true,
		"-W": true, "-w": true,
	}
	seenDest := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			if valued[a] {
				i++
			}
			continue
		}
		if !seenDest {
			seenDest = true
			continue
		}
		return false // something after the destination: that is the command
	}
	return seenDest
}

// runsWithoutTerminal reports whether this invocation of one of the
// interactivePrograms is a form that never needs a terminal: it prints and
// exits, runs in batch mode, or its output goes somewhere other than a screen.
// command is the whole line; only its first simple command is examined, which
// is the one prog came from.
func runsWithoutTerminal(prog, command string) bool {
	words, stdoutElsewhere := firstSimpleCommand(command)
	if len(words) == 0 {
		return false
	}
	args := words[1:]
	for _, a := range args {
		if a == "--help" || a == "--version" {
			return true // every one of them prints and exits
		}
	}
	switch prog {
	case "less", "more", "most", "man":
		// With stdout not a terminal the pagers copy their input like cat, and
		// man does not start a pager at all.
		return stdoutElsewhere
	case "top":
		// -b is procps batch mode; -l is logging mode on macOS. Plain `top` in
		// a pipe is not enough: it still fails with "failed tty get".
		letters := shortFlags(args, "dnoOpuUwEel")
		return letters['b'] || letters['l']
	case "crontab":
		return crontabNonInteractive(args)
	case "tmux":
		return tmuxNonInteractive(args)
	case "screen":
		return screenNonInteractive(args)
	case "visudo":
		return shortFlags(args, "fx")['c'] || containsWord(args, "--check")
	}
	return false
}

// crontabNonInteractive: -e opens an editor and -i asks for confirmation;
// listing, removing and installing from a file do neither. A bare `crontab`
// stays refused: it reads the new table from stdin, which here is empty, so it
// would replace the user's crontab with nothing.
func crontabNonInteractive(args []string) bool {
	if containsWord(args, "-") {
		return true // the table comes from a pipe or redirection
	}
	letters := shortFlags(args, "unT")
	if letters['e'] || letters['i'] {
		return false
	}
	if letters['l'] || letters['r'] || letters['V'] {
		return true
	}
	// Otherwise it installs a file, if one is named.
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-u" || a == "-n" || a == "-T":
			i++
		case !strings.HasPrefix(a, "-"):
			return true
		}
	}
	return false
}

// tmuxNonInteractive: only a bare `tmux`, an attach, or a new session that is
// not detached (-d) wants a terminal. Listing sessions, send-keys,
// capture-pane and the rest run and exit. tmux accepts any unambiguous prefix
// of a command name, so `tmux a` and `tmux attach` are both attach-session.
func tmuxNonInteractive(args []string) bool {
	i := 0
	for i < len(args) && len(args[i]) > 1 && args[i][0] == '-' {
		a := args[i]
		i++
		if a == "--" {
			break
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if c == 'V' || c == 'c' {
				return true // prints the version / runs the shell command and exits
			}
			if strings.IndexByte("fLST", c) >= 0 {
				if j == len(a)-1 {
					i++ // the value is the next word
				}
				break
			}
		}
	}
	if i >= len(args) {
		return false // a bare tmux starts an attached session
	}
	sub, rest := args[i], args[i+1:]
	switch {
	case sub == "attach" || strings.HasPrefix("attach-session", sub):
		return false
	case sub == "new" || (strings.HasPrefix("new-session", sub) && len(sub) > len("new-")):
		return shortFlags(rest, "cefFnstxy")['d']
	}
	return true
}

// screenNonInteractive: screen attaches unless it is told to start detached
// (-d -m, -dmS), to detach another session (-d), to send a session a command
// (-X, -Q), or to report (-ls, -wipe, -v).
func screenNonInteractive(args []string) bool {
	letters := map[byte]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			break // the program to run in the new window, and its arguments
		}
		switch a {
		case "-ls", "-list", "-wipe", "-v", "-version":
			return true
		}
		for j := 1; j < len(a); j++ {
			letters[a[j]] = true
			if strings.IndexByte("SceEhpsTt", a[j]) >= 0 {
				if j == len(a)-1 {
					i++ // the value is the next word
				}
				break
			}
		}
	}
	if letters['X'] || letters['Q'] {
		return true
	}
	return (letters['d'] || letters['D']) && !letters['r'] && !letters['R'] && !letters['x']
}

// shortFlags collects the letters of every short-option cluster in args, so
// -bn1 and -b -n 1 both report 'b'. A letter in valued takes a value — the
// rest of its cluster, or the next word — which is skipped rather than read as
// more flags. Long options and operands are ignored.
func shortFlags(args []string, valued string) map[byte]bool {
	letters := map[byte]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for j := 1; j < len(a); j++ {
			letters[a[j]] = true
			if strings.IndexByte(valued, a[j]) >= 0 {
				if j == len(a)-1 {
					i++ // the value is the next word
				}
				break
			}
		}
	}
	return letters
}

func containsWord(args []string, w string) bool {
	for _, a := range args {
		if a == w {
			return true
		}
	}
	return false
}

// firstSimpleCommand splits the first command of a shell line into words, and
// reports whether its standard output leaves through a pipe or a redirection
// rather than going where a terminal would be.
//
// It honours single quotes, double quotes and backslashes, so a `|` inside
// quotes is not a pipe, and it stops at the first unquoted `|`, `||`, `&&`,
// `&`, `;`, newline or comment. Redirection operators and their targets are
// left out of the words. `>`, `1>`, `>>` and `&>` move stdout; `2>` and `2>&1`
// do not. It is not a shell parser — it is enough to tell `man ls | cat` from
// `man ls` and `man ls || true`.
func firstSimpleCommand(s string) (words []string, stdoutElsewhere bool) {
	var (
		cur      strings.Builder
		inWord   bool
		quote    byte
		dropNext bool // the next word is a redirection target
	)
	flush := func() {
		if !inWord {
			return
		}
		if dropNext {
			dropNext = false
		} else {
			words = append(words, cur.String())
		}
		cur.Reset()
		inWord = false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote == '\'':
			if ch == '\'' {
				quote = 0
			} else {
				cur.WriteByte(ch)
			}
		case quote == '"':
			switch {
			case ch == '"':
				quote = 0
			case ch == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0:
				i++
				cur.WriteByte(s[i])
			default:
				cur.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			quote, inWord = ch, true
		case ch == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case ch == '#' && !inWord:
			return words, stdoutElsewhere
		case ch == '>' || ch == '<':
			fd := ""
			if inWord && !dropNext && isDigits(cur.String()) {
				fd = cur.String() // 2>file: the digits name the descriptor
				cur.Reset()
				inWord = false
			} else {
				flush()
			}
			if ch == '>' && (fd == "" || fd == "1") {
				stdoutElsewhere = true
			}
			for i+1 < len(s) && strings.IndexByte("<>&|-", s[i+1]) >= 0 {
				i++ // >>, >&, >|, <<, <<-, <&
			}
			dropNext = true
		case ch == '&' && i+1 < len(s) && s[i+1] == '>':
			flush()
			stdoutElsewhere = true
			for i+1 < len(s) && s[i+1] == '>' {
				i++
			}
			dropNext = true
		case ch == '|':
			flush()
			if i+1 < len(s) && s[i+1] == '|' {
				return words, stdoutElsewhere // ||: the next command runs only on failure
			}
			return words, true
		case ch == ';' || ch == '&' || ch == '\n':
			flush()
			return words, stdoutElsewhere
		case ch == ' ' || ch == '\t':
			flush()
		default:
			cur.WriteByte(ch)
			inWord = true
		}
	}
	flush()
	return words, stdoutElsewhere
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
