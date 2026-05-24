package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRefreshOAuthSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want %s", r.Method, http.MethodPost)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyStr := string(body)
		if !strings.Contains(bodyStr, `"grant_type":"refresh_token"`) {
			t.Errorf("body missing grant_type: %s", bodyStr)
		}
		if !strings.Contains(bodyStr, `"client_id":"`+oauthClientID+`"`) {
			t.Errorf("body missing client_id: %s", bodyStr)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	oldURL := oauthTokenURL
	oauthTokenURL = server.URL
	defer func() { oauthTokenURL = oldURL }()

	before := time.Now().UnixMilli()
	tokens, err := refreshOAuth(context.Background(), "old-refresh")
	after := time.Now().UnixMilli()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want new-access", tokens.AccessToken)
	}
	if tokens.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q, want new-refresh", tokens.RefreshToken)
	}
	wantMin := before + 3600*1000
	wantMax := after + 3600*1000 + 3*1000
	if tokens.ExpiresAt < wantMin || tokens.ExpiresAt > wantMax {
		t.Errorf("ExpiresAt = %d, want between %d and %d", tokens.ExpiresAt, wantMin, wantMax)
	}
}

func TestRefreshOAuthFallsBackToInputRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","expires_in":3600}`))
	}))
	defer server.Close()

	oldURL := oauthTokenURL
	oauthTokenURL = server.URL
	defer func() { oauthTokenURL = oldURL }()

	tokens, err := refreshOAuth(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.RefreshToken != "old-refresh" {
		t.Errorf("RefreshToken = %q, want old-refresh", tokens.RefreshToken)
	}
}

func TestRefreshOAuthNon200ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad refresh", http.StatusBadRequest)
	}))
	defer server.Close()

	oldURL := oauthTokenURL
	oauthTokenURL = server.URL
	defer func() { oauthTokenURL = oldURL }()

	_, err := refreshOAuth(context.Background(), "old-refresh")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want status 400", err.Error())
	}
}
