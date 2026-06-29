package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// TestDumpOutgoingHeaders is a debugging helper, not an assertion. Run with
//
//	go test ./internal/api/... -run TestDumpOutgoingHeaders -v
//
// to see what we put on the wire for OAuth vs API-key clients. Headers turned
// out NOT to be the OAuth rate-limit discriminator — the gate is the body's
// system[0] prefix (see augmentSystem) — so this is now mostly useful for
// confirming we haven't accidentally regressed back to a heavy header set, or
// for debugging future provider quirks.
func TestDumpOutgoingHeaders(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")

	var captured http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"sink"}}`, 500)
	}))
	defer server.Close()

	for _, tc := range []struct {
		label string
		cred  Credential
	}{
		{"OAUTH", Credential{AuthToken: "oauth-fake"}},
		{"API-KEY", Credential{APIKey: "sk-fake"}},
	} {
		captured = nil
		c := New(tc.cred, server.URL)
		params := anthropic.BetaMessageNewParams{
			Model:     anthropic.Model(DefaultModel),
			MaxTokens: 16,
			Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
			Betas:     c.augmentBetas(DefaultBetas),
		}
		_, _ = c.StreamTurn(context.Background(), params, StreamSink{})

		fmt.Printf("=== %s ===\n", tc.label)
		keys := make([]string, 0, len(captured))
		for k := range captured {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, v := range captured[k] {
				fmt.Printf("%s: %s\n", k, v)
			}
		}
		fmt.Println()
	}
}
