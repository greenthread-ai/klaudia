package lsp

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

const (
	initializeTimeout = 30 * time.Second // servers like gopls index on init
	requestTimeout    = 15 * time.Second
	diagnosticsWait   = 10 * time.Second // wait for the server to push diagnostics
)

// Initialize performs the LSP handshake for a workspace root.
func (c *Client) Initialize(ctx context.Context, root string) error {
	ctx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	rootURI := pathToURI(root)
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": "workspace"},
		},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didSave": true},
				"publishDiagnostics": map[string]any{},
				"definition":         map[string]any{},
				"references":         map[string]any{},
			},
		},
	}
	if err := c.call(ctx, "initialize", params, nil); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

// didOpen opens a file with its current on-disk contents so the server analyses
// it (the agent's edits hit disk before this runs).
func (c *Client) didOpen(path, languageID string) (uri string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	uri = pathToURI(path)
	return uri, c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": languageID, "version": 1, "text": string(data),
		},
	})
}

func (c *Client) didClose(uri string) {
	_ = c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// Diagnostics opens path, waits for the server to publish diagnostics for it
// (or a short timeout), and returns them. An empty slice means "no problems".
func (c *Client) Diagnostics(ctx context.Context, path, languageID string) ([]Diagnostic, error) {
	uri := pathToURI(path)
	// Register the waiter BEFORE opening, so a fast server push isn't missed
	// (which would otherwise force a full-timeout wait).
	c.mu.Lock()
	wait := make(chan struct{})
	c.diagCh[uri] = append(c.diagCh[uri], wait)
	c.mu.Unlock()

	if _, err := c.didOpen(path, languageID); err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	select {
	case <-wait:
	case <-time.After(diagnosticsWait):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.diags[uri], nil
}

// Definition returns the definition location(s) for the symbol at pos.
func (c *Client) Definition(ctx context.Context, path, languageID string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/definition", path, languageID, pos, nil)
}

// References returns all references to the symbol at pos (including its declaration).
func (c *Client) References(ctx context.Context, path, languageID string, pos Position) ([]Location, error) {
	extra := map[string]any{"context": map[string]any{"includeDeclaration": true}}
	return c.locations(ctx, "textDocument/references", path, languageID, pos, extra)
}

func (c *Client) locations(ctx context.Context, method, path, languageID string, pos Position, extra map[string]any) ([]Location, error) {
	uri, err := c.didOpen(path, languageID)
	if err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}
	for k, v := range extra {
		params[k] = v
	}
	// The result may be a single Location or an array; decode the raw result once.
	var raw json.RawMessage
	if err := c.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	return parseLocations(raw), nil
}

// parseLocations decodes an LSP location result, which is either a single
// Location, an array of Locations, or null.
func parseLocations(raw json.RawMessage) []Location {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []Location
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	var one Location
	if json.Unmarshal(raw, &one) == nil && one.URI != "" {
		return []Location{one}
	}
	return nil
}
