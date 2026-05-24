# Klaudia

A locally-buildable, extensible coding agent — a single static Go binary. Klaudia
began as a cleanroom of Claude Code (v2.1.66) and has been ported to Go; the
original JavaScript reference has been retired (preserved on the `js-reference`
branch). See [PRD.md](PRD.md) for background and [docs/parity.md](docs/parity.md)
for the feature map.

## Build

Pure Go, no CGO, no system libraries:

```bash
CGO_ENABLED=0 go build -o klaudia ./cmd/klaudia
go test ./internal/...
```

The result is one self-contained binary (Linux + macOS).

## Usage

### Interactive (TUI)

```bash
./klaudia
```

A Bubble Tea terminal UI: streamed Markdown answers, `/` slash commands with
type-ahead, `@path` file completion (Tab), input history (↑/↓), scrollback
(PgUp/PgDn), and `Esc` to interrupt a turn. Type `/help` for the full list.

### Headless (one-shot)

```bash
# Print the final result and exit
./klaudia -p "What files are in this directory?"

# Full autonomy (skip permission prompts)
./klaudia -p "Create hello.py" --dangerously-skip-permissions

# Stream events as JSON (tool calls, results) as they happen
./klaudia -p "Explain the build" --output-format stream-json --verbose

# Partial message deltas, JS-compatible (only with --print + stream-json)
./klaudia -p "…" --output-format stream-json --verbose --include-partial-messages
```

### Embedding (stream-json over stdin)

A persistent agent driven by newline-delimited JSON over stdin/stdout — the
channel for editor/SDK integrations (no terminal needed):

```bash
./klaudia --input-format stream-json --verbose
```

### Resuming

```bash
./klaudia --continue                 # resume the most recent session here
./klaudia -r <session-id>            # resume a specific session
./klaudia -r <session-id> --full     # replay the whole transcript (not the summary)
```

Sessions are JSONL transcripts under `~/.klaudia/projects/<encoded-cwd>/`
(override the base with `KLAUDIA_CONFIG_DIR`). When a session has a persisted
compaction summary, `--resume` seeds from it (token-saving) unless `--full`.

## Permission modes

| Flag | Mode | Behavior |
|------|------|----------|
| *(default)* | `default` | Ask before risky operations (interactive only) |
| `--permission-mode acceptEdits` | `acceptEdits` | Auto-accept file edits |
| `--dangerously-skip-permissions` | `bypassPermissions` | Allow everything |
| `--permission-mode plan` | `plan` | Read-only; mutations and network blocked |
| `--permission-mode dontAsk` | `dontAsk` | Deny anything not pre-approved |

In the TUI, `/mode` switches modes interactively. Headless `-p` without
`--dangerously-skip-permissions` denies anything that would prompt (no TTY).
Session allow/deny rules: `--allowedTools 'Bash(go test:*)'`, `--disallowedTools …`,
or `/allow` / `/deny` at runtime.

## Model & provider

Klaudia defaults to the Anthropic Messages API. A project or user
`.klaudia/config.json` selects the provider and model:

```jsonc
{
  "provider": "openai",                 // "anthropic" (default) | "openai"
  "model": "openai/gpt-5.5",
  "baseURL": "https://api.example.com/v1", // OpenAI-compatible endpoint
  "apiKeyEnv": "MY_API_KEY"             // or "apiKey" (prefer the env form)
}
```

`--model haiku|sonnet|opus` (or a full model ID) overrides per-run. The
OpenAI-compatible provider translates the Anthropic message shape to Chat
Completions (including image tool-results → `image_url`).

`~/.klaudia/config.json` is the user default; a project `./.klaudia/config.json`
overlays it (project wins). Settings merge per field.

### Auth

- `ANTHROPIC_API_KEY` (or `ANTHROPIC_AUTH_TOKEN`), or
- an existing Claude Code OAuth session in the macOS Keychain (Klaudia refreshes
  expired tokens and writes them back), or
- a provider key via `apiKey` / `apiKeyEnv` in `.klaudia/config.json`.

## Sandboxing the Bash tool

`.klaudia/config.json` → `sandbox.mode`:

- `local` (default) — run on the host, unconfined.
- `os` — host confinement: `sandbox-exec` (macOS) / `bubblewrap` (Linux). Reads
  are unrestricted; writes limited to cwd + temp (+ `writeRoots`); `network`
  configurable. Falls back to local with a warning if the tool is absent.
- `container` — run inside docker/podman (`runtime`, `image`, `mountCwd`,
  `readOnly`, `network`).

## Web search & browsing

Built-in, permission-gated tools backed by a lazily-launched **headless Chrome**
(nothing spawns until a web tool runs; the browser is closed at session end):

- `WebSearch` — DuckDuckGo (default) or Google; returns titles/URLs/snippets.
- `WebFetch` / `BrowserNavigate` / `BrowserSnapshot` — render a page and return
  Markdown.

Requires a Chrome/Chromium install (auto-discovered; set `KLAUDIA_CHROME_PATH`
on Linux/Windows if not on `PATH`). Tunable via `.klaudia/config.json` →
`browser` (`engine`, `headless`, `chromePath`, `userDataDir`, `headedFallback`,
…) or `KLAUDIA_*` env vars. When a search hits a bot-challenge page, Klaudia can
relaunch a **headed** Chrome with a persistent profile (`~/.klaudia/browser/…`)
so you can solve it once. Anthropic's server-side `web_search`/`web_fetch` betas
remain available when using the Anthropic provider.

## MCP

Model Context Protocol servers from `.mcp.json` (project `.klaudia/.mcp.json`
overrides). A server is **stdio** (`command` + `args`) or **HTTP** (`url`, with
`type:"sse"` for the legacy SSE transport):

```jsonc
{ "mcpServers": {
  "local":  { "command": "my-server", "args": ["--stdio"] },
  "remote": { "type": "http", "url": "https://mcp.example.com/v1" }
} }
```

Their tools appear as `mcp__<server>__<tool>`, auto-deferred behind `ToolSearch`.
In the TUI, `/mcp` lists servers and reconnects/disconnects them.

## Skills

Drop Markdown files with YAML frontmatter in `~/.claude/skills` or
`.klaudia/skills/` (project wins). They become a `Skill` tool the model can
invoke and `/＜name＞` commands in the TUI. Body supports `$ARGUMENTS`.

```markdown
---
name: review
description: Structured review of the current diff
---
Review the staged changes carefully. $ARGUMENTS
```

## Memory & project knowledge

- **Auto-memory** — the `Memory` tool reads/writes notes under
  `.klaudia/memory/`; the index is recalled into the system prompt.
- **Project knowledge** — `.klaudia/KNOWLEDGE.md` (curated, durable lessons) is
  injected into the system prompt when present.

## Internal package layout

| Package | Responsibility |
|---------|----------------|
| `agent` | the agentic loop + sub-agent spawning |
| `api` | provider abstraction (Anthropic client + OpenAI-compatible shim) |
| `tools` | local tool implementations |
| `browser` | lazy headless-Chrome engine + web search |
| `permission` | the 5-mode permission system + allow/deny rules |
| `session` | JSONL transcripts, resume, persisted summaries |
| `compaction` | micro + auto context compaction |
| `mcp` | Model Context Protocol client |
| `subagent` | built-in sub-agent types |
| `skill` | user-defined skills |
| `memory` | auto-memory store |
| `doctor` | `/doctor` environment diagnostics |
| `sandbox` | local / OS-confined / container Bash execution |
| `streamjson` | bidirectional stream-json frontend |
| `tui` | Bubble Tea terminal UI |
| `cli` | command entry, flags, wiring |
| `native` | pure-Go search / bash-parsing / PDF |
| `prompt`, `schema`, `config`, `version`, `tasks` | supporting packages |

## Documentation

- [PRD.md](PRD.md) — background and goals
- [docs/parity.md](docs/parity.md) — JS→Go feature map and divergences
- [docs/agent-calling-usage.md](docs/agent-calling-usage.md) — CLI → agent → tool flow
- [docs/compaction.md](docs/compaction.md) — context-window management
- [docs/server-side-tools.md](docs/server-side-tools.md) — Anthropic server-side tool schemas

## History

The original JavaScript reference (extracted and prettified from the
`@anthropic-ai/claude-code` v2.1.66 npm package, split into `src/sections/*.js`)
served as the golden reference during the port and now lives on the
`js-reference` branch. `git checkout js-reference` to consult it.
