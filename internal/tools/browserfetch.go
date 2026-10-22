package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread-ai/klaudia/internal/browser"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

type BrowserFetchInput struct {
	URL    string `json:"url" jsonschema:"description=The HTTP or HTTPS URL to fetch"`
	Prompt string `json:"prompt,omitempty" jsonschema:"description=Optional extraction hint for the model; the full markdown is returned"`
}

type BrowserFetchOutput struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Markdown string `json:"markdown"`
	// Note names an anti-bot interstitial when that is what came back, because
	// the markdown of one reads like a successful fetch of a very short page.
	// Without it the model reports "the article says: Just a moment...".
	Note string `json:"note,omitempty"`
}

type BrowserFetch struct {
	schema *schema.Schema
	engine *browser.Engine
}

func NewBrowserFetch(engine *browser.Engine) (*BrowserFetch, error) {
	s, err := schema.For[BrowserFetchInput]()
	if err != nil {
		return nil, fmt.Errorf("webfetch: build schema: %w", err)
	}
	return &BrowserFetch{schema: s, engine: engine}, nil
}

func (w *BrowserFetch) Name() string { return "BrowserFetch" }

func (w *BrowserFetch) Description(context.Context) (string, error) {
	return "Fetch a web page by driving a lazily launched headless Chrome browser and return the rendered page as markdown — good for JavaScript-heavy or dynamic pages. Prefer the built-in web_fetch tool when it is available (Claude models); use this when web_fetch is not available (non-Claude models) or when explicitly asked to use the browser. Pairs with BrowserSearch result URLs.", nil
}

func (w *BrowserFetch) InputSchema() json.RawMessage { return w.schema.Raw }

func (w *BrowserFetch) ValidateInput(raw json.RawMessage) error {
	if err := w.schema.Validate(raw); err != nil {
		return err
	}
	var in BrowserFetchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if err := validateHTTPURL(in.URL); err != nil {
		return err
	}
	return nil
}

func (w *BrowserFetch) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in BrowserFetchInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.URL}
}

func (w *BrowserFetch) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return networkClassDecision(pctx)
}

func (w *BrowserFetch) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in BrowserFetchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if w.engine == nil {
		return []Result{{Content: "Error: browser engine is not configured", IsError: true}}, nil
	}
	snap, err := w.engine.Fetch(ctx, browser.FetchOptions{
		URL:             in.URL,
		IncludeMarkdown: true,
		Notify:          tctx.Progress,
	})
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	out := BrowserFetchOutput{URL: snap.URL, Title: snap.Title, Markdown: snap.Markdown}
	if snap.Challenge != "" {
		out.Note = fmt.Sprintf("this is %s, not the requested page — the site did not serve its content", snap.Challenge)
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return []Result{{Content: string(body)}}, nil
}
