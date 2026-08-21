package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The bug this guards: seatbelt and bwrap match on the real path, so an
// unresolved symlink denies writes to the very directory being allowed. On
// macOS /tmp is a symlink to /private/tmp, so a project there would find the
// sandbox refusing writes to its own files.
func TestResolveRootsFollowsSymlinks(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("/tmp symlink behaviour is macOS-specific")
	}
	got := resolveRoots([]string{"/tmp"})
	if len(got) != 1 || got[0] != "/private/tmp" {
		t.Errorf("resolveRoots([/tmp]) = %v, want [/private/tmp]", got)
	}
}

func TestResolveRootsHandlesASymlinkedProject(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("cannot symlink:", err)
	}
	got := resolveRoots([]string{link})
	want, _ := filepath.EvalSymlinks(real)
	if len(got) != 1 || got[0] != want {
		t.Errorf("resolveRoots(symlink) = %v, want [%s]", got, want)
	}
}

// A root that doesn't exist yet must be kept, not dropped — silently narrowing
// the sandbox is worse than allowing a path that isn't there.
func TestResolveRootsKeepsUnresolvablePaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created-yet")
	got := resolveRoots([]string{missing})
	if len(got) != 1 || !strings.HasSuffix(got[0], "not-created-yet") {
		t.Errorf("resolveRoots(missing) = %v, want the path kept", got)
	}
}

func TestResolveRootsDedupesAndDropsEmpty(t *testing.T) {
	dir := t.TempDir()
	got := resolveRoots([]string{dir, dir, "", dir + "/"})
	if len(got) != 1 {
		t.Errorf("resolveRoots = %v, want one deduped entry", got)
	}
}

// The profile the sandbox actually builds must contain the resolved root.
func TestSeatbeltProfileUsesResolvedRoots(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt is macOS-only")
	}
	s := NewSeatbelt(nil, "")
	profile := s.profile(Request{WorkingDir: "/tmp"})
	if !strings.Contains(profile, `"/private/tmp"`) {
		t.Errorf("profile should allow the resolved root:\n%s", profile)
	}
}
