package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	initializeTimeout = 30 * time.Second // servers like gopls index on init
	requestTimeout    = 15 * time.Second
)

// diagnosticsWait bounds the wait for the server to push diagnostics for an
// opened file. A variable so tests can shorten it.
var diagnosticsWait = 10 * time.Second

// ErrNoDiagnosticsReport means the server published nothing for the file
// within diagnosticsWait. It is not "no problems": a server still starting or
// indexing (a cold gopls or rust-analyzer) is silent, not clean.
var ErrNoDiagnosticsReport = errors.New("did not report diagnostics")

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
// (or a short timeout), and returns them. An empty slice with a nil error means
// the server reported no problems; if it reports nothing in time the error is
// ErrNoDiagnosticsReport, never an empty "clean" answer.
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
		c.dropDiagWaiter(uri, wait)
		// Whatever is stored predates this open (an earlier report, before
		// the latest edit hit disk), so it is not an answer either.
		return nil, fmt.Errorf("%s %w for %s within %s (it may still be starting or indexing the workspace); "+
			"this is not an all-clear — run Diagnostics again shortly",
			filepath.Base(c.cmd.Path), ErrNoDiagnosticsReport, path, diagnosticsWait)
	case <-ctx.Done():
		c.dropDiagWaiter(uri, wait)
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.diags[uri], nil
}

// dropDiagWaiter unregisters a waiter that gave up, so a server that never
// publishes for uri doesn't accumulate one per Diagnostics call.
func (c *Client) dropDiagWaiter(uri string, wait chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ws := c.diagCh[uri]
	for i, w := range ws {
		if w == wait {
			ws = append(ws[:i], ws[i+1:]...)
			break
		}
	}
	if len(ws) == 0 {
		delete(c.diagCh, uri)
	} else {
		c.diagCh[uri] = ws
	}
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
