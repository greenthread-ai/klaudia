package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// revealParams is a conversation with one eager and one deferred tool, where
// a tool-result message has revealed the deferred one.
func revealParams() anthropic.BetaMessageNewParams {
	schema := anthropic.BetaToolInputSchemaParam{Properties: map[string]any{}}
	return anthropic.BetaMessageNewParams{
		Model:     "claude-opus-5-5",
		MaxTokens: 100,
		Tools: []anthropic.BetaToolUnionParam{
			{OfTool: &anthropic.BetaToolParam{Name: "ToolSearch", InputSchema: schema}},
			{OfTool: &anthropic.BetaToolParam{Name: "Later", InputSchema: schema, DeferLoading: anthropic.Bool(true)}},
			{OfTool: &anthropic.BetaToolParam{Name: "Hidden", InputSchema: schema, DeferLoading: anthropic.Bool(true)}},
		},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("go")),
			assistantUse("t1"),
			anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("t1", "Loaded 1 tool(s)", false), RevealBlock("Later")),
		},
	}
}

func assistantUse(id string) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaToolUseBlock(id, map[string]any{}, "ToolSearch")}}
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func toolsSent(p anthropic.BetaMessageNewParams) map[string]bool {
	out := map[string]bool{}
	for _, t := range p.Tools {
		out[t.OfTool.Name] = t.OfTool.DeferLoading.Value
	}
	return out
}

// By addition: the tools array is untouched and the marker becomes a
// tool_addition system message right after the results it rode in.
func TestRevealByAddition(t *testing.T) {
	p := revealParams()
	if !shapeReveals(&p, revealByAddition) {
		t.Fatal("no tool_addition added")
	}
	if got := toolsSent(p); len(got) != 3 || !got["Later"] || !got["Hidden"] {
		t.Errorf("tools = %v, want all three with the deferred ones still deferred", got)
	}
	body := marshal(t, p.Messages)
	if strings.Contains(body, revealPrefix) {
		t.Errorf("a marker was sent: %s", body)
	}
	if len(p.Messages) != 4 || !strings.HasSuffix(body, `{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_reference","name":"Later"}}]}]`) {
		t.Errorf("messages = %s", body)
	}
	if !strings.Contains(marshal(t, p.Betas), ToolChangesBeta) {
		t.Errorf("betas = %v", p.Betas)
	}
	// The caller's history keeps its marker for the next request.
	if !RevealedTools(revealParams().Messages)["Later"] {
		t.Error("RevealedTools lost the marker")
	}
}

// Each request is the previous one with messages appended, system messages
// included: a later turn re-reveals nothing, and an earlier reveal stays put.
func TestRevealByAdditionIsAppendOnly(t *testing.T) {
	first := revealParams()
	second := revealParams()
	second.Messages = append(second.Messages,
		assistantUse("t2"),
		anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("t2", "again", false), RevealBlock("Later"), RevealBlock("Missing")))
	shapeReveals(&first, revealByAddition)
	shapeReveals(&second, revealByAddition)
	a, b := marshal(t, first.Messages), marshal(t, second.Messages)
	if !strings.HasPrefix(b, strings.TrimSuffix(a, "]")) {
		t.Errorf("second request does not extend the first:\n%s\n%s", a, b)
	}
	if strings.Count(b, "tool_addition") != 1 {
		t.Errorf("Later re-added, or an unknown tool added: %s", b)
	}
	if marshal(t, first.Tools) != marshal(t, second.Tools) {
		t.Error("the tools array changed")
	}
}

func TestRevealByFlagAndByOmission(t *testing.T) {
	p := revealParams()
	if shapeReveals(&p, revealByFlag) {
		t.Error("flag mode added a tool_addition")
	}
	if got := toolsSent(p); len(got) != 3 || got["Later"] || !got["Hidden"] {
		t.Errorf("flag tools = %v, want Later undeferred and Hidden still deferred", got)
	}
	o := revealParams()
	shapeReveals(&o, revealByOmission)
	if got := toolsSent(o); len(got) != 2 || got["Later"] {
		t.Errorf("omission tools = %v, want ToolSearch and Later, neither deferred", got)
	}
	if body := marshal(t, o.Messages); strings.Contains(body, revealPrefix) || strings.Contains(body, "system") {
		t.Errorf("omission messages = %s", body)
	}
	// Shaping copies: the caller's deferred tool is still deferred.
	q := revealParams()
	tools := q.Tools
	shapeReveals(&q, revealByOmission)
	if !tools[1].OfTool.DeferLoading.Value {
		t.Error("shaping changed the caller's tool")
	}
}

// The cache breakpoints skip a deferred tool (the API refuses one there) and
// a trailing tool_addition message, which has no blocks to carry one.
func TestCacheControlAroundReveals(t *testing.T) {
	p := revealParams()
	shapeReveals(&p, revealByAddition)
	applyCacheControl(&p)
	if p.Tools[2].OfTool.CacheControl.Type == "" && p.Tools[0].OfTool.CacheControl.Type == "" {
		t.Error("no tool breakpoint")
	}
	if p.Tools[1].OfTool.CacheControl.Type != "" || p.Tools[2].OfTool.CacheControl.Type != "" {
		t.Error("a deferred tool carries cache_control")
	}
	if !strings.Contains(marshal(t, p.Messages[2]), "cache_control") {
		t.Error("the rolling breakpoint did not fall back to the tool results")
	}
}

// revealServer refuses the first request with message, then answers.
func revealServer(t *testing.T, message string) (*Client, *[]string) {
	t.Helper()
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"type":"error","error":{"type":"invalid_request_error","message":%q}}`, message)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseStart+sseText+sseEnd)
	}))
	t.Cleanup(srv.Close)
	return New(Credential{APIKey: "sk-fake"}, srv.URL, Tuning{}), &bodies
}

// A model without mid-conversation tool changes (measured: Sonnet 5) refuses
// the tool_addition; the turn is resent with the tool undeferred, and that
// model is not asked again. Another model still is.
func TestRevealFallsBackForAModelWithoutToolChanges(t *testing.T) {
	c, bodies := revealServer(t, "tool_addition/tool_removal is not supported on this model")
	if _, err := c.StreamTurn(context.Background(), revealParams(), StreamSink{}); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 2 || strings.Contains((*bodies)[1], "tool_addition") {
		t.Fatalf("want the refused request and a resend without tool_addition; got %d", len(*bodies))
	}
	if c.revealMode("claude-opus-5-5") != revealByFlag || c.revealMode("claude-sonnet-5-5") != revealByAddition {
		t.Error("the refusal should stick to the model that made it")
	}
}

// An endpoint that refuses defer_loading gets requests shaped as they were
// before it: unrevealed deferred tools left out.
func TestRevealFallsBackWhereDeferLoadingIsRefused(t *testing.T) {
	c, bodies := revealServer(t, "tools.1.custom.defer_loading: Extra inputs are not permitted")
	if _, err := c.StreamTurn(context.Background(), revealParams(), StreamSink{}); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 2 || strings.Contains((*bodies)[1], "defer_loading") || strings.Contains((*bodies)[1], `"Hidden"`) {
		t.Fatalf("resend = %s", (*bodies)[len(*bodies)-1])
	}
}

// The OpenAI shim has no deferred tools: it sends the revealed ones and
// neither the rest nor the markers.
func TestOpenAIShimSendsOnlyRevealedTools(t *testing.T) {
	p := NewOpenAIProvider("http://unused", "k", nil, Tuning{})
	req, err := p.translateRequest(revealParams())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range req.Tools {
		names = append(names, tool.Function.Name)
	}
	if strings.Join(names, ",") != "ToolSearch,Later" {
		t.Errorf("tools = %v, want ToolSearch,Later", names)
	}
	if body := marshal(t, req.Messages); strings.Contains(body, revealPrefix) {
		t.Errorf("a marker reached the model: %s", body)
	}
}
