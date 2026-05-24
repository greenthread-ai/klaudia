package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread/klaudia/internal/browser"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

type WebFetchInput struct {
	URL    string `json:"url" jsonschema:"description=The HTTP or HTTPS URL to fetch"`
	Prompt string `json:"prompt,omitempty" jsonschema:"description=Optional extraction hint for the model; the full markdown is returned"`
}

type WebFetchOutput struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Markdown string `json:"markdown"`
}

type WebFetch struct {
	schema *schema.Schema
	engine *browser.Engine
}

func NewWebFetch(engine *browser.Engine) (*WebFetch, error) {
	s, err := schema.For[WebFetchInput]()
	if err != nil {
		return nil, fmt.Errorf("webfetch: build schema: %w", err)
	}
	return &WebFetch{schema: s, engine: engine}, nil
}

func (w *WebFetch) Name() string { return "WebFetch" }

func (w *WebFetch) Description(context.Context) (string, error) {
	return "Fetch a web page using a lazily launched headless Chrome browser and return the rendered page as markdown. Useful for reading dynamic pages and URLs from WebSearch results.", nil
}

func (w *WebFetch) InputSchema() json.RawMessage { return w.schema.Raw }

func (w *WebFetch) ValidateInput(raw json.RawMessage) error {
	if err := w.schema.Validate(raw); err != nil {
		return err
	}
	var in WebFetchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if err := validateHTTPURL(in.URL); err != nil {
		return err
	}
	return nil
}

func (w *WebFetch) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in WebFetchInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.URL}
}

func (w *WebFetch) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return networkClassDecision(pctx)
}

func (w *WebFetch) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in WebFetchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if err := w.engine.Navigate(in.URL); err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	snap, err := w.engine.Snapshot(false, true)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	out, err := json.MarshalIndent(WebFetchOutput{URL: snap.URL, Title: snap.Title, Markdown: snap.Markdown}, "", "  ")
	if err != nil {
		return nil, err
	}
	return []Result{{Content: string(out)}}, nil
}
