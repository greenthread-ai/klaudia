package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// anthropicStatusErr builds an *anthropic.Error with the given status and
// body, the way the SDK does from a response.
func anthropicStatusErr(t *testing.T, status int, body string) *anthropic.Error {
	t.Helper()
	var e anthropic.Error
	if err := e.UnmarshalJSON([]byte(body)); err != nil {
		t.Fatalf("unmarshal anthropic.Error: %v", err)
	}
	e.StatusCode = status
	return &e
}

const anthropicModelNotFound = `{"type":"error","error":{"type":"not_found_error","message":"model: claude-sonet-5"}}`

const openAIModelNotFound = `{"error":{"message":"The model 'gpt-9' does not exist or you do not have access to it.","type":"invalid_request_error","code":"model_not_found"}}`

func TestIsModelNotFoundShapes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"anthropic 404", anthropicStatusErr(t, 404, anthropicModelNotFound), true},
		{"openai model_not_found code", &OpenAIError{StatusCode: 404, Body: openAIModelNotFound}, true},
		{"vllm 404", &OpenAIError{StatusCode: 404, Body: `{"object":"error","message":"The model ` + "`llama-9`" + ` does not exist.","type":"NotFoundError","code":404}`}, true},
		{"plain-text 404", &OpenAIError{StatusCode: 404, Body: "model not found"}, true},
		{"proxy 400", &OpenAIError{StatusCode: 400, Body: `{"error":{"message":"Invalid model name passed in model=foo","type":"invalid_request_error"}}`}, true},
		{"wrong path 404", &OpenAIError{StatusCode: 404, Body: "404 page not found"}, false},
		{"unrelated 400", anthropicStatusErr(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: must be at least 1"}}`), false},
		{"annotated", &requestError{model: "gpt-9", endpoint: "https://x/v1", err: &OpenAIError{StatusCode: 404, Body: openAIModelNotFound}}, true},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := IsModelNotFound(c.err); got != c.want {
			t.Errorf("%s: IsModelNotFound = %v, want %v", c.name, got, c.want)
		}
	}
}

// The reported case: a typo'd model on an OpenAI-compatible endpoint used to
// surface as `openai endpoint 404: {"error":…model_not_found}`.
func TestFriendlyErrorModelNotFoundNamesModelAndEndpoint(t *testing.T) {
	err := annotateNotFound(&OpenAIError{StatusCode: 404, Body: openAIModelNotFound}, "gpt-9", "https://llm.example/v1")
	got := FriendlyError(err)
	for _, want := range []string{"Model not found", "gpt-9 isn't served by https://llm.example/v1", "/model", "--model", "does not exist"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in: %q", want, got)
		}
	}
	if strings.Contains(got, "openai endpoint 404") || strings.Contains(got, `"code"`) {
		t.Errorf("raw error body leaked into the friendly message: %q", got)
	}
}

// Without the annotation (an error that did not come through a provider's
// StreamTurn) the model is still read from Anthropic's "model: <id>" body.
func TestFriendlyErrorAnthropicModelNotFoundWithoutAnnotation(t *testing.T) {
	got := FriendlyError(anthropicStatusErr(t, 404, anthropicModelNotFound))
	for _, want := range []string{"Model not found", "claude-sonet-5 isn't served by this endpoint", "/model", "--model"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in: %q", want, got)
		}
	}
}

// A 404 that is not about a model is a wrong path, not a wrong model: point at
// the baseURL rather than /model.
func TestFriendlyError404WrongPathPointsAtBaseURL(t *testing.T) {
	err := annotateNotFound(&OpenAIError{StatusCode: 404, Body: "404 page not found"}, "gpt-4o", "https://llm.example")
	got := FriendlyError(err)
	for _, want := range []string{"Not found (404) at https://llm.example", "404 page not found", "baseURL", "/v1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in: %q", want, got)
		}
	}
	if strings.Contains(got, "/model") {
		t.Errorf("a wrong-path 404 should not suggest changing the model: %q", got)
	}
}

// A 400 counts as model-not-found in the message only with the explicit
// model_not_found code; a 400 that merely mentions a model keeps the Bad
// request wording, which quotes the provider.
func TestFriendlyError400ModelNotFoundNeedsTheCode(t *testing.T) {
	coded := FriendlyError(&OpenAIError{StatusCode: 400, Body: openAIModelNotFound})
	if !strings.Contains(coded, "Model not found") {
		t.Errorf("a 400 with code model_not_found should read as model-not-found: %q", coded)
	}
	vague := FriendlyError(&OpenAIError{StatusCode: 400, Body: `{"error":{"message":"Invalid model name passed in model=foo","type":"invalid_request_error"}}`})
	if !strings.Contains(vague, "Bad request (400)") || !strings.Contains(vague, "Invalid model name") {
		t.Errorf("a vague 400 should stay a bad request quoting the provider: %q", vague)
	}
}

func TestAnnotateNotFoundLeavesOtherErrorsAlone(t *testing.T) {
	orig := &OpenAIError{StatusCode: 500, Body: "boom"}
	if got := annotateNotFound(orig, "m", "e"); got != error(orig) {
		t.Errorf("a 500 should pass through unwrapped, got %T", got)
	}
	if annotateNotFound(nil, "m", "e") != nil {
		t.Error("nil should stay nil")
	}
}

// End to end through the OpenAI-compatible provider: the error that reaches
// FriendlyError carries the request's model and the configured baseURL.
func TestOpenAIStreamTurnModelNotFoundIsAnnotated(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(openAIModelNotFound))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL+"/v1", "k", nil, nil)
	_, err := p.StreamTurn(context.Background(), anthropic.BetaMessageNewParams{
		Model:     "gpt-9",
		MaxTokens: 16,
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
	}, StreamSink{})
	if err == nil {
		t.Fatal("expected an error")
	}
	got := FriendlyError(err)
	if !strings.Contains(got, "gpt-9 isn't served by "+srv.URL+"/v1") {
		t.Errorf("friendly message should name the model and endpoint: %q", got)
	}
}

// End to end through the Anthropic client against a custom endpoint.
func TestAnthropicStreamTurnModelNotFoundIsAnnotated(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(anthropicModelNotFound))
	}))
	defer srv.Close()

	c := New(Credential{APIKey: "sk-fake"}, srv.URL)
	_, err := c.StreamTurn(context.Background(), anthropic.BetaMessageNewParams{
		Model:     "claude-sonet-5",
		MaxTokens: 16,
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
	}, StreamSink{})
	if err == nil {
		t.Fatal("expected an error")
	}
	got := FriendlyError(err)
	if !strings.Contains(got, "claude-sonet-5 isn't served by "+srv.URL) {
		t.Errorf("friendly message should name the model and endpoint: %q", got)
	}
}

func TestClientEndpointDefaultsToAnthropicAPI(t *testing.T) {
	if got := New(Credential{APIKey: "k"}, "").endpoint(); got != defaultAnthropicEndpoint {
		t.Errorf("endpoint() = %q, want %q", got, defaultAnthropicEndpoint)
	}
}
