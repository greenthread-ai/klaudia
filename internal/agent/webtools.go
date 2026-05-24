package agent

import "github.com/anthropics/anthropic-sdk-go"

// webToolParams returns the server-side tool definitions (web_search,
// web_fetch) appended to a request when WebTools is enabled. These are executed
// by the Anthropic API; the loop never dispatches them locally.
func webToolParams() []anthropic.BetaToolUnionParam {
	return []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20250305: &anthropic.BetaWebSearchTool20250305Param{}},
		{OfWebFetchTool20250910: &anthropic.BetaWebFetchTool20250910Param{}},
	}
}
