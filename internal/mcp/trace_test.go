package mcp

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// A traced call carries W3C traceparent in _meta; an untraced one sends no
// _meta at all, exactly as before.
func TestTraceMetaCarriesTraceparent(t *testing.T) {
	if traceMeta(context.Background()) != nil {
		t.Error("an untraced call got _meta")
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	meta := traceMeta(trace.ContextWithSpanContext(context.Background(), sc))
	tp, _ := meta["traceparent"].(string)
	if !strings.HasPrefix(tp, "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01") {
		t.Errorf("traceparent = %q", tp)
	}
}
