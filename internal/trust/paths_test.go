package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func testRoots(t *testing.T) (Roots, string, string) {
	t.Helper()
	home := t.TempDir()
	project := filepath.Join(home, "work", "app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	return NewRoots(home, project), home, canonical(project)
}

func TestClassifyPathZones(t *testing.T) {
	r, home, project := testRoots(t)
	for _, tc := range []struct {
		path string
		want Zone
	}{
		// Project work.
		{filepath.Join(project, "main.go"), ZoneProject},
		{filepath.Join(project, "deep/nested/file.ts"), ZoneProject},

		// The operating system.
		{"/etc/hosts", ZoneHost},
		{"/etc/nginx/nginx.conf", ZoneHost},
		{"/usr/local/bin/tool", ZoneHost},
		{"/opt/acme/config.yaml", ZoneHost},
		{"/Library/LaunchDaemons/x.plist", ZoneHost},
		{"/Applications/Foo.app", ZoneHost},

		// Scratch space inside a host prefix is not the OS.
		{"/var/tmp/build", ZoneProject},

		// Credentials.
		{filepath.Join(home, ".ssh/id_rsa"), ZoneSensitive},
		{filepath.Join(home, ".aws/credentials"), ZoneSensitive},
		{filepath.Join(home, ".config/gcloud/x.json"), ZoneSensitive},
		{"/somewhere/deploy.pem", ZoneSensitive},
		// ...but known_hosts is not a secret and ssh rewrites it constantly.
		{filepath.Join(home, ".ssh/known_hosts"), ZoneProject},

		// Tool caches must stay silent or every build prompts.
		{filepath.Join(home, ".cache/go-build/ab/cd"), ZoneProject},
		{filepath.Join(home, "go/pkg/mod/github.com/x"), ZoneProject},
		{filepath.Join(home, ".npm/_cacache"), ZoneProject},
		{filepath.Join(home, ".cargo/registry"), ZoneProject},
		{filepath.Join(home, "Library/Caches/pip"), ZoneProject},

		// Per-user machine configuration is a host change even though it's in
		// $HOME — appending to a shell rc persists into every future shell.
		{filepath.Join(home, ".zshrc"), ZoneHost},
		{filepath.Join(home, ".bash_profile"), ZoneHost},
		{filepath.Join(home, ".gitconfig"), ZoneHost},
		{filepath.Join(home, "Library/LaunchAgents/com.x.plist"), ZoneHost},

		// The rest of home is the user's data, not the OS. Deliberate.
		{filepath.Join(home, "Documents/notes.md"), ZoneProject},
	} {
		if got := r.ClassifyPath(canonical(tc.path)); got != tc.want {
			t.Errorf("ClassifyPath(%s) = %s, want %s", tc.path, got, tc.want)
		}
	}
}

// A project living under a host prefix is ordinary work. Losing this makes the
// feature unusable for anyone whose code isn't under $HOME.
func TestProjectRootBeatsHostPrefix(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRoots(dir, project)
	if got := r.ClassifyPath(canonical(filepath.Join(project, "src/main.go"))); got != ZoneProject {
		t.Errorf("a file in the project = %s, want project", got)
	}
}

// A root of "/" would make everything project work and silently disable the
// whole policy — the CI-as-root case.
func TestRefusesDangerousProjectRoots(t *testing.T) {
	r := NewRoots("/home/u", "/", "/etc", "/usr")
	if len(r.Project) != 0 {
		t.Errorf("dangerous roots should be refused, got %v", r.Project)
	}
	if got := r.ClassifyPath("/etc/hosts"); got != ZoneHost {
		t.Errorf("with roots refused, /etc/hosts = %s, want host", got)
	}
}

// macOS: /etc is a symlink to /private/etc. Policy and paths must be compared
// in the same shape or a write to /etc looks like it lands nowhere in policy.
func TestMacOSPrivateSymlinksResolve(t *testing.T) {
	real, err := filepath.EvalSymlinks("/etc")
	if err != nil || real == "/etc" {
		t.Skip("no /etc symlink on this platform")
	}
	r := NewRoots(t.TempDir())
	for _, p := range []string{"/etc/hosts", real + "/hosts"} {
		if got := r.ClassifyPath(canonical(p)); got != ZoneHost {
			t.Errorf("ClassifyPath(%s) = %s, want host", p, got)
		}
	}
}

// Writing a file that doesn't exist yet is the normal case; resolution has to
// walk up to an existing ancestor rather than give up.
func TestCanonicalResolvesNonexistentLeaf(t *testing.T) {
	r := NewRoots(t.TempDir())
	got := r.ClassifyPath(canonical("/etc/brand-new-file.conf"))
	if got != ZoneHost {
		t.Errorf("a new file under /etc = %s, want host", got)
	}
}

func TestResolveHandlesRelativeAndTilde(t *testing.T) {
	r, home, project := testRoots(t)
	if got := r.Resolve(project, "src/main.go"); got != filepath.Join(project, "src/main.go") {
		t.Errorf("relative resolve = %q", got)
	}
	if got := r.Resolve(project, "~/.ssh/id_rsa"); got != canonical(filepath.Join(home, ".ssh/id_rsa")) {
		t.Errorf("tilde resolve = %q", got)
	}
	if got := r.Resolve(project, "../.."); !under(canonical(home), got) && got != canonical(home) {
		t.Logf("parent traversal resolved to %q", got) // informational
	}
}

// /etc/nginx must not cover /etc/nginx-extra.
func TestUnderMatchesOnComponentBoundaries(t *testing.T) {
	if under("/etc/nginx-extra/x", "/etc/nginx") {
		t.Error("/etc/nginx must not cover /etc/nginx-extra")
	}
	if !under("/etc/nginx/sites/x", "/etc/nginx") {
		t.Error("/etc/nginx should cover its subtree")
	}
	if !under("/etc/nginx", "/etc/nginx") {
		t.Error("a directory is under itself")
	}
}

// Reported from a live session: `2>/dev/null` was gated as "writes /dev/null".
//
// Writing to a pseudo-device changes nothing about the machine, and discarding
// output is one of the most common things a command does. A false prompt on it
// costs more than the whole guardrail is worth.
func TestPseudoDevicesAreNotHostChanges(t *testing.T) {
	r, _, _ := testRoots(t)
	for _, p := range []string{
		"/dev/null", "/dev/zero", "/dev/full", "/dev/random", "/dev/urandom",
		"/dev/stdin", "/dev/stdout", "/dev/stderr", "/dev/tty",
		"/dev/fd/3", "/dev/pts/2", "/dev/ptmx",
	} {
		if z := r.ClassifyPath(canonical(p)); z.Protected() {
			t.Errorf("%s classified as %s; writing to it changes nothing", p, z)
		}
	}
}

// The reason /dev is a host prefix at all: a block device is the one thing
// under it that a stray write really does destroy.
func TestBlockDevicesAreStillHostChanges(t *testing.T) {
	r, _, _ := testRoots(t)
	for _, p := range []string{
		"/dev/disk2", "/dev/rdisk0", "/dev/sda", "/dev/nvme0n1", "/dev/mem",
	} {
		if z := r.ClassifyPath(canonical(p)); z != ZoneHost {
			t.Errorf("%s classified as %s, want host — dd to it destroys a disk", p, z)
		}
	}
}

// /tmp is scratch space on every unix. It used to be a host change on macOS
// only, because /tmp resolves to /private/tmp and the resolved form was being
// added to the host prefixes — so the same command asked for permission on one
// machine and not the other.
func TestTmpIsScratchSpaceEverywhere(t *testing.T) {
	r, _, _ := testRoots(t)
	for _, p := range []string{"/tmp", "/tmp/scratch.txt", "/tmp/build/out.o"} {
		if z := r.ClassifyPath(canonical(p)); z.Protected() {
			t.Errorf("%s classified as %s, want scratch space", p, z)
		}
	}
}
