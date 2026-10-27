package cli

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// No OTEL_EXPORTER_OTLP_* means no exporter and no SDK provider; an endpoint
// installs one, and OTEL_SDK_DISABLED wins over it.
func TestSetupTracingFollowsTheStandardEnvironment(t *testing.T) {
	prev := otel.GetTracerProvider()
	defer otel.SetTracerProvider(prev)
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }

	setupTracing(context.Background(), env(nil))()
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		t.Fatal("installed a provider with no endpoint configured")
	}
	setupTracing(context.Background(), env(map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318", "OTEL_SDK_DISABLED": "true",
	}))()
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		t.Fatal("OTEL_SDK_DISABLED was ignored")
	}
	stop := setupTracing(context.Background(), env(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318"}))
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Error("no provider with an endpoint configured")
	}
	stop()
}
