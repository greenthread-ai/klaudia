package agent

import "github.com/anthropics/anthropic-sdk-go"

// webToolParams returns the server-side tool definitions (web_search,
// web_fetch) appended to a request when WebTools is enabled. These are executed
// by the Anthropic API; the loop never dispatches them locally.
//
// We use the current GA tool versions (…20260318). allowed_callers is pinned to
// "direct" on purpose: from web_search_20260209 on, the default is to run the
// search from inside code execution ("dynamic filtering"), which (a) nests the
// results under a code_execution_tool_result with a caller field — a different
// response shape than the direct web_search_tool_result / web_search_result_location
// blocks the recorder and citation repair handle — and (b) 400s on models that
// don't support programmatic tool calling (pre-4.6) unless overridden. "direct"
// keeps the exact result/citation shape we already round-trip and works on every
// model. Enabling dynamic filtering is a deliberate follow-up: it needs
// code-execution result handling, a model-capability gate for allowed_callers,
// and a live smoke test.
func webToolParams() []anthropic.BetaToolUnionParam {
	return []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20260318: &anthropic.BetaWebSearchTool20260318Param{
			AllowedCallers: []string{"direct"},
		}},
		{OfWebFetchTool20260318: &anthropic.BetaWebFetchTool20260318Param{
			AllowedCallers: []string{"direct"},
		}},
	}
}
