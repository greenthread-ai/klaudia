package config

import "github.com/greenthread-ai/klaudia/pkg/api"

// Hints is Klaudia's wording for the settings and situations the core's
// messages point at (api.Hints): this file's keys, Klaudia's /compact, and
// the Claude Code sign-in Klaudia can use. The core words them neutrally for
// any other host.
var Hints = api.Hints{
	Retries:       "maxRetries in .klaudia/config.toml",
	IdleTimeout:   "streamIdleTimeout in .klaudia/config.toml",
	ContextWindow: "`contextWindow` in .klaudia/config.toml",
	MaxTokens:     "maxTokens in .klaudia/config.toml",
	Endpoint:      "the baseURL in ~/.klaudia/config.toml or ./.klaudia/config.toml",
	Credentials:   ".klaudia/config.toml for a custom provider",
	Compact:       "/compact",
	SharedLogin:   "you signed in via Claude Code OAuth",
}
