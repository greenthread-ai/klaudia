package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/fuzzy"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/schema"
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

// wholeWordBelow is the term length at which substring matching stops being a
// convenience and becomes noise. "is" is a substring of "disk" and of "this",
// so a query containing it matched a scene-management tool and a job lister on
// the strength of two letters. Above this length a bare substring is still
// wanted — it is what lets "runtime" find godot_runtime_state.
const wholeWordBelow = 3

// termScore rates one query term against one tool, in tiers: naming the tool
// beats describing it, and an exact substring beats a fuzzy one.
//
// The fuzzy tier is deliberately last and small. It is what lets "gametime"
// find godot_game_time and absorbs the odd typo, but it matches loosely enough
// that letting it outrank a real description hit would bury the obvious answer.
func termScore(term, name, desc string) int {
	if len(term) < wholeWordBelow {
		// Whole words only, and no fuzzy tier: a two-character subsequence
		// appears in almost every name.
		switch {
		case name == term:
			return 120
		case hasWord(name, term):
			return 40
		case hasWord(desc, term):
			return 12
		}
		return 0
	}
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

// hasWord reports whether term occurs in s bounded by non-alphanumeric
// characters on both sides. Underscores count as boundaries, so "run" is a
// word in "godot_run".
func hasWord(s, term string) bool {
	for i := 0; i+len(term) <= len(s); {
		j := strings.Index(s[i:], term)
		if j < 0 {
			return false
		}
		at := i + j
		if isBoundary(s, at-1) && isBoundary(s, at+len(term)) {
			return true
		}
		i = at + 1
	}
	return false
}

// isBoundary reports whether position i falls outside s or on a character that
// cannot be part of a word.
func isBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9')
}

// A term that matches more than half the catalog has not narrowed anything, so
// it is dropped before qualifying matches. minCatalogForNoise keeps the rule
// off tiny catalogs, where "more than half" is two entries and means nothing.
const minCatalogForNoise = 4

// informativeTerms drops the query terms that match most of the catalog.
//
// OR-matching made descriptive queries work, and brought this with it: a term
// like "the" or "running" qualifies tools that have nothing to do with the
// request, and because a match *reveals* a tool, every one of those spends part
// of the budget deferred loading exists to protect. Asked to "stop the
// background job that is running", Klaudia loaded a Godot screenshot tool — it
// contains the word "running" — and a project runner, for "the".
//
// Which terms are noise is decided against this catalog rather than from a list
// of English stopwords, because it depends on what is loaded: with 65 Godot
// tools present, "godot" narrows nothing either.
//
// If every term is noise the whole query is kept. That is the bare "godot"
// case, where returning all 65 is the honest answer to a question that broad,
// and returning nothing was the original bug.
func informativeTerms(catalog []ToolInfo, terms []string) []string {
	if len(catalog) < minCatalogForNoise || len(terms) < 2 {
		return terms
	}
	df := make(map[string]int, len(terms))
	for _, entry := range catalog {
		name := strings.ToLower(entry.Name)
		desc := strings.ToLower(entry.Description)
		for _, term := range terms {
			if termScore(term, name, desc) > 0 {
				df[term]++
			}
		}
	}
	kept := make([]string, 0, len(terms))
	for _, term := range terms {
		if df[term]*2 <= len(catalog) {
			kept = append(kept, term)
		}
	}
	if len(kept) == 0 {
		return terms
	}
	return kept
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
	terms = informativeTerms(catalog, terms)
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
