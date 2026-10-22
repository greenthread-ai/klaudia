package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ModelCacheMaxAge is how long a fetched model list counts as current.
//
// A day, because the list changes when a model ships and models ship on the
// order of weeks; and because fetching it costs about 300ms (measured: four
// calls, 301–335ms), which is fine once a day and too much to put on every
// startup.
const ModelCacheMaxAge = 24 * time.Hour

// ModelCache is the provider's model list, kept on disk so that what the
// provider says about a model can beat the offline tables without a network
// call on every start.
//
// The tables are allowlists of model ids with figures attached, and a wrong
// figure in them was never contradicted at runtime: the provider was only asked
// about models the table did not know, so the claude-sonnet-4-5 entry claimed a
// 1M window for months while the API said 200k. With the list cached, a fresh
// provider answer wins for every model it lists.
//
// It is also a ModelLister: a ListModels call goes to the network and, when it
// succeeds, rewrites the cache — so the /model picker keeps it current as a
// side effect.
type ModelCache struct {
	lister ModelLister
	path   string
	maxAge time.Duration
	now    func() time.Time
}

// NewModelCache caches lister's answers at path. An empty path is an in-memory
// pass-through: nothing is read or written and Cached never reports fresh.
func NewModelCache(lister ModelLister, path string) *ModelCache {
	return &ModelCache{lister: lister, path: path, maxAge: ModelCacheMaxAge, now: time.Now}
}

// ModelCachePath is where the list for one endpoint lives under root
// (~/.klaudia). Keyed by endpoint as well as provider: two OpenAI-compatible
// gateways serve different models under the same provider name, and one's list
// must not answer for the other.
func ModelCachePath(root, provider, endpoint string) string {
	if root == "" {
		return ""
	}
	if provider == "" {
		provider = "anthropic"
	}
	sum := sha256.Sum256([]byte(endpoint))
	return filepath.Join(root, "cache", "models-"+provider+"-"+hex.EncodeToString(sum[:4])+".json")
}

type modelCacheFile struct {
	FetchedAt time.Time          `json:"fetched_at"`
	Models    []modelCacheRecord `json:"models"`
}

type modelCacheRecord struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
	MaxTokens     int    `json:"max_tokens,omitempty"`
}

// Cached returns the stored list and whether it is fresh. A missing, unreadable
// or expired file is not fresh; the list is still returned when it parses, but
// callers treat only a fresh one as authoritative.
func (c *ModelCache) Cached() ([]ModelInfo, bool) {
	if c == nil || c.path == "" {
		return nil, false
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, false
	}
	var f modelCacheFile
	if json.Unmarshal(data, &f) != nil {
		return nil, false
	}
	models := make([]ModelInfo, 0, len(f.Models))
	for _, r := range f.Models {
		models = append(models, ModelInfo{ID: r.ID, DisplayName: r.DisplayName, ContextWindow: r.ContextWindow, MaxTokens: r.MaxTokens})
	}
	age := c.now().Sub(f.FetchedAt)
	return models, age >= 0 && age < c.maxAge
}

// FetchedAt reports when the stored list was fetched, and how many models it
// holds. ok is false when there is no readable cache.
func (c *ModelCache) FetchedAt() (at time.Time, models int, ok bool) {
	if c == nil || c.path == "" {
		return time.Time{}, 0, false
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return time.Time{}, 0, false
	}
	var f modelCacheFile
	if json.Unmarshal(data, &f) != nil {
		return time.Time{}, 0, false
	}
	return f.FetchedAt, len(f.Models), true
}

// Stale reports whether the cache should be refreshed.
func (c *ModelCache) Stale() bool {
	_, fresh := c.Cached()
	return !fresh
}

// ListModels asks the provider and, on success, rewrites the cache. A failed
// write is not an error: the caller asked for the list and has it.
func (c *ModelCache) ListModels(ctx context.Context) ([]ModelInfo, error) {
	models, err := c.lister.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	c.write(models)
	return models, nil
}

func (c *ModelCache) write(models []ModelInfo) {
	if c.path == "" {
		return
	}
	f := modelCacheFile{FetchedAt: c.now().UTC()}
	for _, m := range models {
		f.Models = append(f.Models, modelCacheRecord{ID: m.ID, DisplayName: m.DisplayName, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens})
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(c.path), 0o755) != nil {
		return
	}
	// Write-then-rename: the refresh runs in the background and the process
	// may exit mid-write, and a half-written file must never be read as a list.
	tmp := c.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, c.path)
}
