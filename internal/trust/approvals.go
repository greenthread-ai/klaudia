package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Approvals records which command sets a repository's config declared — its
// hooks, the MCP servers in its .mcp.json — the user has agreed may run on this
// machine. Each set is identified by a fingerprint its owner computes, so
// editing the set is a new question.
//
// Path is the file; each kind of set keeps its own, so approving a project's
// hooks says nothing about its MCP servers.
type Approvals struct {
	Path string
}

// approvalsFile is the on-disk shape: project directory to approved
// fingerprint.
//
// Keyed by project, not by fingerprint alone. The same `make fmt` hook in two
// unrelated repositories is two decisions, because what is being trusted is the
// repository — approving a command once should not pre-approve every other
// checkout that happens to ship the same line.
type approvalsFile struct {
	Approved map[string]string `json:"approved"`
}

func (a Approvals) read() approvalsFile {
	var f approvalsFile
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return f
	}
	// A corrupt file is treated as empty rather than fatal: the consequence is
	// being asked again, and the alternative is a session that cannot start
	// because of a truncated write in a cache.
	_ = json.Unmarshal(data, &f)
	return f
}

// Approved reports whether this exact set has been approved for this project.
// An empty fingerprint — nothing declared — is approved trivially; a store that
// answered "no" to "may I run nothing" would be a confusing thing to debug.
func (a Approvals) Approved(project, fingerprint string) bool {
	if fingerprint == "" {
		return true
	}
	key, err := filepath.Abs(project)
	if err != nil {
		return false
	}
	return a.read().Approved[key] == fingerprint
}

// Approve records the approval, replacing any earlier one for this project.
//
// Replacing rather than accumulating: the question answered was "may this set
// run", and keeping the previous set's fingerprint would mean reverting to a
// formerly-approved set skips the prompt. That is precisely the move an
// attacker makes after being approved once.
func (a Approvals) Approve(project, fingerprint string) error {
	key, err := filepath.Abs(project)
	if err != nil {
		return err
	}
	f := a.read()
	if f.Approved == nil {
		f.Approved = map[string]string{}
	}
	f.Approved[key] = fingerprint
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o700); err != nil {
		return err
	}
	// 0600: this file decides what runs on the machine. A set is approved by
	// fingerprint, so anything that can write here can pre-approve a command
	// of its choosing.
	return os.WriteFile(a.Path, data, 0o600)
}
