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
