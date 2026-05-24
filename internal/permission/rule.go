package permission

import (
	"fmt"
	"strings"
)

// ParseRule parses a permission rule string into a Rule.
func ParseRule(s string) (Rule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Rule{}, fmt.Errorf("permission rule is empty")
	}

	open := strings.IndexByte(s, '(')
	if open == -1 {
		return Rule{Tool: s}, nil // bare tool name (already non-empty)
	}
	if !strings.HasSuffix(s, ")") {
		return Rule{}, fmt.Errorf("permission rule %q is missing closing paren", s)
	}

	tool := strings.TrimSpace(s[:open])
	if tool == "" {
		return Rule{}, fmt.Errorf("permission rule tool is empty")
	}

	return Rule{Tool: tool, Specifier: s[open+1 : len(s)-1]}, nil
}

// FormatRule formats a Rule as a permission rule string.
func FormatRule(r Rule) string {
	if r.Specifier == "" {
		return r.Tool
	}
	return r.Tool + "(" + r.Specifier + ")"
}

// ParseRules parses permission rule strings into Rules.
func ParseRules(ss []string) ([]Rule, error) {
	rules := make([]Rule, 0, len(ss))
	for _, s := range ss {
		rule, err := ParseRule(s)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}
