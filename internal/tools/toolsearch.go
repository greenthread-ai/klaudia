package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
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

func (t *ToolSearch) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in ToolSearchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(in.Query))
	matches := make([]ToolInfo, 0)
	matchedNames := make([]string, 0)
	for _, entry := range t.catalog {
		haystack := strings.ToLower(entry.Name + " " + entry.Description)
		matched := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, entry)
			matchedNames = append(matchedNames, entry.Name)
		}
	}

	if tctx.Reveal != nil {
		tctx.Reveal(matchedNames...)
	}
	if len(matches) == 0 {
		return []Result{{Content: fmt.Sprintf("No tools matched %s.", in.Query)}}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Loaded %d tool(s):", len(matches))
	for _, entry := range matches {
		fmt.Fprintf(&b, "\n- %s: %s", entry.Name, entry.Description)
	}
	return []Result{{Content: b.String()}}, nil
}
