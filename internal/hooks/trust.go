package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/greenthread-ai/klaudia/internal/session"
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

// Store records which project hook sets the user has approved.
//
// The zero value reads and writes ~/.klaudia/hooks.json. Path is settable so
// tests do not have to move HOME, and so a future per-machine state directory
// has somewhere to plug in.
type Store struct {
	Path string
}

// storeFile is the on-disk shape: project directory to approved fingerprint.
//
// Keyed by project, not by fingerprint alone. The same `make fmt` hook in two
// unrelated repositories is two decisions, because what is being trusted is the
// repository — approving a command once should not pre-approve every other
// checkout that happens to ship the same line.
type storeFile struct {
	Approved map[string]string `json:"approved"`
}

func (s Store) path() (string, error) {
	if s.Path != "" {
		return s.Path, nil
	}
	// The config root, like every other file Klaudia keeps: built from the
	// home directory, it ignored KLAUDIA_CONFIG_DIR, so a sandboxed run shared
	// the real user's hook approvals.
	return filepath.Join(session.ConfigRoot(), "hooks.json"), nil
}

func (s Store) read() storeFile {
	var f storeFile
	p, err := s.path()
	if err != nil {
		return f
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return f
	}
	// A corrupt file is treated as empty rather than fatal: the consequence is
	// being asked again, and the alternative is a session that cannot start
	// because of a truncated write in a cache.
	_ = json.Unmarshal(data, &f)
	return f
}

// approved reports whether this exact hook set has been approved for this
// project. An empty fingerprint — no project hooks — is approved trivially; the
// callers never reach it, but a Store that answered "no" to "may I run nothing"
// would be a confusing thing to debug.
func (s Store) approved(project, fingerprint string) bool {
	if fingerprint == "" {
		return true
	}
	key, err := filepath.Abs(project)
	if err != nil {
		return false
	}
	return s.read().Approved[key] == fingerprint
}

// approve records the approval, replacing any earlier one for this project.
//
// Replacing rather than accumulating: the question answered was "may this set
// run", and keeping the previous set's fingerprint would mean reverting a hook
// to a formerly-approved value skips the prompt. That is precisely the move an
// attacker makes after being approved once.
func (s Store) approve(project, fingerprint string) error {
	p, err := s.path()
	if err != nil {
		return err
	}
	key, err := filepath.Abs(project)
	if err != nil {
		return err
	}
	f := s.read()
	if f.Approved == nil {
		f.Approved = map[string]string{}
	}
	f.Approved[key] = fingerprint
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	// 0600: this file decides what runs on the machine. A hook set is approved
	// by fingerprint, so anything that can write here can pre-approve a command
	// of its choosing.
	return os.WriteFile(p, data, 0o600)
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
