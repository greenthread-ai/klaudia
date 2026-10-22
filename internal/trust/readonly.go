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
	a, err := bashparser.Parse(cmd)
	if err != nil {
		return MutationUnclear, "the command line could not be parsed"
	}
	if len(a.Commands) == 0 && len(a.Redirects) == 0 {
		return MutationUnclear, "no command was found on the line"
	}
	return inspectAnalysis(a, 0)
}

// inspectAnalysis judges a parsed line by its worst component.
func inspectAnalysis(a bashparser.Analysis, depth int) (Mutation, string) {
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
			return MutationPresent, "it redirects output to a file"
		}
		return MutationPresent, fmt.Sprintf("it redirects output to %s", r.Target)
	}

	verdict, reason := MutationNone, ""
	for _, c := range a.Commands {
		m, why := inspectOne(c, depth)
		if m.rank() > verdict.rank() {
			verdict, reason = m, why
			if verdict == MutationPresent {
				// Nothing a later component says can soften this, and the
				// first offender is the one worth naming.
				break
			}
		}
	}
	return verdict, reason
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
		return inspectAnalysis(inner, depth+1)
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
	if readOnlyPrograms[prog] {
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
}

// subcommands covers git and go, which is where the measured plan-mode session
// was actually blocked. Everything else — npm, cargo, docker, kubectl — is
// absent on purpose: an unclassified multiplexer asks, which is the correct
// answer for a tool nobody has sat down and read the grammar of, and is how
// this list is meant to grow.
var subcommands = map[string]subcommandTable{
	"git": {
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
