package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/browser"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

type BrowserSearchInput struct {
	Query          string   `json:"query" jsonschema:"description=The search query to run"`
	Engine         string   `json:"engine,omitempty" jsonschema:"description=Search engine to use: ddg or google (defaults to DDG)"`
	AllowedDomains []string `json:"allowed_domains,omitempty" jsonschema:"description=Optional domains to include; subdomains are matched"`
	BlockedDomains []string `json:"blocked_domains,omitempty" jsonschema:"description=Optional domains to exclude; subdomains are matched"`
	MaxResults     int      `json:"max_results,omitempty" jsonschema:"description=Maximum number of results to return; defaults to 8 and caps at 20"`
}

type BrowserSearch struct {
	schema *schema.Schema
	engine *browser.Engine
}

func NewBrowserSearch(engine *browser.Engine) (*BrowserSearch, error) {
	s, err := schema.For[BrowserSearchInput]()
	if err != nil {
		return nil, fmt.Errorf("websearch: build schema: %w", err)
	}
	return &BrowserSearch{schema: s, engine: engine}, nil
}

func (w *BrowserSearch) Name() string { return "BrowserSearch" }

func (w *BrowserSearch) Description(context.Context) (string, error) {
	return "Search the web by driving a lazily launched headless Chrome browser (scrapes DuckDuckGo by default; set engine to google when needed). Returns titles, URLs, and snippets as JSON — no citations. Prefer the built-in web_search tool when it is available (Claude models): it returns higher-quality, cited results. Use this Chrome-based search when web_search is not available (non-Claude models) or when explicitly asked to use the browser.", nil
}

func (w *BrowserSearch) InputSchema() json.RawMessage { return w.schema.Raw }

func (w *BrowserSearch) ValidateInput(raw json.RawMessage) error {
	if err := w.schema.Validate(raw); err != nil {
		return err
	}
	var in BrowserSearchInput
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

func (w *BrowserSearch) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in BrowserSearchInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.Query}
}

func (w *BrowserSearch) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return networkClassDecision(pctx)
}

func (w *BrowserSearch) Execute(ctx context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in BrowserSearchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if w.engine == nil {
		return []Result{{Content: "Error: browser engine is not configured", IsError: true}}, nil
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
