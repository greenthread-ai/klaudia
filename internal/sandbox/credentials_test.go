package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// credHome makes a home with an ssh key, known_hosts, and a .netrc, and
// returns it with the hide/keep lists for it.
func credHome(t *testing.T) (home string, hide, keep []string) {
	t.Helper()
	home, _ = filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("SECRET-KEY"), 0o600)
	os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte("github.com ssh-ed25519 AAAA"), 0o644)
	os.WriteFile(filepath.Join(home, ".netrc"), []byte("machine x password SECRET-NETRC"), 0o600)
	hide = []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".netrc"), filepath.Join(home, ".aws")}
	keep = []string{filepath.Join(home, ".ssh", "known_hosts")}
	return home, hide, keep
}

// `--ro-bind / /` made every credential readable inside the sandbox. The
// directories are now covered with tmpfs, the files with /dev/null, the
// exceptions bound back; paths that do not exist (.aws here) are skipped.
func TestBwrapHidesCredentials(t *testing.T) {
	home, hide, keep := credHome(t)
	b := NewBwrap(nil, "")
	b.Hide, b.Keep = hide, keep
	joined := strings.Join(b.buildArgs(Request{Command: "true", WorkingDir: "/work"}), " ")
	for _, want := range []string{
		"--tmpfs " + filepath.Join(home, ".ssh"),
		"--ro-bind /dev/null " + filepath.Join(home, ".netrc"),
		"--ro-bind " + filepath.Join(home, ".ssh", "known_hosts") + " " + filepath.Join(home, ".ssh", "known_hosts"),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q\n%s", want, joined)
		}
	}
	if strings.Contains(joined, ".aws") {
		t.Errorf("a missing path was passed to bwrap (it would fail to mount): %s", joined)
	}
	if strings.Index(joined, "--tmpfs "+filepath.Join(home, ".ssh")) > strings.Index(joined, "--ro-bind "+filepath.Join(home, ".ssh", "known_hosts")) {
		t.Error("an exception is bound before the directory it sits in is hidden, so the tmpfs would cover it")
	}
}

func TestSeatbeltHidesCredentials(t *testing.T) {
	home, hide, keep := credHome(t)
	s := NewSeatbelt(nil, "")
	s.Hide, s.Keep = hide, keep
	p := s.profile(Request{WorkingDir: "/work"})
	deny := strings.Index(p, "(deny file-read*")
	allow := strings.Index(p, "(allow file-read*")
	if deny < 0 || !strings.Contains(p, `(subpath "`+filepath.Join(home, ".ssh")+`")`) {
		t.Fatalf("profile does not deny reading ~/.ssh:\n%s", p)
	}
	if allow < deny || !strings.Contains(p, `(literal "`+filepath.Join(home, ".ssh", "known_hosts")+`")`) {
		t.Errorf("known_hosts is not re-allowed after the deny:\n%s", p)
	}
}

// Where bwrap can create namespaces, check the real thing: the key and .netrc
// read as nothing, known_hosts still reads.
func TestBwrapHidesCredentialsForReal(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	if err := exec.Command("bwrap", "--ro-bind", "/", "/", "true").Run(); err != nil {
		t.Skipf("bwrap cannot create namespaces here: %v", err)
	}
	home, hide, keep := credHome(t)
	b := NewBwrap(nil, "")
	b.Hide, b.Keep = hide, keep
	resp, err := b.Run(context.Background(), Request{WorkingDir: t.TempDir(),
		Command: "cat " + filepath.Join(home, ".ssh", "id_ed25519") + " " + filepath.Join(home, ".netrc") + " 2>&1; cat " + filepath.Join(home, ".ssh", "known_hosts")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.Stdout, "SECRET") {
		t.Errorf("a credential was readable inside the sandbox:\n%s", resp.Stdout)
	}
	if !strings.Contains(resp.Stdout, "github.com ssh-ed25519") {
		t.Errorf("known_hosts was hidden too:\n%s", resp.Stdout)
	}
}
