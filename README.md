# Klaudia

A locally-buildable, extensible fork of Claude Code, extracted from the v2.1.66 npm package. See [PRD.md](PRD.md) for the full roadmap.

## Prerequisites

- Node.js >= 18
- npm
- Anthropic API key or existing Claude Code OAuth session

## Setup

```bash
cd klaudia
npm install
node build.mjs          # Concatenates sections → dist/cli.js
```

## Usage

### Interactive (REPL)

```bash
node dist/cli.js
```

### Headless (one-shot)

```bash
# Basic — waits for all turns, prints final result
node dist/cli.js -p "What files are in this directory?"

# With full autonomy (skips all permission prompts)
node dist/cli.js -p "Create hello.py" --dangerously-skip-permissions

# Stream events as they happen (see tool calls in real-time)
node dist/cli.js -p "Explain the build system" --output-format stream-json --verbose

# JSON output with full metadata
node dist/cli.js -p "What is 2+2?" --output-format json

# To Spy on your running session
ls -t ~/.claude/projects/*/*.jsonl | head -1 | xargs tail -20
```

**Note:** You must unset `CLAUDECODE` when running inside a Claude Code session, otherwise it blocks with a "nested sessions" error.

### Watching progress in headless mode

In text mode (`-p`), output only appears after all turns complete. To monitor:

```bash
# Watch the live session transcript
ls -t ~/.claude/projects/*/*.jsonl | head -1 | xargs tail -f

# Or use stream-json to see events as they arrive
node dist/cli.js -p "..." --output-format stream-json --verbose 2>/dev/null \
  | jq -r 'select(.type == "assistant") | .message.content[] | select(.type == "text") | .text'
```

### Permission modes

| Flag | Mode | Behavior |
|------|------|----------|
| *(default)* | `default` | Prompts for dangerous operations (interactive only) |
| `--permission-mode acceptEdits` | `acceptEdits` | Auto-accepts file edits |
| `--dangerously-skip-permissions` | `bypassPermissions` | Allows everything |
| `--permission-mode plan` | `plan` | Read-only exploration, no modifications |
| `--permission-mode dontAsk` | `dontAsk` | Denies anything not pre-approved |

In headless mode (`-p`) without `--dangerously-skip-permissions`, tools needing
approval are **denied** (no TTY to prompt). The model adapts and reports what it
found without modifying anything.

### Model selection

```bash
node dist/cli.js --model haiku       # Fast/cheap
node dist/cli.js --model sonnet      # Default
node dist/cli.js --model opus        # Most capable
```

### Custom model and endpoint

For inference against a custom endpoint (e.g. a dev model on a local network), use CLI flags or env vars. Server-side calls (telemetry, auth, token counting metadata) always go to the Anthropic prod API — only inference calls are routed to the custom endpoint.

**CLI flags:**

```bash
node dist/cli.js \
  --custom-model moonshotai/Kimi-VL-A3B-Thinking-2506 \
  --custom-endpoint http://122.252.4.33:8000
```

**Environment variables (ideal for galph/subshell usage):**

```bash
export KLAUDIA_CUSTOM_MODEL=moonshotai/Kimi-VL-A3B-Thinking-2506
export KLAUDIA_CUSTOM_ENDPOINT=http://122.252.4.33:8000
node dist/cli.js
```

You can also select "Custom endpoint" from the interactive `/model` picker at runtime.

### Auth

Klaudia uses the same auth as Claude Code. Either:
- Set `ANTHROPIC_API_KEY` in your environment, or
- Use an existing Claude Code OAuth session (tokens in `~/.claude/`)

Auth is used for both prod Anthropic API calls and custom endpoint calls (the same API key/token is sent to both).

## Build

```bash
node build.mjs            # One-shot build
node build.mjs --watch    # Watch mode (rebuilds on section changes)
```

### Go tools

All Go tools are built with `CGO_ENABLED=0` (pure Go, no C dependencies):

```bash
CGO_ENABLED=0 go build -o vendor/klaudia-search ./tools/search/
CGO_ENABLED=0 go build -o vendor/klaudia-pdf ./tools/pdf/
CGO_ENABLED=0 go build -o vendor/klaudia-bash-parser ./tools/bash-parser/
cd tools/screenshot && CGO_ENABLED=0 go build -o ../../vendor/klaudia-screenshot . && cd ../..
```

## Test

```bash
bash test-harness.sh                  # 5 core tests
bash test-harness.sh --with-websearch # 6 tests (includes web search)
```

| # | Test | What it checks |
|---|------|----------------|
| 1 | Version | `--version` outputs `2.1.66-klaudia` |
| 2 | Simple prompt | `-p` returns expected response |
| 3 | Model override | `--model claude-haiku-4-5` selects correct model |
| 4 | JSON output | `--output-format json` produces valid JSON |
| 5 | Help | `--help` shows usage info |
| 6 | Web search | Server-side `web_search` tool works (optional) |

## Architecture

The app is split into 9 section files in `src/sections/`, concatenated by `build.mjs` into `dist/cli.js`.

| File | Contents |
|------|----------|
| `00-runtime.js` | esbuild helpers (`E()`, `C()`, `s1()`) |
| `01-zod-state.js` | Zod validation, i18n locales, session state, MCP transport |
| `02-network.js` | WebSocket, AJV, Axios |
| `03-providers.js` | OpenTelemetry, gRPC |
| `04-react-ink.js` | React, Ink, yoga-layout, tool name constants |
| `05-app-core.js` | Tools, permissions, MCP, compaction |
| `06-app-ui.js` | UI, API client, tool dispatch, server-side tools |
| `07-app-features.js` | Tree-sitter, screenshot, voice, headless mode |
| `08-entry.js` | CLI bootstrap, Commander setup |

### Removed providers

Bedrock (AWS), Vertex (Google), and Foundry (Microsoft) provider code paths have been removed. `getProvider()` always returns `"firstParty"` (Anthropic direct API). Custom model endpoints are supported via `--custom-model` / `--custom-endpoint`.

### Key renamed functions

We've renamed ~75 mangled identifiers to readable names. Key ones:

| Name | File | Purpose |
|------|------|---------|
| `cliMain` | 08-entry.js | CLI entry point |
| `setupCommander` | 08-entry.js | Commander CLI parser |
| `runHeadless` | 07-app-features.js | Headless mode executor |
| `agentLoop` | 06-app-ui.js | Core stream→tools→repeat loop |
| `callModel` | 06-app-ui.js | SDK entry point |
| `streamApiRequest` | 06-app-ui.js | API request builder |
| `dispatchToolUse` | 06-app-ui.js | Tool executor |
| `checkToolPermissions` | 07-app-features.js | Permission checker |
| `createApiClient` | 05-app-core.js | API client factory (supports custom `baseURL`) |
| `getProvider` | 03-providers.js | Provider router (always `"firstParty"`) |
| `getModelOverride` | 07-app-features.js | Model override retrieval |
| `getCurrentModel` | 07-app-features.js | Current model getter |
| `getCustomEndpoint` | 07-app-features.js | Custom endpoint URL resolver |
| `getModelDisplayName` | 07-app-features.js | Model display name formatter |
| `ModelPickerUI` | 06-app-ui.js | Interactive model selection component |
| `buildModelOptions` | 06-app-ui.js | Model option list builder |
| `microcompact` | 05-app-core.js | Fast client-side compaction |
| `autocompactFn` | 05-app-core.js | Model-based compaction |
| `buildToolSchema` | 06-app-ui.js | Tool → API schema |
| `onAppStateChange` | 07-app-features.js | App state change handler |

### Go tool status

| Tool | Binary | Replaces | Wired In |
|------|--------|----------|----------|
| `tools/search/` | `vendor/klaudia-search` | ripgrep | Yes |
| `tools/pdf/` | `vendor/klaudia-pdf` | pdfinfo + pdftoppm | Yes |
| `tools/bash-parser/` | `vendor/klaudia-bash-parser` | tree-sitter-bash.wasm | Yes (fallback) |
| `tools/screenshot/` | `vendor/klaudia-screenshot` | resvg.wasm | Yes (fallback) |

Go tools are preferred at runtime; the app falls back to originals if missing.

## Documentation

- [PRD.md](PRD.md) — Product roadmap and phase tracking
- [docs/agent-calling-usage.md](docs/agent-calling-usage.md) — Full CLI → agent → tool execution flow
- [docs/compaction.md](docs/compaction.md) — Context window management (microcompact + autocompact)
- [docs/server-side-tools.md](docs/server-side-tools.md) — WebSearch, WebFetch, Code Execution, MCP schemas

## Origins

Extracted from the `@anthropic-ai/claude-code` npm package v2.1.66:

```bash
# How this was created (for reference, don't re-run)
curl -sL https://registry.npmjs.org/@anthropic-ai/claude-code/-/claude-code-2.1.66.tgz | tar xz
npx prettier --write package/cli.js
# Then split into sections and moved into this directory structure
```
