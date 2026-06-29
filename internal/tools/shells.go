package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// bgShell is one background shell tracked by the store.
type bgShell struct {
	id      string
	command string
	proc    *sandbox.BackgroundProcess
	offset  int // bytes of output already returned by BashOutput
	started time.Time
}

// ShellStore holds the background shells started by the Bash tool, shared with
// the BashOutput and KillShell tools. Shells are session-scoped: they persist
// across turns until killed or the session ends. Safe for concurrent use.
type ShellStore struct {
	parent context.Context
	mu     sync.Mutex
	shells map[string]*bgShell
	seq    int
}

// NewShellStore creates a store whose shells are parented to ctx (cancelling it,
// or calling KillAll, terminates them).
func NewShellStore(ctx context.Context) *ShellStore {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ShellStore{parent: ctx, shells: map[string]*bgShell{}}
}

// Start launches req as a detached background shell and returns its id.
func (s *ShellStore) Start(e sandbox.Executor, req sandbox.Request) (string, error) {
	req.Timeout = 0 // background shells run until they exit or are killed
	proc, err := sandbox.StartBackground(s.parent, e, req)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := fmt.Sprintf("bash_%d", s.seq)
	s.shells[id] = &bgShell{id: id, command: req.Command, proc: proc, started: time.Now()}
	return id, nil
}

// ShellOutput is the incremental read of a background shell.
type ShellOutput struct {
	ID       string
	Command  string
	Output   string
	Running  bool
	ExitCode int
}

// Read returns output produced since the last read, advancing the read offset.
func (s *ShellStore) Read(id string) (ShellOutput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shells[id]
	if !ok {
		return ShellOutput{}, false
	}
	data, newOffset, done, code := sh.proc.Read(sh.offset)
	sh.offset = newOffset
	return ShellOutput{ID: id, Command: sh.command, Output: data, Running: !done, ExitCode: code}, true
}

// Kill terminates a shell. Returns false if there is no such shell.
func (s *ShellStore) Kill(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shells[id]
	if !ok {
		return false
	}
	sh.proc.Kill()
	return true
}

// List returns the tracked shells, newest first, for descriptions/diagnostics.
func (s *ShellStore) List() []ShellOutput {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ShellOutput, 0, len(s.shells))
	for _, sh := range s.shells {
		out = append(out, ShellOutput{ID: sh.id, Command: sh.command, Running: sh.proc.Running()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// KillAll terminates every shell (session teardown).
func (s *ShellStore) KillAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.shells {
		sh.proc.Kill()
	}
}

// --- BashOutput tool ---

type BashOutputInput struct {
	BashID string `json:"bash_id" jsonschema:"description=The background shell id returned by Bash (run_in_background)"`
	Filter string `json:"filter,omitempty" jsonschema:"description=Optional regex; only matching output lines are returned"`
}

type BashOutput struct {
	schema *schema.Schema
	shells *ShellStore
}

func NewBashOutput(shells *ShellStore) (*BashOutput, error) {
	s, err := schema.For[BashOutputInput]()
	if err != nil {
		return nil, fmt.Errorf("bashoutput: build schema: %w", err)
	}
	return &BashOutput{schema: s, shells: shells}, nil
}

func (t *BashOutput) Name() string { return "BashOutput" }

func (t *BashOutput) Description(context.Context) (string, error) {
	return "Read new output from a background shell started by Bash with run_in_background. " +
		"Returns only output produced since the last read, plus whether the shell is still running.", nil
}

func (t *BashOutput) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *BashOutput) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in BashOutputInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.BashID) == "" {
		return fmt.Errorf("bash_id is required")
	}
	if in.Filter != "" {
		if _, err := regexp.Compile(in.Filter); err != nil {
			return fmt.Errorf("invalid filter regex: %w", err)
		}
	}
	return nil
}

func (t *BashOutput) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *BashOutput) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx) // reading output of a shell we already started
}

func (t *BashOutput) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in BashOutputInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.shells == nil {
		return []Result{{Content: "background shells are not available", IsError: true}}, nil
	}
	out, ok := t.shells.Read(in.BashID)
	if !ok {
		return []Result{{Content: fmt.Sprintf("No such background shell %q.", in.BashID), IsError: true}}, nil
	}
	body := out.Output
	if in.Filter != "" {
		body = filterLines(body, in.Filter)
	}
	var b strings.Builder
	if body == "" {
		b.WriteString("[no new output]")
	} else {
		b.WriteString(body)
	}
	if out.Running {
		b.WriteString("\n[shell running]")
	} else {
		fmt.Fprintf(&b, "\n[shell exited, code %d]", out.ExitCode)
	}
	return []Result{{Content: b.String()}}, nil
}

// filterLines keeps only lines matching the (already-validated) regex.
func filterLines(s, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return s
	}
	var kept []string
	for _, ln := range strings.Split(s, "\n") {
		if re.MatchString(ln) {
			kept = append(kept, ln)
		}
	}
	return strings.Join(kept, "\n")
}

// --- KillShell tool ---

type KillShellInput struct {
	ShellID string `json:"shell_id" jsonschema:"description=The background shell id to terminate"`
}

type KillShell struct {
	schema *schema.Schema
	shells *ShellStore
}

func NewKillShell(shells *ShellStore) (*KillShell, error) {
	s, err := schema.For[KillShellInput]()
	if err != nil {
		return nil, fmt.Errorf("killshell: build schema: %w", err)
	}
	return &KillShell{schema: s, shells: shells}, nil
}

func (t *KillShell) Name() string { return "KillShell" }

func (t *KillShell) Description(context.Context) (string, error) {
	return "Terminate a background shell started by Bash with run_in_background, by its id.", nil
}

func (t *KillShell) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *KillShell) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in KillShellInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.ShellID) == "" {
		return fmt.Errorf("shell_id is required")
	}
	return nil
}

func (t *KillShell) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *KillShell) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx) // stopping a shell we already started
}

func (t *KillShell) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in KillShellInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.shells == nil || !t.shells.Kill(in.ShellID) {
		return []Result{{Content: fmt.Sprintf("No such background shell %q.", in.ShellID), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf("Killed background shell %s.", in.ShellID)}}, nil
}
