package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seededCache writes models to a cache file fetched at `age` ago.
func seededCache(t *testing.T, lister ModelLister, age time.Duration, models []ModelInfo) *ModelCache {
	t.Helper()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	c := NewModelCache(lister, filepath.Join(t.TempDir(), "models.json"))
	c.now = func() time.Time { return now.Add(-age) }
	c.write(models)
	c.now = func() time.Time { return now }
	return c
}

// The table used to beat the provider for any model it listed, so a wrong
// entry (claude-sonnet-4-5: 1M in the table, 200k from the API) was never
// contradicted. A provider answer must now win whenever one is in hand without
// a network call — the picker's figures or a fresh cache — and a stale cache
// must cost a known model no startup latency.
func TestResolvePrefersTheProviderOverTheTable(t *testing.T) {
	const model = "claude-opus-5-5" // in the table: 128000 out, 1M window
	provider := []ModelInfo{{ID: model, MaxTokens: 64000, ContextWindow: 200_000}}

	tests := []struct {
		name          string
		in            func(t *testing.T, live *fakeLister) LimitsInput
		wantMax       int
		wantMaxSource string
		wantCtx       int
		wantTableMax  int
		wantCalls     int
	}{
		{
			name: "figures the caller already holds beat the table",
			in: func(t *testing.T, live *fakeLister) LimitsInput {
				return LimitsInput{Model: model, Lister: live, Known: provider[0]}
			},
			wantMax: 64000, wantMaxSource: LimitSourceProvider, wantCtx: 200_000, wantTableMax: 128000,
		},
		{
			name: "a fresh cache beats the table without a network call",
			in: func(t *testing.T, live *fakeLister) LimitsInput {
				return LimitsInput{Model: model, Lister: seededCache(t, live, time.Hour, provider)}
			},
			wantMax: 64000, wantMaxSource: LimitSourceProvider, wantCtx: 200_000, wantTableMax: 128000,
		},
		{
			name: "a stale cache leaves a known model on the table, still offline",
			in: func(t *testing.T, live *fakeLister) LimitsInput {
				return LimitsInput{Model: model, Lister: seededCache(t, live, 48*time.Hour, provider)}
			},
			wantMax: 128000, wantMaxSource: LimitSourceModel, wantCtx: 1_000_000,
		},
		{
			name: "config still beats a provider answer",
			in: func(t *testing.T, live *fakeLister) LimitsInput {
				return LimitsInput{Model: model, Known: provider[0], ConfigMaxTokens: 4096}
			},
			wantMax: 4096, wantMaxSource: LimitSourceConfig, wantCtx: 200_000, wantTableMax: 128000,
		},
		{
			name: "an unknown model on a stale cache asks the network",
			in: func(t *testing.T, live *fakeLister) LimitsInput {
				live.models = []ModelInfo{{ID: "claude-new-6", MaxTokens: 99000}}
				return LimitsInput{Model: "claude-new-6", Lister: seededCache(t, live, 48*time.Hour, nil)}
			},
			wantMax: 99000, wantMaxSource: LimitSourceProvider, wantCalls: 1,
		},
		{
			// /model runs on the UI goroutine: offline, a lookup would freeze
			// the TUI for the 5s timeout.
			name: "NoNetwork never calls out",
			in: func(t *testing.T, live *fakeLister) LimitsInput {
				return LimitsInput{Model: "claude-new-6", Lister: live, NoNetwork: true}
			},
			wantMax: DefaultMaxOutputTokens, wantMaxSource: LimitSourceFallback,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			live := &fakeLister{}
			got := Resolve(context.Background(), tc.in(t, live))
			if got.MaxOutputTokens != tc.wantMax || got.MaxOutputSource != tc.wantMaxSource {
				t.Errorf("output = (%d, %q), want (%d, %q)", got.MaxOutputTokens, got.MaxOutputSource, tc.wantMax, tc.wantMaxSource)
			}
			if got.ContextWindow != tc.wantCtx {
				t.Errorf("context = %d, want %d", got.ContextWindow, tc.wantCtx)
			}
			if got.TableMaxOutputTokens != tc.wantTableMax {
				t.Errorf("table disagreement = %d, want %d", got.TableMaxOutputTokens, tc.wantTableMax)
			}
			if live.calls != tc.wantCalls {
				t.Errorf("network calls = %d, want %d", live.calls, tc.wantCalls)
			}
		})
	}
}

func TestModelCache(t *testing.T) {
	t.Run("a live call writes the cache and it reads back fresh", func(t *testing.T) {
		live := &fakeLister{models: []ModelInfo{{ID: "m", DisplayName: "M", ContextWindow: 1, MaxTokens: 2}}}
		c := NewModelCache(live, filepath.Join(t.TempDir(), "sub", "models.json"))
		if !c.Stale() {
			t.Fatal("a missing cache must be stale")
		}
		if _, err := c.ListModels(context.Background()); err != nil {
			t.Fatal(err)
		}
		got, fresh := c.Cached()
		if !fresh || len(got) != 1 || got[0] != live.models[0] {
			t.Errorf("Cached() = %+v, %v", got, fresh)
		}
	})
	t.Run("a failed call leaves the cache alone", func(t *testing.T) {
		c := seededCache(t, &fakeLister{err: os.ErrDeadlineExceeded}, time.Hour, []ModelInfo{{ID: "kept"}})
		if _, err := c.ListModels(context.Background()); err == nil {
			t.Fatal("want the error")
		}
		if got, fresh := c.Cached(); !fresh || got[0].ID != "kept" {
			t.Errorf("cache changed: %+v %v", got, fresh)
		}
	})
	t.Run("a corrupt file is stale", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "models.json")
		_ = os.WriteFile(p, []byte("{not json"), 0o644)
		if !NewModelCache(&fakeLister{}, p).Stale() {
			t.Error("a corrupt cache read as fresh")
		}
	})
	t.Run("the path is keyed by endpoint", func(t *testing.T) {
		a := ModelCachePath("/r", "openai", "https://one.example/v1")
		b := ModelCachePath("/r", "openai", "https://two.example/v1")
		if a == b {
			t.Errorf("two gateways share %s", a)
		}
		if ModelCachePath("", "openai", "x") != "" {
			t.Error("no root must mean no cache")
		}
	})
}

func TestModelCacheFetchedAt(t *testing.T) {
	if _, _, ok := NewModelCache(nil, filepath.Join(t.TempDir(), "none.json")).FetchedAt(); ok {
		t.Error("a missing cache reported a fetch time")
	}
	c := seededCache(t, nil, 3*time.Hour, []ModelInfo{{ID: "a"}, {ID: "b"}})
	at, n, ok := c.FetchedAt()
	if !ok || n != 2 || c.now().Sub(at) != 3*time.Hour {
		t.Errorf("FetchedAt() = %v, %d, %v; want 3h ago, 2 models", at, n, ok)
	}
}
