package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// The offline tables in client.go and effort.go are an allowlist, and an
// allowlist of model ids goes stale the day a model ships. When Claude 5.5
// arrived, every one of the three tables missed it at once and nothing said so:
// MaxOutputTokens quietly returned the 8192 floor instead of 128000 and
// ContextWindow returned "unknown", so a 1M model compacted as if it had 200k.
// Both failures look like the model behaving oddly, not like a stale map.
//
// This test is the thing that would have caught it. It is network- and
// credential-dependent, so it is opt-in rather than part of `go test ./...`:
//
//	KLAUDIA_LIVE_MODELS=1 go test ./internal/api -run TestLiveModelTablesMatchProvider -v
//
// Run it when bumping the tables, and it reports what to write rather than just
// that something is wrong.
const liveModelsEnv = "KLAUDIA_LIVE_MODELS"

func TestLiveModelTablesMatchProvider(t *testing.T) {
	if os.Getenv(liveModelsEnv) == "" {
		t.Skipf("set %s=1 to check the model tables against GET /v1/models", liveModelsEnv)
	}
	cred, err := ResolveCredential()
	if err != nil {
		t.Skipf("no Anthropic credential: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := New(cred, "").ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("the endpoint reported no models — nothing to check the tables against")
	}
	for _, m := range models {
		// Only Claude ids belong in these tables; a workspace may serve others.
		if !strings.HasPrefix(m.ID, "claude-") {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			// A served model missing from the tables is the actual defect: it
			// is not an error anywhere, it just silently gets the floor.
			if m.MaxTokens > 0 {
				got, ok := modelMaxOutputTokens[m.ID]
				switch {
				case !ok:
					t.Errorf("not in modelMaxOutputTokens — MaxOutputTokens returns the %d floor instead of %d; add %q: %d",
						DefaultMaxOutputTokens, m.MaxTokens, m.ID, m.MaxTokens)
				case got != m.MaxTokens:
					t.Errorf("modelMaxOutputTokens = %d, endpoint says %d", got, m.MaxTokens)
				}
			}
			if m.ContextWindow > 0 {
				got, ok := modelContextWindows[m.ID]
				switch {
				case !ok:
					t.Errorf("not in modelContextWindows — ContextWindow reports %q instead of %d; add %q: %d",
						ContextSourceUnknown, m.ContextWindow, m.ID, m.ContextWindow)
				case got != m.ContextWindow:
					t.Errorf("modelContextWindows = %d, endpoint says %d", got, m.ContextWindow)
				}
			}
			// The effort ceiling cannot be read off /v1/models — it is not
			// reported — so this only checks the entry exists. An absent model
			// fails open (EffortSupported treats unknown as capable), which is
			// safe but means /model can no longer lower a stranded level.
			if _, ok := modelMaxEffort[m.ID]; !ok {
				t.Errorf("not in modelMaxEffort — probe it with output_config.effort and add the ceiling")
			}
		})
	}
}

// The retry in the agent loop depends on the 400's wording: it reads the real
// cap out of the message. A reword silently turns the recovery off, so measure
// it rather than trusting the comment on maxTokensAnthropicRE.
//
// Sonnet 4.5 is the probe because it is a 64k-output model, so asking for 128k
// is over its cap but under the lineup's maximum — the refusal is about this
// model's limit, not about an impossible number. The request is refused before
// any generation, so it costs nothing.
func TestLiveMaxTokensTooLargeWording(t *testing.T) {
	if os.Getenv(liveModelsEnv) == "" {
		t.Skipf("set %s=1 to re-measure the over-cap 400", liveModelsEnv)
	}
	cred, err := ResolveCredential()
	if err != nil {
		t.Skipf("no Anthropic credential: %v", err)
	}
	const (
		model = "claude-sonnet-4-5-20250929"
		want  = 64000
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	params := anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: int64(want * 2),
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
	}
	_, err = New(cred, "").StreamTurn(ctx, params, StreamSink{})
	if err == nil {
		t.Fatalf("max_tokens=%d on %s was accepted — the cap moved; re-check modelMaxOutputTokens", want*2, model)
	}
	limit, ok := MaxTokensTooLarge(err)
	if !ok {
		t.Fatalf("MaxTokensTooLarge did not recognise the refusal, so the loop can no longer recover from it.\nerror: %v", err)
	}
	if limit != want {
		t.Errorf("the 400 names %d as the cap, modelMaxOutputTokens says %d", limit, want)
	}
}
