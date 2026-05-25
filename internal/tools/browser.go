package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/greenthread/klaudia/internal/browser"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

type BrowserNavigateInput struct {
	URL string `json:"url" jsonschema:"description=The HTTP or HTTPS URL to navigate to"`
}

type BrowserSnapshotInput struct {
	IncludeHTML     bool `json:"include_html,omitempty" jsonschema:"description=Include the rendered page HTML in the result"`
	IncludeMarkdown bool `json:"include_markdown,omitempty" jsonschema:"description=Include rendered page markdown in the result"`
}

type BrowserNavigate struct {
	schema *schema.Schema
	engine *browser.Engine
}

type BrowserSnapshot struct {
	schema *schema.Schema
	engine *browser.Engine
}

func NewBrowserNavigate(engine *browser.Engine) (*BrowserNavigate, error) {
	s, err := schema.For[BrowserNavigateInput]()
	if err != nil {
		return nil, fmt.Errorf("browsernavigate: build schema: %w", err)
	}
	return &BrowserNavigate{schema: s, engine: engine}, nil
}

func NewBrowserSnapshot(engine *browser.Engine) (*BrowserSnapshot, error) {
	s, err := schema.For[BrowserSnapshotInput]()
	if err != nil {
		return nil, fmt.Errorf("browsersnapshot: build schema: %w", err)
	}
	return &BrowserSnapshot{schema: s, engine: engine}, nil
}

func (b *BrowserNavigate) Name() string { return "BrowserNavigate" }

func (b *BrowserNavigate) Description(context.Context) (string, error) {
	return "Navigate the shared lazy headless Chrome browser to a URL. Use BrowserSnapshot afterward to inspect the rendered page.", nil
}

func (b *BrowserNavigate) InputSchema() json.RawMessage { return b.schema.Raw }

func (b *BrowserNavigate) ValidateInput(raw json.RawMessage) error {
	if err := b.schema.Validate(raw); err != nil {
		return err
	}
	var in BrowserNavigateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	return validateHTTPURL(in.URL)
}

func (b *BrowserNavigate) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in BrowserNavigateInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.URL}
}

func (b *BrowserNavigate) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return networkClassDecision(pctx)
}

func (b *BrowserNavigate) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in BrowserNavigateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if b.engine == nil {
		return []Result{{Content: "Error: browser engine is not configured", IsError: true}}, nil
	}
	if err := b.engine.Navigate(in.URL); err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	snap, err := b.engine.Snapshot(false, false)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, err
	}
	return []Result{{Content: string(out)}}, nil
}

func (b *BrowserSnapshot) Name() string { return "BrowserSnapshot" }

func (b *BrowserSnapshot) Description(context.Context) (string, error) {
	return "Capture the shared browser's current page. Can include rendered HTML and/or markdown; use include_markdown=true for readable page content.", nil
}

func (b *BrowserSnapshot) InputSchema() json.RawMessage { return b.schema.Raw }

func (b *BrowserSnapshot) ValidateInput(raw json.RawMessage) error { return b.schema.Validate(raw) }

func (b *BrowserSnapshot) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (b *BrowserSnapshot) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return networkClassDecision(pctx)
}

func (b *BrowserSnapshot) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in BrowserSnapshotInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if b.engine == nil {
		return []Result{{Content: "Error: browser engine is not configured", IsError: true}}, nil
	}
	snap, err := b.engine.Snapshot(in.IncludeHTML, in.IncludeMarkdown)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, err
	}
	return []Result{{Content: string(out)}}, nil
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("url must be an absolute http or https URL")
	}
	return nil
}
