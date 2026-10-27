package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// A server in a project's .mcp.json is a command that arrived with the
// repository, exactly as a project hook is, and starting Klaudia in a fresh
// clone used to launch it before the user had read a line of the file. So
// project servers wait for one approval, remembered per project until the set
// changes; the global ~/.klaudia/.mcp.json is the user's own and is trusted.
// docs/hooks.md has the reasoning, which is the same for both.
//
// HTTP servers are held too. Nothing is launched, but the model's tool calls,
// with whatever it has read, go to a URL the repository chose.

// Confirm asks the user whether a project's MCP servers may start. lines
// describe each server (name and command or URL) and files are the configs
// they came from. A nil Confirm means nobody can be asked.
type Confirm func(ctx context.Context, lines, files []string) bool

// ProjectTrust decides which configured servers may start. Admit filters a
// config at load and on every reload; Decide asks, once per set per session,
// when there is someone to ask.
type ProjectTrust struct {
	Dir string // the project
	// Path is the approvals file; empty means mcp-approvals.json under the
	// config root. Separate from hooks.json: approving a repository's hooks
	// says nothing about its servers.
	Path string

	mu      sync.Mutex
	fp      string // the set the decision below is about
	decided bool
	allowed bool
}

func (t *ProjectTrust) store() trust.Approvals {
	p := t.Path
	if p == "" {
		p = filepath.Join(session.ConfigRoot(), "mcp-approvals.json")
	}
	return trust.Approvals{Path: p}
}

// Admit returns the part of cfg that may start now, and the names of the
// project servers held back for approval. A nil receiver admits everything,
// for callers that do not gate.
func (t *ProjectTrust) Admit(cfg Config) (Config, []string) {
	if t == nil {
		return cfg, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fp := projectFingerprint(cfg)
	if t.allowedLocked(fp) {
		return cfg, nil
	}
	run := Config{MCPServers: map[string]ServerConfig{}}
	var held []string
	for name, sc := range cfg.MCPServers {
		if _, project := cfg.Project[name]; project {
			held = append(held, name)
			continue
		}
		run.MCPServers[name] = sc
	}
	sort.Strings(held)
	return run, held
}

func (t *ProjectTrust) allowedLocked(fp string) bool {
	if fp == "" {
		return true
	}
	if t.fp == fp && t.decided {
		return t.allowed
	}
	return t.store().Approved(t.Dir, fp)
}

// Decide puts cfg's held project servers to the user, if it has not already
// this session. It reports whether they were newly admitted — the caller then
// reloads with Admit's widened result — and a notice to show either way.
func (t *ProjectTrust) Decide(ctx context.Context, cfg Config, confirm Confirm) (admitted bool, notice string) {
	if t == nil {
		return false, ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fp := projectFingerprint(cfg)
	if t.allowedLocked(fp) || (t.fp == fp && t.decided) {
		return false, ""
	}
	lines, files := projectLines(cfg)
	t.fp, t.decided, t.allowed = fp, true, false
	if confirm == nil {
		return false, fmt.Sprintf("%s from %s will not start: servers from a repository need approving interactively, and nobody is here to approve them.",
			countServers(len(lines)), strings.Join(files, ", "))
	}
	if !confirm(ctx, lines, files) {
		// Not the approver's own wording: a headless approver's refusal is
		// written for the model, and this is for the user.
		return false, fmt.Sprintf("%s from %s will not start this session. Approve them in an interactive session (remembered until they change), or pass --allow-host-changes to a headless run.",
			countServers(len(lines)), strings.Join(files, ", "))
	}
	t.allowed = true
	if err := t.store().Approve(t.Dir, fp); err != nil {
		// They start — the user just agreed. They will be asked again next
		// session, a smaller failure than overriding a decision made out loud.
		return true, fmt.Sprintf("could not record the MCP server approval (%v); you will be asked again next session", err)
	}
	return true, ""
}

// Pending describes the project servers Admit is holding: one line per
// server (name and command or URL, as the card shows it), and whether the user
// has already said no to this set this session.
func (t *ProjectTrust) Pending(cfg Config) (lines []string, refused bool) {
	if t == nil {
		return nil, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fp := projectFingerprint(cfg)
	if t.allowedLocked(fp) {
		return nil, false
	}
	lines, _ = projectLines(cfg)
	return lines, t.fp == fp && t.decided
}

// Grant approves cfg's project servers without asking: the user chose to,
// from /mcp, having been shown them there. It overrides a no given earlier in
// the session — changing your mind is what the row is for.
func (t *ProjectTrust) Grant(cfg Config) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fp := projectFingerprint(cfg)
	t.fp, t.decided, t.allowed = fp, true, true
	return t.store().Approve(t.Dir, fp)
}

// projectFingerprint hashes the project servers: every field that decides
// what runs, by name. The source file is not included, so moving a server
// between ./.mcp.json and ./.klaudia/.mcp.json is not an approval event.
func projectFingerprint(cfg Config) string {
	names := make([]string, 0, len(cfg.Project))
	for name := range cfg.Project {
		if _, ok := cfg.MCPServers[name]; ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		b, _ := json.Marshal(cfg.MCPServers[name]) // map keys marshal sorted
		fmt.Fprintf(h, "%s\x00%s\n", name, b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// projectLines describes each project server as the user should read it: the
// command line or URL, verbatim — "start 2 servers: yes" is not a question a
// person can answer.
func projectLines(cfg Config) (lines, files []string) {
	names := make([]string, 0, len(cfg.Project))
	for name := range cfg.Project {
		if _, ok := cfg.MCPServers[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		sc := cfg.MCPServers[name]
		what := strings.TrimSpace(sc.URL)
		if what == "" {
			what = strings.TrimSpace(strings.Join(append([]string{sc.Command}, sc.Args...), " "))
		}
		// Env names, not values: they can change what runs (PATH,
		// LD_PRELOAD), and the values are often tokens.
		if len(sc.Env) > 0 {
			keys := make([]string, 0, len(sc.Env))
			for k := range sc.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			what += " (env: " + strings.Join(keys, ", ") + ")"
		}
		lines = append(lines, name+": "+what)
		if f := cfg.Project[name]; !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return lines, files
}

func countServers(n int) string {
	if n == 1 {
		return "1 MCP server"
	}
	return fmt.Sprintf("%d MCP servers", n)
}
