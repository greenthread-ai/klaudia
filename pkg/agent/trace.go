package agent

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracing follows the OpenTelemetry GenAI semantic conventions: an
// invoke_agent span per Run (a sub-agent's Run nests inside its parent's
// execute_tool span, because the context carries it), a chat span per model
// request, and an execute_tool span per tool call, with token usage and
// finish reasons as attributes.
//
// The conventions are still in development and their names move between
// releases, so the version written against is pinned here and in the schema
// URL; change both together. Only the OTel API is imported: with no provider
// installed every span is a no-op, and an embedder chooses the exporter (the
// Klaudia CLI installs OTLP when OTEL_EXPORTER_OTLP_* is set).
const genAISemconv = "1.37.0"

// tracer is looked up from the global provider at each span, not once at
// package init. A tracer taken before any provider is set is a delegate that
// binds to the first provider installed and never follows a later one, so an
// embedder (or a test) that replaces the provider would keep tracing into the
// old one.
func tracer() trace.Tracer {
	return otel.Tracer("github.com/greenthread-ai/klaudia/pkg/agent",
		trace.WithSchemaURL("https://opentelemetry.io/schemas/"+genAISemconv))
}

// Attribute names, from the GenAI conventions at genAISemconv.
const (
	attrOperation     = "gen_ai.operation.name"
	attrAgentName     = "gen_ai.agent.name"
	attrRequestModel  = "gen_ai.request.model"
	attrMaxTokens     = "gen_ai.request.max_tokens"
	attrResponseModel = "gen_ai.response.model"
	attrResponseID    = "gen_ai.response.id"
	attrFinish        = "gen_ai.response.finish_reasons"
	attrInputTokens   = "gen_ai.usage.input_tokens"
	attrOutputTokens  = "gen_ai.usage.output_tokens"
	attrCacheRead     = "gen_ai.usage.cache_read.input_tokens"
	attrCacheCreation = "gen_ai.usage.cache_creation.input_tokens"
	attrToolName      = "gen_ai.tool.name"
	attrToolCallID    = "gen_ai.tool.call.id"
	attrErrorType     = "error.type"
)

// Run executes one user turn: it sends the prompt, dispatches the tools the
// model asks for, and repeats until the model stops, inside an invoke_agent
// span.
func (l *Loop) Run(ctx context.Context, opts Options, emit Emitter) (Result, error) {
	name := "klaudia"
	if opts.SubAgent {
		name = "klaudia sub-agent"
	}
	ctx, span := tracer().Start(ctx, "invoke_agent "+name, trace.WithAttributes(
		attribute.String(attrOperation, "invoke_agent"),
		attribute.String(attrAgentName, name),
		attribute.String(attrRequestModel, string(opts.Model)),
	))
	defer span.End()
	res, err := l.run(ctx, opts, emit)
	span.SetAttributes(
		attribute.Int64(attrInputTokens, res.InputTokens),
		attribute.Int64(attrOutputTokens, res.OutputTokens),
		attribute.Int("klaudia.turns", res.NumTurns),
	)
	if res.StopReason != "" {
		span.SetAttributes(attribute.StringSlice(attrFinish, []string{res.StopReason}))
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.String(attrErrorType, fmt.Sprintf("%T", err)))
	}
	return res, err
}

// startChatSpan opens the span for one model request.
func startChatSpan(ctx context.Context, params anthropic.BetaMessageNewParams) (context.Context, trace.Span) {
	return tracer().Start(ctx, "chat "+string(params.Model), trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String(attrOperation, "chat"),
		attribute.String(attrRequestModel, string(params.Model)),
		attribute.Int64(attrMaxTokens, params.MaxTokens),
	))
}

// endChatSpan records the response — or the error — and closes the span.
func endChatSpan(span trace.Span, m anthropic.BetaMessage, err error) {
	defer span.End()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.String(attrErrorType, fmt.Sprintf("%T", err)))
		return
	}
	span.SetAttributes(
		attribute.String(attrResponseModel, string(m.Model)),
		attribute.String(attrResponseID, m.ID),
		attribute.StringSlice(attrFinish, []string{string(m.StopReason)}),
		attribute.Int64(attrInputTokens, m.Usage.InputTokens),
		attribute.Int64(attrOutputTokens, m.Usage.OutputTokens),
		attribute.Int64(attrCacheRead, m.Usage.CacheReadInputTokens),
		attribute.Int64(attrCacheCreation, m.Usage.CacheCreationInputTokens),
	)
}

// dispatch runs one tool call inside an execute_tool span. The span is the
// context the tool runs in, so an MCP call can pass the trace on and a
// sub-agent's invoke_agent nests under it.
func (l *Loop) dispatch(ctx context.Context, tu anthropic.BetaToolUseBlock, opts Options, emit Emitter, reveal func(...string), fs *failureState) anthropic.BetaContentBlockParamUnion {
	ctx, span := tracer().Start(ctx, "execute_tool "+tu.Name, trace.WithAttributes(
		attribute.String(attrOperation, "execute_tool"),
		attribute.String(attrToolName, tu.Name),
		attribute.String(attrToolCallID, tu.ID),
	))
	defer span.End()
	block := l.dispatchCall(ctx, tu, opts, emit, reveal, fs)
	if tr := block.OfToolResult; tr != nil && tr.IsError.Valid() && tr.IsError.Value {
		span.SetStatus(codes.Error, "tool reported an error")
		span.SetAttributes(attribute.String(attrErrorType, "tool_error"))
	}
	return block
}
