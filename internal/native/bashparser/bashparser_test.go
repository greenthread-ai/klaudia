package bashparser

import "testing"

func TestParsePrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"git status", "git status"},
		{"git status -s", "git status"},
		{"ls", "ls"},
		{"npm run build", "npm run"},
		{"echo hi", "echo hi"},
	}
	for _, c := range cases {
		a, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got := a.Prefix(); got != c.want {
			t.Errorf("Parse(%q).Prefix() = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParsePipe(t *testing.T) {
	a, err := Parse("cat foo | grep bar")
	if err != nil {
		t.Fatal(err)
	}
	if !a.HasPipe {
		t.Error("expected HasPipe for piped command")
	}
	if len(a.Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(a.Commands))
	}
}

func TestParseError(t *testing.T) {
	if _, err := Parse("for do done ("); err == nil {
		t.Error("expected parse error for malformed input")
	}
}
