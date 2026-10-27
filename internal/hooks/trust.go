package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// A hook from the project config is a command that arrived with the repository.
//
// Cloning a repo and starting Klaudia in it would otherwise be enough to run
// whatever its .klaudia/config.toml says, before the user has read a line of it.
// That is not a hypothetical attack shape — it is the same one that made
// editors stop sourcing project-local config, and the reason .vscode tasks and
// direnv both grew an approval step.
//
// So the user confirms a project's hooks once. The unit of approval is the whole
// set, identified by a fingerprint, for two reasons. A prompt has to show what
// is being agreed to, and "these four commands" is something a person can read
// where "hooks: yes" is not. And approving a set rather than a file means
// *editing* it comes back for confirmation: the dangerous case is not the repo
// that asks on day one, it is the repo that asks for something harmless and
// changes it in a later pull.
//
// What this does not do: nothing here inspects what a command *is*. An approved
// hook runs unconfined with the user's privileges, which is what makes the
// feature useful and what makes the confirmation load-bearing. The prompt is the
// security control; there is no second line of defence behind it.

// Store records which project hook sets the user has approved, in a
// trust.Approvals file of its own.
//
// The zero value reads and writes ~/.klaudia/hooks.json. Path is settable so
// tests do not have to move HOME, and so a future per-machine state directory
// has somewhere to plug in.
type Store struct {
	Path string
}

func (s Store) path() string {
	if s.Path != "" {
		return s.Path
	}
	// The config root, like every other file Klaudia keeps: built from the
	// home directory, it ignored KLAUDIA_CONFIG_DIR, so a sandboxed run shared
	// the real user's hook approvals.
	return filepath.Join(session.ConfigRoot(), "hooks.json")
}

func (s Store) approved(project, fingerprint string) bool {
	return trust.Approvals{Path: s.path()}.Approved(project, fingerprint)
}

func (s Store) approve(project, fingerprint string) error {
	return trust.Approvals{Path: s.path()}.Approve(project, fingerprint)
}

// fingerprint hashes a hook set: every field that affects what runs, in order.
//
// Order is included — hooks run in configuration order and a formatter before a
// linter is not the same automation as the reverse. Timeout is included because
// it bounds the damage a wedged hook does. The source file is not, so moving a
// hook between the two levels is not an approval event; the level is already
// what decides whether approval is needed at all.
func fingerprint(hs []Hook) string {
	if len(hs) == 0 {
		return ""
	}
	h := sha256.New()
	for _, k := range hs {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\n", k.Event, k.raw, k.Command, k.Timeout)
	}
	return hex.EncodeToString(h.Sum(nil))
}
