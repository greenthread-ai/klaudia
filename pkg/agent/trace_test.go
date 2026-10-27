package agent

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// A run with one tool call is an invoke_agent span holding two chat spans and
// an execute_tool span, named and attributed by the GenAI conventions.
func TestRunIsTraced(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	probe := &probeTool{name: "Write", log: &runLog{}}
	p := &scriptedProvider{turns: []anthropic.BetaMessage{toolTurn(t, "w1", "Write", "{}")}}
	if _, err := New(p, tools.NewRegistry(probe)).Run(context.Background(), Options{Model: "claude-sonnet-5-5", Prompt: "go"}, nil); err != nil {
		t.Fatal(err)
	}
	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range rec.Ended() {
		byName[s.Name()] = s
	}
	agentSpan, ok := byName["invoke_agent klaudia"]
	if !ok {
		t.Fatalf("no invoke_agent span; got %v", keysOf(byName))
	}
	tool, ok := byName["execute_tool Write"]
	if !ok {
		t.Fatalf("no execute_tool span; got %v", keysOf(byName))
	}
	if tool.Parent().SpanID() != agentSpan.SpanContext().SpanID() {
		t.Error("execute_tool is not a child of invoke_agent")
	}
	chat, ok := byName["chat claude-sonnet-5-5"]
	if !ok || chat.Parent().SpanID() != agentSpan.SpanContext().SpanID() {
		t.Fatalf("chat span missing or misparented; got %v", keysOf(byName))
	}
	if !hasAttr(chat.Attributes(), "gen_ai.operation.name", "chat") || !hasAttr(tool.Attributes(), "gen_ai.tool.call.id", "w1") {
		t.Errorf("attributes: chat %v, tool %v", chat.Attributes(), tool.Attributes())
	}
}

func keysOf(m map[string]sdktrace.ReadOnlySpan) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasAttr(kvs []attribute.KeyValue, key, want string) bool {
	for _, kv := range kvs {
		if string(kv.Key) == key && kv.Value.Emit() == want {
			return true
		}
	}
	return false
}
