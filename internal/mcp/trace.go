package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// traceMeta carries the caller's trace into an MCP request: W3C traceparent
// (and tracestate) in the request's _meta, where the MCP tracing proposal
// and servers that trace look for them, so a tool call's work on the server
// joins the agent's trace. Nil — no _meta at all — when nothing is being
// traced, so an untraced session sends exactly what it always did.
func traceMeta(ctx context.Context) mcpsdk.Meta {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return nil
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	if len(carrier) == 0 {
		return nil
	}
	meta := mcpsdk.Meta{}
	for k, v := range carrier {
		meta[k] = v
	}
	return meta
}
