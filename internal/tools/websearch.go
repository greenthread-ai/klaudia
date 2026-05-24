package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread/klaudia/internal/browser"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

type WebSearchInput struct {
	Query          string   `json:"query" jsonschema:"description=The search query to run"`
	Engine         string   `json:"engine,omitempty" jsonschema:"description=Search engine to use: ddg or google (defaults to DDG)"`
	AllowedDomains []string `json:"allowed_domains,omitempty" jsonschema:"description=Optional domains to include; subdomains are matched"`
	BlockedDomains []string `json:"blocked_domains,omitempty" jsonschema:"description=Optional domains to exclude; subdomains are matched"`
	MaxResults     int      `json:"max_results,omitempty" jsonschema:"description=Maximum number of results to return; defaults to 8 and caps at 20"`
}

type WebSearch struct {
	schema *schema.Schema
	engine *browser.Engine
}

func NewWebSearch(engine *browser.Engine) (*WebSearch, error) {
	s, err := schema.For[WebSearchInput]()
	if err != nil {
		return nil, fmt.Errorf("websearch: build schema: %w", err)
	}
	return &WebSearch{schema: s, engine: engine}, nil
}

func (w *WebSearch) Name() string { return "WebSearch" }

func (w *WebSearch) Description(context.Context) (string, error) {
	return "Search the web using a lazily launched headless Chrome browser. Defaults to DuckDuckGo; set engine to google when specifically needed. Returns result titles, URLs, and snippets as JSON.", nil
}

func (w *WebSearch) InputSchema() json.RawMessage { return w.schema.Raw }

func (w *WebSearch) ValidateInput(raw json.RawMessage) error {
	if err := w.schema.Validate(raw); err != nil {
		return err
	}
	var in WebSearchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Query) == "" {
		return fmt.Errorf("query is required")
	}
	engine := strings.ToLower(strings.TrimSpace(in.Engine))
	if engine != "" && engine != "ddg" && engine != "duckduckgo" && engine != "duckduckgo-html" && engine != "google" {
		return fmt.Errorf("engine must be ddg or google")
	}
	return nil
}

func (w *WebSearch) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in WebSearchInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.Query}
}

func (w *WebSearch) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return networkClassDecision(pctx)
}

func (w *WebSearch) Execute(ctx context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in WebSearchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	results, err := w.engine.Search(ctx, browser.SearchOptions{
		Engine:         in.Engine,
		Query:          in.Query,
		AllowedDomains: in.AllowedDomains,
		BlockedDomains: in.BlockedDomains,
		MaxResults:     in.MaxResults,
	})
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return nil, err
	}
	return []Result{{Content: string(out)}}, nil
}
