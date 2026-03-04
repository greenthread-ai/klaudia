// klaudia bash-parser — Bash command parser for autocomplete
//
// Pure Go replacement for tree-sitter-bash WASM.
// Parses bash commands and extracts structure for completion.
//
// Usage:
//   echo 'FOO=bar git commit -m "msg"' | klaudia-bash-parser
//
// Output (JSON):
//   {"command":"git","args":["commit","-m","msg"],"envVars":["FOO=bar"]}
//
// Exit codes: 0 = parsed OK, 1 = parse error or no input
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type ParseResult struct {
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	EnvVars  []string `json:"envVars"`
	HasPipe  bool     `json:"hasPipe"`
	Partial  bool     `json:"partial"`
	LastWord string   `json:"lastWord"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		os.Exit(1)
	}
	input := scanner.Text()
	if input == "" {
		os.Exit(1)
	}

	result := parse(input)

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

func parse(input string) ParseResult {
	result := ParseResult{
		Args:    []string{},
		EnvVars: []string{},
	}

	parser := syntax.NewParser(
		syntax.KeepComments(false),
		syntax.Variant(syntax.LangBash),
	)

	// Try parsing — if it fails, the command is likely partial/incomplete
	reader := strings.NewReader(input)
	prog, err := parser.Parse(reader, "")
	if err != nil {
		result.Partial = true
		// Extract what we can from the raw input
		parts := strings.Fields(input)
		if len(parts) > 0 {
			result.LastWord = parts[len(parts)-1]
			// Check for env vars at start
			for _, p := range parts {
				if strings.Contains(p, "=") && !strings.HasPrefix(p, "-") {
					result.EnvVars = append(result.EnvVars, p)
				} else {
					if result.Command == "" {
						result.Command = p
					} else {
						result.Args = append(result.Args, p)
					}
				}
			}
		}
		return result
	}

	// Walk the AST
	syntax.Walk(prog, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.BinaryCmd:
			if n.Op == syntax.Pipe {
				result.HasPipe = true
			}
		case *syntax.CallExpr:
			extractCall(n, &result, input)
			return false // Don't recurse into this call's children
		}
		return true
	})

	// Determine last word for completion
	trimmed := strings.TrimRight(input, " \t")
	if strings.HasSuffix(input, " ") || strings.HasSuffix(input, "\t") {
		result.LastWord = ""
	} else {
		parts := strings.Fields(trimmed)
		if len(parts) > 0 {
			result.LastWord = parts[len(parts)-1]
		}
	}

	return result
}

func extractCall(call *syntax.CallExpr, result *ParseResult, input string) {
	// Extract env var assignments
	for _, assign := range call.Assigns {
		name := assign.Name.Value
		val := nodeText(assign.Value, input)
		result.EnvVars = append(result.EnvVars, name+"="+val)
	}

	// Extract command and args from the word list
	for i, word := range call.Args {
		text := wordText(word, input)
		if i == 0 && result.Command == "" {
			result.Command = text
		} else {
			result.Args = append(result.Args, text)
		}
	}
}

func wordText(word *syntax.Word, input string) string {
	if word == nil {
		return ""
	}
	start := word.Pos().Offset()
	end := word.End().Offset()
	if int(end) <= len(input) {
		return input[start:end]
	}
	// Fallback: reconstruct from parts
	var parts []string
	for _, part := range word.Parts {
		if lit, ok := part.(*syntax.Lit); ok {
			parts = append(parts, lit.Value)
		}
	}
	return strings.Join(parts, "")
}

func nodeText(node *syntax.Word, input string) string {
	if node == nil {
		return ""
	}
	return wordText(node, input)
}
