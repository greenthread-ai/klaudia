# Klaudia — Product Requirements Document

## Vision

Klaudia is a locally-buildable, extensible agentic coding tool: a single static
Go binary (Linux + macOS) with no runtime dependencies, a self-contained tooling
layer, and room to grow tools/providers/UI for our team's workflow.

## Status

**The Go port is complete and is the product.** Klaudia began as a cleanroom
extraction of Claude Code (`@anthropic-ai/claude-code` v2.1.66) into prettified
JavaScript, which served as a golden reference for differential testing during a
full port to Go. That reference has been retired to the `js-reference` branch.

What ships today (see [docs/parity.md](docs/parity.md) for the full map):

- **Agentic loop** with streaming, tool dispatch, compaction (micro + auto), and
  sub-agents.
- **Providers**: Anthropic Messages API (default) and an OpenAI-compatible Chat
  Completions shim (incl. image tool-results), selected via `.klaudia/config.json`.
- **~18 local tools** (Read/Write/Edit/Glob/Grep/Bash/Notebook/Todo/Task/Agent/
  Memory/Skill/ToolSearch/AskUserQuestion/ExitPlanMode) plus permission-gated
  **web search & browsing** backed by a lazy headless Chrome.
- **Three frontends**: interactive Bubble Tea TUI, headless `-p`, and a
  bidirectional stream-json embedding channel for editors/SDKs.
- **5-mode permission system** + allow/deny rules; **Bash sandboxing** (local /
  OS-confined via sandbox-exec·bwrap / container).
- **MCP** stdio servers (with `/mcp` reconnect/disconnect), **skills**,
  **auto-memory** + curated **project knowledge**, **sessions** with
  token-saving resume from persisted compaction summaries, **OAuth** keychain
  refresh.

All builds are pure Go (`CGO_ENABLED=0`); the native search / bash-parsing / PDF
layers are pure-Go (no wasm, no vendored binaries, no system tools required).

## Deliberate divergences from the JS reference

- Bubble Tea TUI (not React + Ink).
- Multi-provider abstraction (the JS was Anthropic-centric).
- Local, default-on web search/browse via headless Chrome (the JS used
  Anthropic's server-side `web_search`/`web_fetch`, which remain available on the
  Anthropic provider).
- Config/sessions live under `~/.klaudia` (`KLAUDIA_CONFIG_DIR`), not `~/.claude`.
- New capabilities with no JS analogue: OS/container Bash sandboxing, persisted
  resume summaries, project `KNOWLEDGE.md`, container code-execution.

## Roadmap (post-parity)

- **Tooling depth**: background shells (BashOutput/KillShell), sub-agent tool
  allowlists beyond the current read-only set, MCP HTTP/SSE transports.
- **Web**: more robust result parsing + a network-idle wait for JS-heavy pages.
- **Provider breadth**: image tool-results and richer translation for more
  OpenAI-compatible backends.
- **Knowledge**: let the agent curate `KNOWLEDGE.md` (scoped Memory writes), and
  evaluate embeddings for recall.
- **Extended tooling**: project-specific analyzers, custom Go MCP servers.

## Non-goals

- Reimplementing the Anthropic SDK or the Claude API.
- Cloud-provider SDK auth (Bedrock/Vertex/Foundry) — out of scope.
- A general-purpose fork — this targets our team's workflow.

## History

The JavaScript reference (split into `src/sections/*.js`, built with `build.mjs`)
and the Go sidecar tools that preceded the pure-Go `native` packages are
preserved on `js-reference`. `git checkout js-reference` to consult them.
