package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
			{"id": "zeta-model"}, {"id": "alpha-model"}, {"id": ""},
		}})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL+"/v1", "test-key", nil)
	got, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Sorted, and the empty id dropped.
	if len(got) != 2 || got[0].ID != "alpha-model" || got[1].ID != "zeta-model" {
		t.Fatalf("got %+v, want alpha then zeta", got)
	}
	// OpenAI-compatible servers don't report a window; we must not invent one.
	if got[0].ContextWindow != 0 {
		t.Errorf("context window = %d, want 0 when the endpoint is silent", got[0].ContextWindow)
	}
}

func TestOpenAIListModelsReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := NewOpenAIProvider(srv.URL+"/v1", "k", nil).ListModels(context.Background()); err == nil {
		t.Fatal("a 401 should surface as an error, not an empty list")
	}
}

func TestModelInfoNameFallsBackToID(t *testing.T) {
	if got := (ModelInfo{ID: "x"}).Name(); got != "x" {
		t.Errorf("Name() = %q, want the id when there's no display name", got)
	}
	if got := (ModelInfo{ID: "x", DisplayName: "X Model"}).Name(); got != "X Model" {
		t.Errorf("Name() = %q", got)
	}
}

// Both providers must satisfy the optional interface, or /model silently
// degrades to type-the-id.
func TestProvidersImplementModelLister(t *testing.T) {
	var _ ModelLister = (*Client)(nil)
	var _ ModelLister = (*OpenAIProvider)(nil)
}

// Live check against the real endpoint. Skipped without credentials, so CI and
// contributors without a key are unaffected.
func TestListModelsLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live test")
	}
	cred, err := ResolveCredential()
	if err != nil {
		t.Skip("no credentials")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	models, err := New(cred, "").ListModels(ctx)
	if err != nil {
		t.Fatalf("live ListModels: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("the endpoint reported no models")
	}
	for _, m := range models {
		if m.ID == "" || m.DisplayName == "" || m.ContextWindow <= 0 {
			t.Errorf("incomplete model info: %+v", m)
		}
	}
	// The default must actually be servable by this account.
	var found bool
	for _, m := range models {
		if m.ID == DefaultModel {
			found = true
			if want, _ := ContextWindow(DefaultModel, 0); want != m.ContextWindow {
				t.Errorf("static table says %s has %d context, the API says %d",
					DefaultModel, want, m.ContextWindow)
			}
		}
	}
	if !found {
		t.Errorf("DefaultModel %q is not in the account's model list", DefaultModel)
	}
	t.Logf("%d models; newest is %s", len(models), models[0].ID)
}
