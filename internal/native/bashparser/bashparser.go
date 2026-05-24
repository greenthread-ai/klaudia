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

// Analysis is the parsed structure of a command line.
type Analysis struct {
	Commands []Command // simple commands, in source order
	HasPipe  bool      // whether the line contains a pipe
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
