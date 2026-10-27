package cli

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/greenthread-ai/klaudia/internal/version"
)

// setupTracing installs an OTLP/HTTP trace exporter when the standard
// OpenTelemetry environment asks for one — OTEL_EXPORTER_OTLP_ENDPOINT or
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT set, and OTEL_SDK_DISABLED not "true" —
// and returns the function that flushes it on exit. Otherwise it installs
// nothing, the loop's spans stay no-ops, and the function does nothing.
//
// The exporter reads the rest of its configuration (headers, protocol
// options, timeouts) from the same variables, so Klaudia adds no settings of
// its own: whatever already points other OTel-instrumented tools at a
// collector points Klaudia there too.
func setupTracing(ctx context.Context, getenv func(string) string) func() {
	noop := func() {}
	if strings.EqualFold(getenv("OTEL_SDK_DISABLED"), "true") {
		return noop
	}
	if getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return noop
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop // tracing is never a reason not to run
	}
	name := getenv("OTEL_SERVICE_NAME")
	if name == "" {
		name = "klaudia"
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", name),
			attribute.String("service.version", version.Version),
		)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return func() {
		// Bounded: an unreachable collector must not hold the terminal.
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(flush)
	}
}
