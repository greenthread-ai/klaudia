package trust

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The scenario the design exists for: one approval, then the rest of the
// operation proceeds without interruption.
func TestOneApprovalCoversTheWholeOperation(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	l := NewLedger(roots)

	if _, err := l.Mint(Request{
		Summary:  "Install nginx and configure it as a development proxy",
		Reason:   "the task asks for the app to run behind a local proxy",
		Paths:    []string{"/etc/nginx/nginx.conf"},
		Services: []string{"nginx"},
		Packages: []string{"nginx"},
	}); err != nil {
		t.Fatal(err)
	}

	steps := []string{
		"sudo apt-get install -y nginx",
		"sudo mkdir -p /etc/nginx/conf.d",
		"sudo tee /etc/nginx/conf.d/app.conf",
		"sudo systemctl restart nginx",
	}
	for _, step := range steps {
		as := ClassifyCommand(step, roots)
		concerns, _ := as.NeedsAgreement()
		_, drift := l.Cover(concerns)
		if len(drift) > 0 {
			t.Errorf("%s drifted out of the approved scope: %s", step, dumpEffects(Assessment{Effects: drift}))
		}
	}
}

// Scope drift: anything the approval did not describe still stops.
func TestScopeDrift(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	l := NewLedger(roots)
	if _, err := l.Mint(Request{
		Summary:  "Install and configure nginx",
		Paths:    []string{"/etc/nginx/nginx.conf"},
		Services: []string{"nginx"},
		Packages: []string{"nginx"},
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		cmd string
		why string
	}{
		{"echo x >> /etc/hosts", "a different host file"},
		{"sudo systemctl restart postgresql", "a different service"},
		{"sudo apt-get install -y postgresql", "a different package"},
		{"sudo apt-get purge nginx", "removal was never approved"},
		{"sudo useradd -m deploy", "user administration has no scope vocabulary"},
		{"sudo rm -rf /etc/nginx", "a recursive delete of a system directory"},
	} {
		t.Run(tc.why, func(t *testing.T) {
			as := ClassifyCommand(tc.cmd, roots)
			concerns, ask := as.NeedsAgreement()
			if !ask {
				t.Fatalf("expected %q to raise a concern at all", tc.cmd)
			}
			if _, drift := l.Cover(concerns); len(drift) == 0 {
				t.Fatalf("%q should have drifted (%s)", tc.cmd, tc.why)
			}
		})
	}
}

// Widening has to be generous enough to be useful and bounded enough to be
// safe. Approving one file inside /etc/nginx covers that directory; approving
// /etc/hosts does not hand over /etc.
func TestWidening(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"/etc/nginx/nginx.conf", "/etc/nginx"},
		{"/etc/nginx/conf.d/app.conf", "/etc/nginx/conf.d"},
		{"/etc/nginx", "/etc/nginx"},
		{"/etc/hosts", "/etc/hosts"}, // parent is a system root: no widening
		// Extensionless leaf: indistinguishable from a directory name without
		// statting, so it is granted exactly rather than widened.
		{"/usr/local/bin/tool", "/usr/local/bin/tool"},
		{"/usr/local/etc/app/config.yaml", "/usr/local/etc/app"},
		{"/etc", ""}, // a system root itself is never grantable
		{"/usr", ""},
		{"/", ""},
	} {
		if got := widenPath(tc.in); got != tc.want {
			t.Errorf("widenPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestApprovingOneFileDoesNotGrantItsSystemRoot(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	l := NewLedger(roots)
	if _, err := l.Mint(Request{Summary: "add a hosts entry", Paths: []string{"/etc/hosts"}}); err != nil {
		t.Fatal(err)
	}
	as := ClassifyCommand("echo x > /etc/resolv.conf", roots)
	concerns, _ := as.NeedsAgreement()
	if _, drift := l.Cover(concerns); len(drift) == 0 {
		t.Fatalf("a grant for /etc/hosts must not cover /etc/resolv.conf")
	}
}

// Patterns are not expressible. Widening is this package's decision, taken from
// a real path, and a requester that could pass "/etc/**" would route around it.
func TestWildcardsAreRefused(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	l := NewLedger(roots)
	for _, p := range []string{"/etc/**", "/etc/nginx/*.conf", "/etc/ngin?"} {
		if _, err := l.Mint(Request{Summary: "x", Paths: []string{p}}); err == nil {
			t.Errorf("%q was accepted as a grant path", p)
		}
	}
	if _, err := l.Mint(Request{Summary: "x", Services: []string{"nginx*"}}); err == nil {
		t.Error("a service pattern was accepted")
	}
}

func TestWholeSystemDirectoryIsRefused(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	l := NewLedger(roots)
	for _, p := range []string{"/etc", "/usr", "/", "/Library"} {
		if _, err := l.Mint(Request{Summary: "x", Paths: []string{p}}); err == nil {
			t.Errorf("%q was accepted as a grant path", p)
		}
	}
}

func TestRevocationIsImmediate(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	l := NewLedger(roots)
	g, err := l.Mint(Request{Summary: "restart nginx", Services: []string{"nginx"}})
	if err != nil {
		t.Fatal(err)
	}
	as := ClassifyCommand("sudo systemctl restart nginx", roots)
	concerns, _ := as.NeedsAgreement()
	if _, drift := l.Cover(concerns); len(drift) != 0 {
		t.Fatalf("grant should cover the restart")
	}
	if !l.Revoke(g.ID) {
		t.Fatal("revoke reported nothing to revoke")
	}
	if _, drift := l.Cover(concerns); len(drift) == 0 {
		t.Fatal("a revoked grant still covered the restart")
	}
	if n := len(l.List()); n != 0 {
		t.Fatalf("List returned %d grants after revocation", n)
	}
}

// An effect we could not pin down must never be matched against a scope: a
// grant for /etc/nginx silently authorising `> "$TARGET"` would be the worst
// possible failure of this design.
func TestUncertainEffectsAreNeverCovered(t *testing.T) {
	roots, _, _ := corpusRoots(t)
	l := NewLedger(roots)
	if _, err := l.Mint(Request{Summary: "configure nginx", Paths: []string{"/etc/nginx/nginx.conf"}}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{
		`echo x > "$TARGET"`,
		`eval "$CMD"`,
		`sh -c "$SCRIPT"`,
	} {
		as := ClassifyCommand(cmd, roots)
		concerns, ask := as.NeedsAgreement()
		if !ask {
			t.Fatalf("%q should raise a concern", cmd)
		}
		if _, drift := l.Cover(concerns); len(drift) == 0 {
			t.Errorf("%q was covered by a grant despite being unreadable", cmd)
		}
	}
}

// Grants are session state. Persisting one would mean a permission the user no
// longer remembers giving, so the type carries no serialisation and the ledger
// exposes no way to save or load.
func TestGrantsAreNotPersistable(t *testing.T) {
	gt := reflect.TypeOf(Grant{})
	for i := 0; i < gt.NumField(); i++ {
		if tag, ok := gt.Field(i).Tag.Lookup("json"); ok {
			t.Errorf("Grant.%s carries a json tag (%q): grants must not be serialisable", gt.Field(i).Name, tag)
		}
	}
	lt := reflect.TypeOf(&Ledger{})
	for i := 0; i < lt.NumMethod(); i++ {
		name := lt.Method(i).Name
		for _, banned := range []string{"Save", "Load", "Path", "Marshal", "Unmarshal", "Write", "Persist"} {
			if strings.Contains(name, banned) {
				t.Errorf("Ledger.%s exists: grants must stay in memory for the session", name)
			}
		}
	}
}

// A grant covers the project-relative path it was given, resolved against the
// project root rather than Klaudia's own working directory.
func TestRelativeGrantPathsResolveAgainstTheProject(t *testing.T) {
	roots, _, proj := corpusRoots(t)
	l := NewLedger(roots)
	g, err := l.Mint(Request{Summary: "x", Paths: []string{"src/generated/out.go"}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(proj, "src", "generated")
	if len(g.Scope.Paths) != 1 || g.Scope.Paths[0] != want {
		t.Fatalf("scope paths = %v, want [%s]", g.Scope.Paths, want)
	}
}
