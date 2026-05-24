package permission

import "testing"

func TestParseRule(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Rule
	}{
		{
			name: "bare tool",
			in:   "Edit",
			want: Rule{Tool: "Edit"},
		},
		{
			name: "tool with prefix specifier",
			in:   "Bash(git status:*)",
			want: Rule{Tool: "Bash", Specifier: "git status:*"},
		},
		{
			name: "surrounding whitespace",
			in:   "  Bash(git status:*)  ",
			want: Rule{Tool: "Bash", Specifier: "git status:*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRule(tt.in)
			if err != nil {
				t.Fatalf("ParseRule(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseRule(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatRule(t *testing.T) {
	tests := []struct {
		name string
		in   Rule
		want string
	}{
		{
			name: "bare tool",
			in:   Rule{Tool: "Edit"},
			want: "Edit",
		},
		{
			name: "with specifier",
			in:   Rule{Tool: "Bash", Specifier: "git status:*"},
			want: "Bash(git status:*)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatRule(tt.in); got != tt.want {
				t.Fatalf("FormatRule(%#v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseRuleFormatRuleRoundTrip(t *testing.T) {
	in := "Bash(git status:*)"
	rule, err := ParseRule(in)
	if err != nil {
		t.Fatalf("ParseRule(%q) error = %v", in, err)
	}
	if got := FormatRule(rule); got != in {
		t.Fatalf("FormatRule(ParseRule(%q)) = %q, want %q", in, got, in)
	}
}

func TestParseRuleMalformed(t *testing.T) {
	tests := []string{
		"Bash(git status:*",
		"",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if _, err := ParseRule(tt); err == nil {
				t.Fatalf("ParseRule(%q) error = nil, want error", tt)
			}
		})
	}
}

func TestParseRules(t *testing.T) {
	got, err := ParseRules([]string{"Edit", "Bash(git status:*)"})
	if err != nil {
		t.Fatalf("ParseRules error = %v", err)
	}
	want := []Rule{{Tool: "Edit"}, {Tool: "Bash", Specifier: "git status:*"}}
	if len(got) != len(want) {
		t.Fatalf("ParseRules length = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ParseRules[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}

	if _, err := ParseRules([]string{"Edit", "Bash(git status:*"}); err == nil {
		t.Fatal("ParseRules malformed input error = nil, want error")
	}
}
