// /v1/models fetcher. Pairs with ai-console's OpenAI-compatible
// catalog endpoint. Returns a flat slice for the TUI picker.

package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Model is a single entry from /v1/models.
type Model struct {
	ID            string   `json:"id"`
	Object        string   `json:"object,omitempty"`
	OwnedBy       string   `json:"owned_by,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length,omitempty"`
	Kind          string   `json:"kind,omitempty"` // "chat" / "tts" / "stt" / …
	Endpoints     []string `json:"supported_endpoints,omitempty"`
}

// ListModels fetches the catalog from ai-console. Returns only models
// whose Kind is "chat" (or unknown), sorted by id — that's what /model
// in klaudia is for. Other modalities are filtered out so the TUI
// picker doesn't show TTS voices.
func ListModels(ctx context.Context, cred *Credential) ([]Model, error) {
	if cred == nil || cred.BaseURL == "" || cred.Secret == "" {
		return nil, errors.New("remotecontrol: not signed in")
	}
	url := strings.TrimRight(cred.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.Secret)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: HTTP %s", resp.Status)
	}
	var body struct {
		Data []Model `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("list models: decode: %w", err)
	}
	out := body.Data[:0]
	for _, m := range body.Data {
		if m.Kind != "" && m.Kind != "chat" {
			continue
		}
		// Some catalogs publish endpoints instead of kind. ai-console
		// uses underscore-style values ("chat_completions", "messages"),
		// while other gateways use slashes — accept both.
		if m.Kind == "" && len(m.Endpoints) > 0 {
			ok := false
			for _, e := range m.Endpoints {
				switch {
				case e == "chat_completions", e == "messages", e == "responses":
					ok = true
				case strings.Contains(e, "chat/completions"),
					strings.HasSuffix(e, "/messages"):
					ok = true
				}
				if ok {
					break
				}
			}
			if !ok {
				continue
			}
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
