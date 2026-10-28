package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func withThinkingHistory() anthropic.BetaMessageNewParams {
	p := testParams()
	p.Messages = append(p.Messages,
		anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaThinkingBlock("sig", ""), anthropic.NewBetaTextBlock("ok"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("next")))
	return p
}

// bodyServer answers with a complete stream whose message_start carries
// transformations, and records every request body; reject makes the first
// request a 400 naming block_binding, as an endpoint without the beta would.
func bodyServer(t *testing.T, transformations string, reject bool) (*Client, *[]string) {
	t.Helper()
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		if reject && n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.adaptive.block_binding: Extra inputs are not permitted"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		start := strings.Replace(sseStart, `"usage":`, `"input_transformations":`+transformations+`,"usage":`, 1)
		fmt.Fprint(w, start+sseText+sseEnd)
	}))
	t.Cleanup(srv.Close)
	return New(Credential{APIKey: "sk-fake"}, srv.URL, Tuning{}), &bodies
}

// Once the conversation holds a thinking block, the request states adaptive
// thinking with block_binding drop_block and the beta, and the drops the
// response lists are handed to the caller. A conversation with no thinking
// sends neither: stating adaptive would turn thinking on for a model whose
// default is off. (PR #63.)
func TestThinkingBindingIsSentOnlyWithThinkingInHistory(t *testing.T) {
	c, bodies := bodyServer(t, `[{"type":"thinking_dropped","reason":"prefix_binding_mismatch","path":"messages.1.content.0"}]`, false)
	var got []InputTransformation
	sink := StreamSink{OnTransformations: func(ts []InputTransformation) { got = ts }}
	if _, err := c.StreamTurn(context.Background(), withThinkingHistory(), sink); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StreamTurn(context.Background(), testParams(), StreamSink{}); err != nil {
		t.Fatal(err)
	}
	var first struct {
		Thinking map[string]any `json:"thinking"`
	}
	json.Unmarshal([]byte((*bodies)[0]), &first)
	bb, _ := first.Thinking["block_binding"].(map[string]any)
	if first.Thinking["type"] != "adaptive" || bb["prefix_mismatch_behavior"] != "drop_block" {
		t.Errorf("thinking sent = %v, want adaptive with block_binding drop_block", first.Thinking)
	}
	if len(got) != 1 || got[0].Reason != "prefix_binding_mismatch" || got[0].Path != "messages.1.content.0" {
		t.Errorf("transformations = %+v", got)
	}
	if strings.Contains((*bodies)[1], "block_binding") || strings.Contains((*bodies)[1], `"thinking"`) {
		t.Errorf("a conversation with no thinking sent thinking config: %s", (*bodies)[1])
	}
}

// An endpoint that does not know the controls refuses them; the turn is sent
// again without, and the client stops trying.
func TestThinkingBindingFallsBackWhereRejected(t *testing.T) {
	c, bodies := bodyServer(t, `[]`, true)
	for i := 0; i < 2; i++ {
		if _, err := c.StreamTurn(context.Background(), withThinkingHistory(), StreamSink{}); err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
	}
	if len(*bodies) != 3 || strings.Contains((*bodies)[1], "block_binding") || strings.Contains((*bodies)[2], "block_binding") {
		t.Errorf("requests = %d; want the rejected one, its resend and the next turn, both without block_binding", len(*bodies))
	}
}

func TestIsThinkingBindingError(t *testing.T) {
	var e anthropic.Error
	e.UnmarshalJSON([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"messages.5.content.0: Invalid ` + "`signature`" + ` in ` + "`thinking`" + ` block. The block is bound to a different conversation. Remove the block, or set ..."}}`))
	e.StatusCode = 400
	if !IsThinkingBindingError(&e) || isBindingRejected(&e) {
		t.Error("a binding refusal is not recognised as one")
	}
	if IsThinkingBindingError(fmt.Errorf("overloaded")) {
		t.Error("an unrelated error matched")
	}
}
