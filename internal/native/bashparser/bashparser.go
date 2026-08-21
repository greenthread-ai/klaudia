// Package bashparser parses bash command lines, absorbing the standalone
// tools/bash-parser binary in-process. It is used to derive a permission
// specifier (e.g. "git status") for a Bash invocation and to detect structure
// such as pipes and multiple commands.
package bashparser

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Command is a single simple command (a program invocation) within a line.
type Command struct {
	Name string   // the program, e.g. "git"
	Args []string // arguments following the program name
}

// Redirect is an output redirection target. Only writing redirections are
// collected — a reader deciding what a command *changes* cares about `>`,
// `>>` and `&>`, not about where stdin came from.
type Redirect struct {
	Target string // the file the redirection writes to
	Append bool   // >> rather than >
}

// Analysis is the parsed structure of a command line.
type Analysis struct {
	Commands  []Command  // simple commands, in source order
	Redirects []Redirect // writing redirections, in source order
	HasPipe   bool       // whether the line contains a pipe
}

// Parse parses a bash command line. On a parse error the returned Analysis is
// empty and err is non-nil; callers typically fall back to treating the whole
// line as opaque.
func Parse(input string) (Analysis, error) {
	parser := syntax.NewParser(
		syntax.KeepComments(false),
		syntax.Variant(syntax.LangBash),
	)
	prog, err := parser.Parse(strings.NewReader(input), "")
	if err != nil {
		return Analysis{}, err
	}

	var a Analysis
	syntax.Walk(prog, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.BinaryCmd:
			if n.Op == syntax.Pipe || n.Op == syntax.PipeAll {
				a.HasPipe = true
			}
		case *syntax.Stmt:
			// Redirections hang off the statement, not the call — without this
			// `echo x > /etc/hosts` looks like a harmless `echo`.
			for _, r := range n.Redirs {
				if r.Word == nil {
					continue
				}
				switch r.Op {
				case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll:
					if t := wordText(r.Word); t != "" {
						a.Redirects = append(a.Redirects, Redirect{
							Target: t,
							Append: r.Op == syntax.AppOut || r.Op == syntax.AppAll,
						})
					}
				}
			}
		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				return true
			}
			words := make([]string, 0, len(n.Args))
			for _, w := range n.Args {
				words = append(words, wordText(w))
			}
			if len(words) > 0 && words[0] != "" {
				a.Commands = append(a.Commands, Command{Name: words[0], Args: words[1:]})
			}
		}
		return true
	})
	return a, nil
}

// Prefix returns a permission specifier for the first command: the program
// name plus its first non-flag argument (a subcommand), e.g. "git status".
// Returns "" if there are no commands.
//
// This is a best-effort heuristic: it does not know which flags take values, so
// for commands like "git -C /repo log" it may pick the flag's value rather than
// the subcommand. Permission rules should account for this approximation.
func (a Analysis) Prefix() string {
	if len(a.Commands) == 0 {
		return ""
	}
	c := a.Commands[0]
	for _, arg := range c.Args {
		if !strings.HasPrefix(arg, "-") {
			return c.Name + " " + arg
		}
	}
	return c.Name
}

// ShellPayloads returns the script text passed to an inline shell — the
// argument after -c for sh/bash/zsh/dash/ksh, and the argument to eval.
//
// The parser records that script as a single opaque word, so a caller
// reasoning about what a command line does has to re-Parse these to see
// inside. Returning them separately keeps that recursion the caller's explicit
// decision rather than something Parse does invisibly (and unboundedly).
func (a Analysis) ShellPayloads() []string {
	var out []string
	for _, c := range a.Commands {
		switch base(c.Name) {
		case "sh", "bash", "zsh", "dash", "ksh":
			for i, arg := range c.Args {
				// -c, and combined forms like -lc / -ec that end in c.
				if strings.HasPrefix(arg, "-") && strings.HasSuffix(arg, "c") && i+1 < len(c.Args) {
					out = append(out, c.Args[i+1])
					break
				}
			}
		case "eval":
			// eval concatenates its arguments into one script.
			if len(c.Args) > 0 {
				out = append(out, strings.Join(c.Args, " "))
			}
		}
	}
	return out
}

// base strips any directory from a program name, so /usr/bin/sudo and sudo
// are recognised alike.
func base(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// wordText extracts the literal text of a word, joining its literal parts. Word
// parts that aren't plain literals (expansions, quotes contents) contribute
// their literal value where available.
func wordText(w *syntax.Word) string {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				if lit, ok := dp.(*syntax.Lit); ok {
					b.WriteString(lit.Value)
				}
			}
		}
	}
	return b.String()
}
