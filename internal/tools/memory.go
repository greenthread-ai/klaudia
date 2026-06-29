package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// MemoryInput is the Memory tool's input.
type MemoryInput struct {
	Operation   string `json:"operation" jsonschema:"enum=search,enum=add,enum=view,enum=recent,enum=stale,enum=by_tag,enum=promote,enum=supersede,description=Operation to perform"`
	Scope       string `json:"scope,omitempty" jsonschema:"enum=session,enum=project,description=Where to write for operation=add: session (default) writes .klaudia/MEMORY.md; project writes .klaudia/KNOWLEDGE.md"`
	Query       string `json:"query,omitempty" jsonschema:"description=Search terms (required for operation=search)"`
	Content     string `json:"content,omitempty" jsonschema:"description=The note to store (required for operation=add)"`
	Tag         string `json:"tag,omitempty" jsonschema:"description=Frontmatter tag to filter on (required for operation=by_tag)"`
	Name        string `json:"name,omitempty" jsonschema:"description=Detail note name without .md (required for promote/supersede)"`
	Replacement string `json:"replacement,omitempty" jsonschema:"description=The replacing note's name (required for operation=supersede)"`
	Within      string `json:"within,omitempty" jsonschema:"description=Duration window for operation=recent — accepts Go duration syntax plus 'Nd' for days (e.g. 7d, 24h). Default 7d."`
	OlderThan   string `json:"older_than,omitempty" jsonschema:"description=Duration threshold for operation=stale. Same format as within. Default 30d."`
}

// Memory lets the agent recall, search, and store long-term notes that persist
// across sessions — so it can remember project context, decisions, and gotchas
// instead of re-deriving or re-asking.
type Memory struct {
	schema *schema.Schema
	store  memory.Store
	cwd    string
}

// NewMemory constructs the Memory tool backed by store.
func NewMemory(store memory.Store) (*Memory, error) {
	return NewMemoryForProject(store, "")
}

// NewMemoryForProject constructs the Memory tool with a project cwd for
// project-scope writes to .klaudia/KNOWLEDGE.md.
func NewMemoryForProject(store memory.Store, cwd string) (*Memory, error) {
	s, err := schema.For[MemoryInput]()
	if err != nil {
		return nil, fmt.Errorf("memory: build schema: %w", err)
	}
	return &Memory{schema: s, store: store, cwd: cwd}, nil
}

func (m *Memory) Name() string { return "Memory" }

func (m *Memory) Description(context.Context) (string, error) {
	return "Your persistent memory across sessions. Use it to recall prior context before " +
		"asking the user or re-investigating, and to store and curate durable facts " +
		"(decisions, conventions, gotchas) for later.\n\n" +
		"Recall ops: \"search\" (matches query terms across the MEMORY.md index and the " +
		".klaudia/memory/*.md detail notes; a detail hit is tagged \"file.md: …\", Read " +
		"that file for full context), \"view\" (read all session notes), \"recent\" " +
		"(detail notes touched within `within` — default 7d, newest first), \"stale\" " +
		"(detail notes untouched for longer than `older_than` — default 30d, oldest " +
		"first), \"by_tag\" (detail notes whose frontmatter tag list contains `tag`).\n\n" +
		"Write ops: \"add\" (store a new note via content; scope=\"session\" appends to " +
		"MEMORY.md by default, scope=\"project\" appends to KNOWLEDGE.md), \"promote\" " +
		"(copy a detail note's body into KNOWLEDGE.md and mark the source superseded — " +
		"requires `name`), \"supersede\" (record that `name` is replaced by `replacement` " +
		"— rewrites both files' frontmatter).\n\n" +
		"Search early when resuming work. Use `stale` to audit aged context; promote " +
		"validated lessons to project knowledge.", nil
}

func (m *Memory) InputSchema() json.RawMessage { return m.schema.Raw }

func (m *Memory) ValidateInput(raw json.RawMessage) error {
	if err := m.schema.Validate(raw); err != nil {
		return err
	}
	var in MemoryInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if in.Scope != "" && in.Scope != "session" && in.Scope != "project" {
		return fmt.Errorf("scope %q is invalid (want session|project)", in.Scope)
	}
	switch in.Operation {
	case "search", "view":
	case "add":
		if strings.TrimSpace(in.Content) == "" {
			return fmt.Errorf("operation \"add\" requires content")
		}
	case "recent", "stale":
		// Window/threshold parsing is deferred to Execute so we can give the
		// user a concrete default. Just validate format here if present.
		field := in.Within
		if in.Operation == "stale" {
			field = in.OlderThan
		}
		if field != "" {
			if _, err := parseMemoryDuration(field); err != nil {
				return fmt.Errorf("operation %q: invalid duration %q (try 7d, 24h, 1h30m): %w", in.Operation, field, err)
			}
		}
	case "by_tag":
		if strings.TrimSpace(in.Tag) == "" {
			return fmt.Errorf("operation \"by_tag\" requires tag")
		}
	case "promote":
		if strings.TrimSpace(in.Name) == "" {
			return fmt.Errorf("operation \"promote\" requires name")
		}
	case "supersede":
		if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Replacement) == "" {
			return fmt.Errorf("operation \"supersede\" requires name and replacement")
		}
	default:
		return fmt.Errorf("operation %q is invalid (want search|view|add|recent|stale|by_tag|promote|supersede)", in.Operation)
	}
	return nil
}

// parseMemoryDuration extends time.ParseDuration with an Nd (days) suffix,
// so the agent and humans can say "7d" / "30d" naturally without converting
// to hours. Stripped to lower-case for lenient input.
func parseMemoryDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// PermissionRequest: memory is the agent's own scratch space, not user files.
func (m *Memory) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (m *Memory) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (m *Memory) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in MemoryInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	switch in.Operation {
	case "add":
		if in.Scope == "project" {
			if strings.TrimSpace(m.cwd) == "" {
				return []Result{{Content: "Failed to store project knowledge: project cwd is unavailable", IsError: true}}, nil
			}
			if err := memory.AddKnowledge(m.cwd, in.Content); err != nil {
				return []Result{{Content: "Failed to store project knowledge: " + err.Error(), IsError: true}}, nil
			}
			return []Result{{Content: "Stored to project knowledge (.klaudia/KNOWLEDGE.md)."}}, nil
		}
		if err := m.store.Add(in.Content); err != nil {
			return []Result{{Content: "Failed to store memory: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: "Stored to session memory."}}, nil
	case "search":
		matches, err := m.store.Search(in.Query)
		if err != nil {
			return []Result{{Content: "Memory search failed: " + err.Error(), IsError: true}}, nil
		}
		if len(matches) == 0 {
			return []Result{{Content: "No memories matched."}}, nil
		}
		return []Result{{Content: "Recalled memories:\n- " + strings.Join(matches, "\n- ")}}, nil
	case "recent":
		window := 7 * 24 * time.Hour
		if in.Within != "" {
			window, _ = parseMemoryDuration(in.Within) // Validate already passed
		}
		entries, err := m.store.Recent(window)
		if err != nil {
			return []Result{{Content: "Memory recent failed: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: formatEntries(entries, "recent", window)}}, nil
	case "stale":
		threshold := 30 * 24 * time.Hour
		if in.OlderThan != "" {
			threshold, _ = parseMemoryDuration(in.OlderThan)
		}
		entries, err := m.store.Stale(threshold)
		if err != nil {
			return []Result{{Content: "Memory stale failed: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: formatEntries(entries, "stale", threshold)}}, nil
	case "by_tag":
		entries, err := m.store.ByTag(in.Tag)
		if err != nil {
			return []Result{{Content: "Memory by_tag failed: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: formatEntries(entries, "by_tag:"+in.Tag, 0)}}, nil
	case "promote":
		if err := m.store.Promote(in.Name); err != nil {
			return []Result{{Content: "Promote failed: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: "Promoted " + in.Name + " to KNOWLEDGE.md; source marked superseded."}}, nil
	case "supersede":
		if err := m.store.Supersede(in.Name, in.Replacement); err != nil {
			return []Result{{Content: "Supersede failed: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: "Marked " + in.Name + " as superseded by " + in.Replacement + "."}}, nil
	default: // view
		idx, err := m.store.Index()
		if err != nil {
			return []Result{{Content: "Failed to read memory: " + err.Error(), IsError: true}}, nil
		}
		if strings.TrimSpace(idx) == "" {
			return []Result{{Content: "Memory is empty."}}, nil
		}
		return []Result{{Content: idx}}, nil
	}
}

// formatEntries renders the Recent/Stale/ByTag list as a bullet list with
// the title and a relative-age annotation. Empty result returns a friendly
// "none" message that tells the agent the operation succeeded but found
// nothing — important to distinguish from a tool error.
func formatEntries(entries []memory.Entry, label string, window time.Duration) string {
	if len(entries) == 0 {
		switch {
		case window > 0 && label == "recent":
			return "No memory notes touched within " + humanizeDuration(window) + "."
		case window > 0 && label == "stale":
			return "No memory notes older than " + humanizeDuration(window) + "."
		default:
			return "No memory notes matched " + label + "."
		}
	}
	var b strings.Builder
	b.WriteString(label)
	if window > 0 {
		b.WriteString(" (window: ")
		b.WriteString(humanizeDuration(window))
		b.WriteString(")")
	}
	b.WriteString(":\n")
	now := time.Now()
	for _, e := range entries {
		b.WriteString("- ")
		b.WriteString(e.Name)
		if e.Title != "" {
			b.WriteString(" — ")
			b.WriteString(e.Title)
		}
		b.WriteString(" (")
		b.WriteString(humanizeDuration(now.Sub(e.Updated)))
		b.WriteString(" ago)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// humanizeDuration renders d as the largest convenient unit (d/h/m), e.g.
// "5d", "3h", "12m". Doesn't aim for SI precision — just a readable
// annotation in tool output.
func humanizeDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}
