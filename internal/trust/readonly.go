package trust

import (
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/bashparser"
)

// Mutation is what InspectCommand concluded on the one axis plan mode cares
// about: does running this line change the user's files?
//
// This is deliberately not the question ClassifyCommandIn answers. That one
// decides whether a command crosses the boundary of the machine Klaudia runs
// on, and ordinary project work is *silent* to it by design — measured, it
// reports no effects at all for `rm -rf build`, `git commit -m x` and
// `go build ./...`. An empty Assessment means "nothing worth asking the user
// about", never "this only reads", so plan mode cannot be built on it.
type Mutation int

const (
	// MutationUnclear: the line could not be read well enough to decide, or
	// nothing here classifies the program.
	//
	// The zero value, and the whole reason the verdict is a tri-state. This is
	// an allowlist, and CLAUDE.md already records what allowlists do: they go
	// stale the day something ships. An allowlist that fails to a *prompt*
	// degrades into a question the user answers in one keypress; one that
	// fails to a *denial* degrades into the bug plan mode opened with, where
	// research was impossible because every shell command was refused.
	MutationUnclear Mutation = iota
	// MutationNone: every component of the line only inspects.
	MutationNone
	// MutationPresent: at least one component is known to change something.
	MutationPresent
)

func (m Mutation) String() string {
	switch m {
	case MutationNone:
		return "read-only"
	case MutationPresent:
		return "mutates"
	}
	return "unclear"
}

// rank orders the verdicts by how much they restrict, so a line can be judged
// by its worst component. Numeric order of the constants is deliberately not
// used: MutationUnclear has to be the zero value (a caller that forgets a case
// must land on "ask"), but it ranks *above* MutationNone, and encoding that in
// the iota order would trade one trap for the other.
func (m Mutation) rank() int {
	switch m {
	case MutationPresent:
		return 2
	case MutationUnclear:
		return 1
	}
	return 0
}

// InspectCommand reports whether cmd only inspects the project, is known to
// change it, or cannot be determined — along with a reason naming what it
// would change, or what could not be read.
//
// Every component of a pipeline or `&&` chain has to clear the bar
// independently, and a writing redirection disqualifies the line whatever the
// program: `echo` inspects, `echo x > main.go` does not.
func InspectCommand(cmd string) (Mutation, string) {
	m, reason, _ := InspectCommandPart(cmd)
	return m, reason
}

// InspectCommandPart is InspectCommand plus the component the verdict is
// about: the simple command that set it, or "" when it is the line as a whole
// (a redirect, or a line that would not parse).
//
// A permission prompt needs it for its headline. It used to show the line's
// first command, so plan mode asking about `git tag` in
// `cd <repo> && git log … ; git tag --sort=…` read "run command cd <repo>"
// — naming the one part that was never in question.
func InspectCommandPart(cmd string) (Mutation, string, string) {
	a, err := bashparser.Parse(cmd)
	if err != nil {
		return MutationUnclear, "the command line could not be parsed", ""
	}
	if len(a.Commands) == 0 && len(a.Redirects) == 0 {
		return MutationUnclear, "no command was found on the line", ""
	}
	return inspectAnalysis(a, 0)
}

// commandText is a simple command as typed, for naming it.
func commandText(c bashparser.Command) string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// inspectAnalysis judges a parsed line by its worst component, and names it.
func inspectAnalysis(a bashparser.Analysis, depth int) (Mutation, string, string) {
	// Redirections first. They hang off the statement rather than the call, so
	// a line whose every program is read-only can still be a write — this is
	// the one project-write case ClassifyCommandIn already detects, and the
	// reason it is not enough on its own is everything below.
	for _, r := range a.Redirects {
		if r.Literal && isPseudoDevice(r.Target) {
			// `2>/dev/null` discards output; it writes nothing. Refusing it
			// blocked an ordinary research idiom in a real planning session.
			continue
		}
		if !r.Literal || strings.TrimSpace(r.Target) == "" {
			return MutationPresent, "it redirects output to a file", ""
		}
		return MutationPresent, fmt.Sprintf("it redirects output to %s", r.Target), ""
	}

	verdict, reason, part := MutationNone, "", ""
	for _, c := range a.Commands {
		m, why := inspectOne(c, depth)
		if m.rank() > verdict.rank() {
			verdict, reason, part = m, why, commandText(c)
			if verdict == MutationPresent {
				// Nothing a later component says can soften this, and the
				// first offender is the one worth naming.
				break
			}
		}
	}
	return verdict, reason, part
}

// inspectOne judges a single simple command.
func inspectOne(c bashparser.Command, depth int) (Mutation, string) {
	name, args, _, ok := unwrap(c)
	if !ok {
		return MutationUnclear, "the program name is not statically known"
	}
	prog := base(name)

	// An inline script is the whole command; nothing else in argv matters.
	// Without this, `bash -c 'rm -rf build'` is a call to `bash`, which no
	// catalog classifies, so it would ask instead of being refused.
	if payload, isShell := shellPayload(prog, args); isShell {
		if depth >= maxDepth {
			return MutationUnclear, "the command nests inline shells too deeply to read"
		}
		if !payload.Literal {
			return MutationUnclear, fmt.Sprintf("the script passed to %s is assembled at runtime", prog)
		}
		inner, err := bashparser.Parse(payload.Text)
		if err != nil {
			return MutationUnclear, fmt.Sprintf("the script passed to %s could not be parsed", prog)
		}
		m, why, _ := inspectAnalysis(inner, depth+1)
		return m, why
	}

	if m, why, known := inspectSubcommand(prog, args); known {
		return m, why
	}
	if writePrograms[prog] {
		return MutationPresent, fmt.Sprintf("%s writes to the files it is given", prog)
	}
	if flags, guarded := writesWithFlag[prog]; guarded {
		if f, found := firstFlag(args, flags); found {
			return MutationPresent, fmt.Sprintf("%s was given %s, which makes it write", prog, f)
		}
		return MutationNone, ""
	}
	if readOnlyPrograms[prog] || directoryBuiltins[prog] {
		return MutationNone, ""
	}
	return MutationUnclear, fmt.Sprintf("nothing here classifies %s, so whether it writes is unknown", prog)
}

// writesWithFlag lists programs that inspect until a particular flag turns
// them into writers.
//
// They need a third category because the bare form is exactly what research
// runs — `sed -n '1,40p' f` is a Read with a line range and `find . -name x`
// is a Glob — while `sed -i` and `find -delete` are among the most destructive
// things on the list. They are also the gap that makes readOnlyPrograms
// unusable as-is for this question: it holds `sort` and `yq`, both of which
// write a file when asked to, because for the host boundary what matters is
// *where* they write and not whether they do.
var writesWithFlag = map[string][]string{
	"sed":  {"-i", "--in-place"},
	"find": {"-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprintf", "-fls"},
	"sort": {"-o", "--output"},
	"yq":   {"-i", "--inplace"},
}

// firstFlag reports the first of flags present in args, matching by prefix so
// that bundled and attached forms are caught: sed's in-place flag arrives as
// `-i`, `-i.bak` and `-ni` alike, and only the first of those is the flag
// itself.
func firstFlag(args []bashparser.Word, flags []string) (string, bool) {
	for _, w := range args {
		for _, f := range flags {
			if strings.HasPrefix(w.Text, f) {
				return f, true
			}
			// A bundled short flag: `sed -ni` is `-n -i`. Only single-dash
			// short flags bundle, so long options are excluded.
			if len(f) == 2 && f[0] == '-' && len(w.Text) > 1 && w.Text[0] == '-' &&
				!strings.HasPrefix(w.Text, "--") && strings.ContainsRune(w.Text, rune(f[1])) {
				return f, true
			}
		}
	}
	return "", false
}

// inspectSubcommand handles the multiplexers, where the program name says
// nothing and the subcommand says everything. known is false when prog is not
// a multiplexer this understands, so the caller falls through to the
// program-level catalogs.
func inspectSubcommand(prog string, args []bashparser.Word) (m Mutation, reason string, known bool) {
	table, isMultiplexer := subcommands[prog]
	if !isMultiplexer {
		return 0, "", false
	}
	sub, ok := subcommandOf(args)
	if !ok {
		return MutationUnclear, fmt.Sprintf("the %s subcommand is not statically known", prog), true
	}
	if sub == "" {
		// Bare `git` or `go` prints usage. Harmless, but there is no reason
		// for the model to run it, and calling it read-only would also accept
		// `git` with only flags it does not understand.
		return MutationUnclear, fmt.Sprintf("%s was called with no subcommand", prog), true
	}
	// Before mutates: `git stash` stashes, but `git stash list` reads, and
	// `git tag` / `git branch` create something only when given a name.
	if lists, has := table.listOnly[sub]; has {
		if rest, literal := literalsAfter(args, sub); literal && lists(rest) {
			return MutationNone, "", true
		}
	}
	if why, found := table.mutates[sub]; found {
		return MutationPresent, why, true
	}
	if table.reads[sub] {
		// `go mod` is two levels: `go mod why` reads, `go mod tidy` rewrites
		// go.mod and go.sum. The outer table cannot answer for it.
		if nested, has := table.nested[sub]; has {
			return inspectSubcommand(nested, args[indexOf(args, sub)+1:])
		}
		return MutationNone, "", true
	}
	return MutationUnclear, fmt.Sprintf("nothing here classifies `%s %s`", prog, sub), true
}

type subcommandTable struct {
	reads map[string]bool
	// mutates maps a subcommand to what it changes, so a refusal can say.
	mutates map[string]string
	// nested names a second table to consult for subcommands that are
	// themselves multiplexers.
	nested map[string]string
	// listOnly holds subcommands that only list in some argument shapes,
	// given the words after the subcommand. A shape the rule does not accept
	// falls through to mutates or, failing that, asks.
	listOnly map[string]func(rest []string) bool
}

// subcommands covers git and go, which is where the measured plan-mode session
// was actually blocked. Everything else — npm, cargo, docker, kubectl — is
// absent on purpose: an unclassified multiplexer asks, which is the correct
// answer for a tool nobody has sat down and read the grammar of, and is how
// this list is meant to grow.
var subcommands = map[string]subcommandTable{
	"git": {
		// Measured: `git tag --sort=-creatordate --format=…` asked during a
		// planning session, and `git stash list` was refused outright,
		// because the bare subcommands write.
		listOnly: map[string]func([]string) bool{
			"tag":    gitTagLists,
			"branch": gitBranchLists,
			"remote": gitRemoteLists,
			"stash":  func(r []string) bool { return len(r) > 0 && (r[0] == "list" || r[0] == "show") },
			"config": gitConfigReads,
		},
		reads: set(
			"status", "log", "diff", "show", "blame", "grep", "shortlog",
			"describe", "rev-parse", "rev-list", "ls-files", "ls-tree",
			"ls-remote", "cat-file", "name-rev", "merge-base", "for-each-ref",
			"diff-tree", "diff-index", "symbolic-ref", "check-ignore",
			"count-objects", "whatchanged", "version", "help", "var",
		),
		mutates: map[string]string{
			"add": "`git add` stages changes", "commit": "`git commit` writes a commit",
			"rm": "`git rm` deletes tracked files", "mv": "`git mv` moves tracked files",
			"checkout": "`git checkout` rewrites the working tree",
			"switch":   "`git switch` rewrites the working tree",
			"restore":  "`git restore` overwrites files in the working tree",
			"reset":    "`git reset` moves the branch and can discard changes",
			"revert":   "`git revert` writes a commit", "cherry-pick": "`git cherry-pick` writes a commit",
			"merge": "`git merge` rewrites the working tree", "rebase": "`git rebase` rewrites history",
			"pull": "`git pull` rewrites the working tree", "push": "`git push` publishes to a remote",
			"fetch": "`git fetch` writes to the object store", "clone": "`git clone` creates a checkout",
			"init": "`git init` creates a repository", "apply": "`git apply` patches the working tree",
			"am": "`git am` writes commits", "clean": "`git clean` deletes untracked files",
			"gc": "`git gc` rewrites the object store", "prune": "`git prune` deletes objects",
			"stash":      "`git stash` without a subcommand stashes the working tree",
			"update-ref": "`git update-ref` moves a ref", "update-index": "`git update-index` writes the index",
			"filter-branch": "`git filter-branch` rewrites history",
			"submodule":     "`git submodule` writes checkouts", "worktree": "`git worktree` creates checkouts",
			"bisect":       "`git bisect` rewrites the working tree",
			"format-patch": "`git format-patch` writes patch files", "archive": "`git archive` writes an archive",
		},
	},
	"go": {
		reads:  set("list", "doc", "env", "version", "vet", "mod"),
		nested: map[string]string{"mod": "go mod"},
		mutates: map[string]string{
			// Denied rather than asked about on the user's call: plan mode is
			// for anything that does not modify files, and compiling or
			// testing can both land something in the tree. `go build ./...`
			// writes only to the build cache, but `go build ./cmd/klaudia`
			// drops a binary in the working directory, and telling the two
			// apart needs the package graph.
			"build":    "`go build` can write a compiled binary into the working directory",
			"test":     "`go test` runs the package's test code, which can write anything",
			"run":      "`go run` executes the program, which can write anything",
			"install":  "`go install` writes a binary to GOBIN",
			"get":      "`go get` rewrites go.mod and go.sum",
			"generate": "`go generate` runs the directives in the source, which write files",
			"clean":    "`go clean` deletes build output", "fix": "`go fix` rewrites source files",
			"work": "`go work` writes go.work", "tool": "`go tool` runs an arbitrary tool",
		},
	},
	"go mod": {
		reads: set("why", "graph", "verify", "download"),
		mutates: map[string]string{
			"tidy": "`go mod tidy` rewrites go.mod and go.sum",
			"edit": "`go mod edit` rewrites go.mod", "init": "`go mod init` writes go.mod",
			"vendor": "`go mod vendor` writes the vendor directory",
		},
	},
}

// subcommandOf returns the first operand — the subcommand — skipping the
// program's own flags. ok is false when that word is an expansion, because a
// subcommand nobody can read must not be matched against either table.
func subcommandOf(args []bashparser.Word) (string, bool) {
	for _, w := range args {
		if strings.HasPrefix(w.Text, "-") {
			continue
		}
		if !w.Literal {
			return "", false
		}
		return w.Text, true
	}
	return "", true
}

func indexOf(args []bashparser.Word, text string) int {
	for i, w := range args {
		if w.Text == text {
			return i
		}
	}
	return len(args)
}

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// directoryBuiltins change the shell's working directory and nothing else.
//
// Kept here rather than in readOnlyPrograms because that table also feeds the
// host classifier, which treats cd as a directory change to track, not as a
// program. Missing, it made plan mode ask about the most common research
// shape there is — `cd <repo> && grep … | cut …` — measured on a real goal
// session where every other component of the line was already read-only.
// Moving somewhere cannot write; a redirect after it is still judged on its
// own, so `cd /etc && echo x > passwd` stays a write.
var directoryBuiltins = map[string]bool{"cd": true, "pushd": true, "popd": true}

// literalsAfter returns the literal words following sub, false if any of them
// is an expansion — a word nobody can read cannot be shown to only list.
func literalsAfter(args []bashparser.Word, sub string) ([]string, bool) {
	var out []string
	for _, w := range args[indexOf(args, sub)+1:] {
		if !w.Literal {
			return nil, false
		}
		out = append(out, w.Text)
	}
	return out, true
}

// flagName is a flag without its "=value".
func flagName(a string) string {
	if i := strings.IndexByte(a, '='); i >= 0 {
		return a[:i]
	}
	return a
}

// listsWith implements the shape git tag and git branch share: with no
// arguments, or only listing flags, they list; a list-mode flag (-l, --list,
// --contains, --merged, …) makes positionals patterns or commits rather than
// names to create; and any flag outside the set — -d, -m, -f, -a for tag — is
// not a listing.
func listsWith(rest []string, listing, listMode map[string]bool) bool {
	inList, positional := false, false
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			positional = true
			continue
		}
		f := flagName(a)
		// -n<num> (tag annotations) is a short flag with an attached value.
		if strings.HasPrefix(f, "-n") && !strings.HasPrefix(f, "--") {
			f = "-n"
		}
		if !listing[f] && !listMode[f] {
			return false
		}
		if listMode[f] {
			inList = true
		}
	}
	return !positional || inList
}

func gitTagLists(rest []string) bool {
	return listsWith(rest,
		set("-n", "--sort", "--format", "--column", "--no-column", "-i", "--ignore-case", "--omit-empty", "--color"),
		set("-l", "--list", "--contains", "--no-contains", "--merged", "--no-merged", "--points-at"))
}

func gitBranchLists(rest []string) bool {
	return listsWith(rest,
		set("-a", "--all", "-r", "--remotes", "-v", "-vv", "--verbose", "--sort", "--format", "--column", "--no-column",
			"-i", "--ignore-case", "--omit-empty", "--color", "--no-color", "--abbrev", "--no-abbrev", "--show-current"),
		set("-l", "--list", "--contains", "--no-contains", "--merged", "--no-merged", "--points-at"))
}

func gitRemoteLists(rest []string) bool {
	if len(rest) == 0 {
		return true
	}
	switch rest[0] {
	case "-v", "--verbose":
		return len(rest) == 1
	case "get-url", "show":
		return true
	}
	return false
}

// gitConfigReads accepts the getters, in both the flag form and the
// subcommand form git 2.46 added, and refuses anything that also sets.
func gitConfigReads(rest []string) bool {
	if len(rest) > 0 && (rest[0] == "get" || rest[0] == "list") {
		for _, a := range rest[1:] {
			if a == "--edit" || a == "-e" {
				return false
			}
		}
		return true
	}
	getter := false
	for _, a := range rest {
		switch flagName(a) {
		case "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "-l", "--get-color", "--get-colorbool":
			getter = true
		case "--unset", "--unset-all", "--add", "--replace-all", "--rename-section", "--remove-section", "-e", "--edit":
			return false
		}
	}
	return getter
}
