package bashparser

import "testing"

func TestRedirectsAreCollected(t *testing.T) {
	for _, tc := range []struct {
		in      string
		targets []string
		append_ []bool
	}{
		{"echo x > /etc/hosts", []string{"/etc/hosts"}, []bool{false}},
		{"echo x >> /etc/hosts", []string{"/etc/hosts"}, []bool{true}},
		{"make 2>&1 > build.log", []string{"build.log"}, []bool{false}},
		{"cmd &> /var/log/out", []string{"/var/log/out"}, []bool{false}},
		{"cat < input.txt", nil, nil}, // reads don't change anything
		{"echo hi", nil, nil},
		{"a > one && b >> two", []string{"one", "two"}, []bool{false, true}},
	} {
		a, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		if len(a.Redirects) != len(tc.targets) {
			t.Errorf("Parse(%q) redirects = %+v, want %v", tc.in, a.Redirects, tc.targets)
			continue
		}
		for i, want := range tc.targets {
			if a.Redirects[i].Target != want {
				t.Errorf("Parse(%q) redirect %d target = %q, want %q", tc.in, i, a.Redirects[i].Target, want)
			}
			if a.Redirects[i].Append != tc.append_[i] {
				t.Errorf("Parse(%q) redirect %d append = %v, want %v", tc.in, i, a.Redirects[i].Append, tc.append_[i])
			}
		}
	}
}

// A redirection inside a subshell or command substitution still writes.
func TestRedirectsInsideNestedConstructs(t *testing.T) {
	for _, in := range []string{
		"( echo x > /etc/hosts )",
		"if true; then echo x > /etc/hosts; fi",
		"for f in a b; do echo $f > /etc/hosts; done",
	} {
		a, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if len(a.Redirects) == 0 || a.Redirects[0].Target != "/etc/hosts" {
			t.Errorf("Parse(%q) missed the nested redirect: %+v", in, a.Redirects)
		}
	}
}

func TestShellPayloads(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`sh -c "echo x > /etc/hosts"`, []string{"echo x > /etc/hosts"}},
		{`bash -c 'rm -rf /'`, []string{"rm -rf /"}},
		{`bash -lc "make install"`, []string{"make install"}},
		{`/bin/sh -c "id"`, []string{"id"}},
		{`eval "systemctl restart nginx"`, []string{"systemctl restart nginx"}},
		{`echo hi`, nil},
		{`sh script.sh`, nil}, // no -c, nothing inline to recurse into
	} {
		a, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		got := a.ShellPayloads()
		if len(got) != len(tc.want) {
			t.Errorf("ShellPayloads(%q) = %q, want %q", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ShellPayloads(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// The payload is opaque to Parse itself — recursion has to be the caller's
// explicit choice, and this pins that contract.
func TestNestedShellPayloadIsNotAutoParsed(t *testing.T) {
	a, _ := Parse(`sh -c "echo x > /etc/hosts"`)
	if len(a.Redirects) != 0 {
		t.Errorf("Parse should not see inside the -c payload, got %+v", a.Redirects)
	}
	inner, err := Parse(a.ShellPayloads()[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(inner.Redirects) != 1 || inner.Redirects[0].Target != "/etc/hosts" {
		t.Errorf("re-parsing the payload should reveal the redirect, got %+v", inner.Redirects)
	}
}

// The dangerous failure isn't a missing path — it's a *plausible wrong* one.
// wordText keeps only literal fragments, so `"$HOME/notes.txt"` reads as the
// absolute path "/notes.txt", and `"$PREFIX/etc/nginx.conf"` reads as a real
// host path that was never in the command. Anything deciding what a command
// touches has to see Literal=false before it trusts Text.
func TestNonLiteralWordsAreFlaggedNotFabricated(t *testing.T) {
	for _, tc := range []struct {
		in      string
		target  string
		literal bool
	}{
		{`echo x > /etc/hosts`, "/etc/hosts", true},
		{`echo x > "$HOME/notes.txt"`, "/notes.txt", false},
		{`echo x > "/etc/$name.conf"`, "/etc/.conf", false},
		{`echo x > $TARGET`, "", false},
		{`echo x > "$(mktemp)"`, "", false},
	} {
		a, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		if len(a.Redirects) != 1 {
			t.Fatalf("Parse(%q) redirects = %+v, want exactly one", tc.in, a.Redirects)
		}
		got := a.Redirects[0]
		if got.Target != tc.target || got.Literal != tc.literal {
			t.Errorf("Parse(%q) = {%q, literal=%v}, want {%q, literal=%v}",
				tc.in, got.Target, got.Literal, tc.target, tc.literal)
		}
	}
}

// A redirect whose target can't be resolved must still be reported. Dropping it
// reported the command as writing nothing at all — a silent fail-open.
func TestUnresolvableRedirectIsStillReported(t *testing.T) {
	a, _ := Parse(`echo x > $TARGET`)
	if len(a.Redirects) != 1 {
		t.Fatalf("an unresolvable redirect must still be reported, got %+v", a.Redirects)
	}
	if a.Redirects[0].Literal {
		t.Error("it must be marked non-literal")
	}
}

// `$SUDO apt-get install nginx` used to produce no commands at all, because the
// empty expanded name was filtered out — the entire invocation vanished.
func TestCommandWithExpandedNameIsNotDropped(t *testing.T) {
	a, err := Parse(`$SUDO apt-get install nginx`)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Commands) != 1 {
		t.Fatalf("got %d commands, want the invocation preserved: %+v", len(a.Commands), a.Commands)
	}
	c := a.Commands[0]
	if c.NameWord.Literal {
		t.Error("an expanded program name must be marked non-literal")
	}
	if len(c.Args) != 3 || c.Args[0] != "apt-get" {
		t.Errorf("arguments should survive: %v", c.Args)
	}
}

func TestHasExpansionFlagsUnreadableLines(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{`go build ./...`, false},
		{`echo x > /etc/hosts`, false},
		{`rm -rf "$DIR"`, true},
		{`eval "$CMD"`, true},
		{`cp a "$(mktemp)"`, true},
	} {
		a, _ := Parse(tc.in)
		if a.HasExpansion != tc.want {
			t.Errorf("Parse(%q).HasExpansion = %v, want %v", tc.in, a.HasExpansion, tc.want)
		}
	}
}

// >| and <> also write.
func TestClobberAndReadWriteRedirects(t *testing.T) {
	for _, in := range []string{`echo x >| /etc/hosts`, `exec 3<> /etc/hosts`} {
		a, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if len(a.Redirects) != 1 || a.Redirects[0].Target != "/etc/hosts" {
			t.Errorf("Parse(%q) redirects = %+v, want /etc/hosts", in, a.Redirects)
		}
	}
}
