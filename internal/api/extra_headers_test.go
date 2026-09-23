package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A header-authenticated endpoint (e.g. Cloudflare Access) has no bearer: only the extra
// headers are sent, and no Authorization header.
func TestOpenAIExtraHeadersNoBearer(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{}})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL+"/v1", "", nil, map[string]string{
		"CF-Access-Client-Id":     "id-123",
		"CF-Access-Client-Secret": "secret-456",
	})
	if _, err := p.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.Get("Authorization") != "" {
		t.Errorf("Authorization = %q, want empty (no bearer configured)", got.Get("Authorization"))
	}
	if got.Get("CF-Access-Client-Id") != "id-123" || got.Get("CF-Access-Client-Secret") != "secret-456" {
		t.Errorf("extra headers missing: id=%q secret=%q", got.Get("CF-Access-Client-Id"), got.Get("CF-Access-Client-Secret"))
	}
}

// A bearer endpoint fronted by CF Access: all three headers are present.
func TestOpenAIExtraHeadersWithBearer(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{}})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL+"/v1", "k", nil, map[string]string{"CF-Access-Client-Id": "id-123"})
	if _, err := p.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.Get("Authorization") != "Bearer k" {
		t.Errorf("Authorization = %q, want Bearer k", got.Get("Authorization"))
	}
	if got.Get("CF-Access-Client-Id") != "id-123" {
		t.Errorf("CF-Access-Client-Id = %q, want id-123", got.Get("CF-Access-Client-Id"))
	}
}
