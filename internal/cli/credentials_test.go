package cli

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/config"
)

// OS confinement hides credentials unless sandbox.readCredentials says not to.
func TestHiddenCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hide, keep := hiddenCredentials(config.Sandbox{Mode: config.SandboxOS})
	if !slices.Contains(hide, filepath.Join(home, ".ssh")) || !slices.Contains(hide, filepath.Join(home, ".aws")) {
		t.Errorf("hide = %v, want ~/.ssh and ~/.aws", hide)
	}
	if slices.Contains(hide, "/etc/ssh") {
		t.Error("/etc/ssh hidden: the ssh client reads its config there")
	}
	if !slices.Contains(keep, filepath.Join(home, ".ssh", "known_hosts")) {
		t.Errorf("keep = %v, want known_hosts", keep)
	}
	if hide, keep := hiddenCredentials(config.Sandbox{Mode: config.SandboxOS, ReadCredentials: true}); hide != nil || keep != nil {
		t.Errorf("readCredentials = true still hides %v", hide)
	}
}
