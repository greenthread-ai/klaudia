package anthropicauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
)

// Klaudia's client sends what it always has; the public pkg/api client does
// not present itself as Claude Code.
func TestKlaudiaShapingStaysOutOfThePublicClient(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"sink"}}`, 500)
	}))
	defer srv.Close()
	params := anthropic.BetaMessageNewParams{
		Model: anthropic.Model(api.DefaultModel), MaxTokens: 16,
		Messages: []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
	}
	cred := api.Credential{APIKey: "sk-fake"}

	_, _ = NewClient(cred, srv.URL, noRetries).StreamTurn(context.Background(), params, api.StreamSink{})
	if !strings.Contains(strings.Join(got.Values("Anthropic-Beta"), ","), string(claudeCodeBeta)) || got.Get("X-App") != "cli" {
		t.Errorf("Klaudia's client lost its shaping: beta=%v x-app=%q", got.Values("Anthropic-Beta"), got.Get("X-App"))
	}

	_, _ = api.New(cred, srv.URL, noRetries).StreamTurn(context.Background(), params, api.StreamSink{})
	if strings.Contains(strings.Join(got.Values("Anthropic-Beta"), ","), "claude-code") || got.Get("X-App") != "" {
		t.Errorf("pkg/api client identifies as Claude Code: beta=%v x-app=%q", got.Values("Anthropic-Beta"), got.Get("X-App"))
	}
}
