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
	"top":      "Use `ps aux` for a one-shot snapshot.",
	"htop":     "Use `ps aux` for a one-shot snapshot.",
	"btop":     "Use `ps aux` for a one-shot snapshot.",
	"man":      "Use `man <page> | cat`, or `<prog> --help`.",
	"tig":      "Use `git log --oneline` or `git show`.",
	"lazygit":  "Use plain git commands.",
	"gitui":    "Use plain git commands.",
	"ncdu":     "Use `du -sh *`.",
	"watch":    "Run the command once; use a background job if it needs to repeat.",
	"screen":   "Start it as a background job instead.",
	"tmux":     "Start it as a background job instead.",
	"mc":       "Use ls/find and the file tools.",
	"crontab":  "Use `crontab -l` to read; write with `crontab <file>`.",
	"visudo":   "Not something to run unattended.",
	"passwd":   "Not something to run unattended.",
	"sudoedit": "Edit the file with the Edit tool instead.",
}

// hasCommitMessage reports whether a `git commit` line supplies its message
// non-interactively. rest is the command after `git`, starting at "commit".
//
// It parses the arguments the way git's option parser does rather than looking
// for substrings: a substring search misses short-flag clusters (`-am`,
// `-asm`), which sent the model into a loop of being told to "pass -m" when it
// had, and it matched "-m" inside a path or another option's value, letting a
// commit through to an editor that hangs the turn.
func hasCommitMessage(rest string) bool {
	args := shellWords(rest)
	if len(args) > 0 {
		args = args[1:] // "commit"
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return false // the rest are pathspecs
		case strings.HasPrefix(a, "--"):
			name, _, attached := strings.Cut(a[2:], "=")
			if commitMessageLong[name] {
				return true
			}
			if commitValuedLong[name] && !attached {
				i++ // the value is the next word, whatever it looks like
			}
		case len(a) > 1 && a[0] == '-':
			supplies, takesNext := commitShortCluster(a[1:])
			if supplies {
				return true
			}
			if takesNext {
				i++
			}
		}
	}
	return false
}

// commitMessageLong are the long options of `git commit` that supply the
// message, or say not to edit one.
var commitMessageLong = map[string]bool{
	"message": true, "file": true, "reuse-message": true,
	"no-edit": true, "fixup": true, "squash": true,
}

// commitValuedLong are the other long options whose value may be the next
// word, which must then be skipped rather than read as an option.
// --reedit-message is here, not above: like -C it reuses a commit's message,
// but it then opens the editor on it.
var commitValuedLong = map[string]bool{
	"author": true, "date": true, "template": true, "cleanup": true,
	"trailer": true, "pathspec-from-file": true, "reedit-message": true,
}

// commitShortCluster reads one cluster of short options (without its leading
// dash) as git does: letters are boolean flags until one that takes a value,
// and that one consumes the rest of the cluster, or the next word when the
// cluster ends there. So -am, -asm and -mfix all carry -m.
func commitShortCluster(c string) (supplies, takesNext bool) {
	for j := 0; j < len(c); j++ {
		switch c[j] {
		case 'm', 'F', 'C':
			return true, false
		case 'c', 't': // --reedit-message (which opens the editor), --template
			return false, j == len(c)-1
		case 'S', 'u': // optional value, attached only
			return false, false
		}
	}
	return false, false
}

// shellWords splits the first simple command of a shell line into words,
// honouring single quotes, double quotes and backslashes, and stopping at an
// unquoted `;`, `&`, `|` or newline. It is not a shell parser: it is enough to
// keep a quoted value in one word and a later command's arguments out.
func shellWords(s string) []string {
	var (
		words  []string
		cur    strings.Builder
		inWord bool
		quote  byte
	)
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
		case ch == ';' || ch == '&' || ch == '|' || ch == '\n':
			if inWord {
				words = append(words, cur.String())
			}
			return words
		case ch == ' ' || ch == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(ch)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
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
