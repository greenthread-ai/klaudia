package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/fuzzy"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// ToolInfo is a searchable catalog entry for a deferred tool.
type ToolInfo struct {
	Name        string
	Description string
}

// ToolSearchInput is the ToolSearch tool's input.
type ToolSearchInput struct {
	Query string `json:"query" jsonschema:"description=keywords describing the capability you need"`
}

// ToolSearch lets the model discover and load deferred tools by capability.
type ToolSearch struct {
	schema  *schema.Schema
	catalog []ToolInfo
}

// NewToolSearch constructs the ToolSearch tool with a catalog of deferred tools.
func NewToolSearch(catalog []ToolInfo) (*ToolSearch, error) {
	s, err := schema.For[ToolSearchInput]()
	if err != nil {
		return nil, fmt.Errorf("toolsearch: build schema: %w", err)
	}
	return &ToolSearch{schema: s, catalog: catalog}, nil
}

func (t *ToolSearch) Name() string { return "ToolSearch" }

func (t *ToolSearch) Description(context.Context) (string, error) {
	return "Many tools are not loaded by default. Call ToolSearch with keywords describing the capability you need " +
		"to find and load relevant tools; matching tools become available on the next step.", nil
}

func (t *ToolSearch) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *ToolSearch) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in ToolSearchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Query) == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

// PermissionRequest: ToolSearch only reveals already-available local tools.
func (t *ToolSearch) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *ToolSearch) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

// maxToolSearchResults caps how many tools a single search reveals. Deferred
// tools exist to keep the context small, and a broad query must not undo that
// by loading an entire MCP server's surface at once; the reply says so when it
// truncates, which is the model's cue to ask something narrower.
const maxToolSearchResults = 25

// scored pairs a catalog entry with its relevance to a query.
type scored struct {
	info  ToolInfo
	hits  int // how many query terms matched at all
	score int
}

// termScore rates one query term against one tool, in tiers: naming the tool
// beats describing it, and an exact substring beats a fuzzy one.
//
// The fuzzy tier is deliberately last and small. It is what lets "gametime"
// find godot_game_time and absorbs the odd typo, but it matches loosely enough
// that letting it outrank a real description hit would bury the obvious answer.
func termScore(term, name, desc string) int {
	switch {
	case name == term:
		return 120
	case strings.Contains(name, term):
		return 40
	case strings.Contains(desc, term):
		return 12
	}
	// Only names are matched fuzzily. Descriptions are long enough that almost
	// any short pattern appears in them as some scattered subsequence.
	if s, ok := fuzzy.Subsequence(term, name); ok {
		return 2 + s/100
	}
	return 0
}

// rank scores the catalog against the query terms and returns the best matches,
// most relevant first, along with the total number that matched.
//
// Matching is OR, ranked, not AND. Requiring every term to appear in the same
// tool meant a descriptive query returned nothing at all: "godot game time
// freeze runtime state digest" found none of the 65 loaded Godot tools, while
// the bare word "godot" found all of them — including godot_game_time, whose
// own description contains freeze, step and state. The more terms a tool
// matches the higher it ranks, so precision survives without the cliff.
func rank(catalog []ToolInfo, terms []string) (top []ToolInfo, total int) {
	matches := make([]scored, 0, len(catalog))
	for _, entry := range catalog {
		name := strings.ToLower(entry.Name)
		desc := strings.ToLower(entry.Description)
		s := scored{info: entry}
		for _, term := range terms {
			if ts := termScore(term, name, desc); ts > 0 {
				s.hits++
				s.score += ts
			}
		}
		if s.hits > 0 {
			matches = append(matches, s)
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].hits != matches[j].hits {
			return matches[i].hits > matches[j].hits // covering more of the query wins
		}
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].info.Name < matches[j].info.Name // stable, readable ties
	})

	total = len(matches)
	if len(matches) > maxToolSearchResults {
		matches = matches[:maxToolSearchResults]
	}
	top = make([]ToolInfo, len(matches))
	for i, m := range matches {
		top[i] = m.info
	}
	return top, total
}

func (t *ToolSearch) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in ToolSearchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(in.Query))
	matches, total := rank(t.catalog, terms)

	matchedNames := make([]string, 0, len(matches))
	for _, entry := range matches {
		matchedNames = append(matchedNames, entry.Name)
	}
	if tctx.Reveal != nil {
		tctx.Reveal(matchedNames...)
	}
	if len(matches) == 0 {
		return []Result{{Content: fmt.Sprintf("No tools matched %s.", in.Query)}}, nil
	}

	var b strings.Builder
	if total > len(matches) {
		fmt.Fprintf(&b, "Loaded the %d best of %d matching tool(s); search again with more specific terms for the rest:", len(matches), total)
	} else {
		fmt.Fprintf(&b, "Loaded %d tool(s):", len(matches))
	}
	for _, entry := range matches {
		fmt.Fprintf(&b, "\n- %s: %s", entry.Name, entry.Description)
	}
	return []Result{{Content: b.String()}}, nil
}
