# Klaudia

A locally-buildable fork of Claude Code, extracted from the v2.1.66 npm package. The long-term goal is to replace vendored third-party tools with custom Go implementations — see [PRD.md](PRD.md) for the full roadmap.

## Prerequisites

- Node.js >= 18
- npm
- Anthropic API key or existing Claude Code OAuth session

## Setup

```bash
cd klaudia
npm install
```

## Build

```bash
node build.mjs
```

Builds `src/cli.js` → `dist/cli.js`. Currently a pass-through (the source is already a self-contained bundle), but this is where esbuild config lives for when we start splitting modules.

## Run

```bash
# Direct (uses src/cli.js)
CLAUDECODE= node src/cli.js --print "hello"

# Built output
CLAUDECODE= node dist/cli.js --print "hello"

# Interactive mode
CLAUDECODE= node src/cli.js
```

**Note:** You must unset `CLAUDECODE` when running inside a Claude Code session, otherwise it blocks with a "nested sessions" error.

### Auth

Klaudia uses the same auth as Claude Code. Either:
- Set `ANTHROPIC_API_KEY` in your environment, or
- Use an existing Claude Code OAuth session (tokens in `~/.claude/`)

### Provider Selection

```bash
# Default: Anthropic direct API
CLAUDECODE= node src/cli.js --print "hello"

# AWS Bedrock
CLAUDE_CODE_USE_BEDROCK=1 CLAUDECODE= node src/cli.js --print "hello"

# Google Vertex AI
CLAUDE_CODE_USE_VERTEX=1 CLAUDECODE= node src/cli.js --print "hello"
```

## Test

```bash
./test-harness.sh
```

Runs 5 validation tests:

| # | Test | What it checks |
|---|------|----------------|
| 1 | Version | `--version` outputs version string |
| 2 | Simple prompt | `--print` returns expected response |
| 3 | Model override | `--model claude-haiku-4-5` selects correct model |
| 4 | JSON output | `--output-format json` produces valid JSON |
| 5 | Help | `--help` shows usage info |

All tests use `--print` (non-interactive mode) and make real API calls (except test 5).

## Go Tools

Klaudia progressively replaces vendored third-party tools with custom Go implementations. All are built with `CGO_ENABLED=0` (pure Go, no C dependencies).

### Building

```bash
cd klaudia

# Build all Go tools
CGO_ENABLED=0 go build -o vendor/klaudia-search ./tools/search/
CGO_ENABLED=0 go build -o vendor/klaudia-pdf ./tools/pdf/
CGO_ENABLED=0 go build -o vendor/klaudia-bash-parser ./tools/bash-parser/
cd tools/screenshot && CGO_ENABLED=0 go build -o ../../vendor/klaudia-screenshot . && cd ../..
```

### Status

| Tool | Binary | Replaces | Wired In |
|------|--------|----------|----------|
| `tools/search/main.go` | `vendor/klaudia-search` | ripgrep | Yes |
| `tools/pdf/main.go` | `vendor/klaudia-pdf` | pdfinfo + pdftoppm | Yes |
| `tools/bash-parser/main.go` | `vendor/klaudia-bash-parser` | tree-sitter-bash.wasm | Yes (fallback) |
| `tools/screenshot/main.go` | `vendor/klaudia-screenshot` | resvg.wasm | Yes (fallback) |

Go tools are preferred at runtime when present in `vendor/`; the app falls back to the original tools if they're missing.

### Testing Go tools directly

```bash
# Search (ripgrep-compatible)
echo "hello world" | vendor/klaudia-search "hello" -n
vendor/klaudia-search --files --glob "*.go" .

# PDF
vendor/klaudia-pdf info sample.pdf
vendor/klaudia-pdf text sample.pdf -f 1 -l 5

# Bash parser
echo 'FOO=bar git commit -m "msg"' | vendor/klaudia-bash-parser
# → {"command":"git","args":["commit","-m","\"msg\""],"envVars":["FOO=bar"],...}

# Screenshot (text-to-PNG renderer)
echo '{"lines":[[{"text":"hello","color":{"r":0,"g":255,"b":0}}]],"options":{}}' | vendor/klaudia-screenshot output.png
```

## Project Structure

```
klaudia/
├── src/cli.js            # Prettified source (600K lines, from minified bundle)
├── dist/cli.js           # Built output
├── build.mjs             # esbuild configuration
├── test-harness.sh       # 5-test validation suite
├── package.json          # Dependencies
├── PRD.md                # Product roadmap (3 phases)
├── go.mod                # Go module definition
├── tools/
│   ├── search/main.go    # Go ripgrep replacement
│   ├── pdf/main.go       # Go PDF tool (pdfcpu)
│   ├── bash-parser/      # Go bash parser (mvdan.cc/sh)
│   │   └── main.go
│   └── screenshot/       # Go text-to-PNG renderer (golang.org/x/image)
│       └── main.go
├── vendor/
│   ├── klaudia-search    # Built Go binary
│   ├── klaudia-pdf       # Built Go binary
│   ├── klaudia-bash-parser # Built Go binary
│   ├── klaudia-screenshot  # Built Go binary
│   └── ripgrep/          # Original rg binaries (fallback)
├── resvg.wasm            # SVG rendering (Go replacement active, WASM is fallback)
├── tree-sitter.wasm      # Code parsing
└── tree-sitter-bash.wasm # Bash grammar (Go replacement active, WASM is fallback)
```

## Architecture (Key Locations in `src/cli.js`)

The source is a prettified esbuild bundle with 4,603 modules (2,369 ESM + 2,234 CommonJS). Variable names are mangled but module boundaries are clear.

### Layout

| Line Range | Content |
|------------|---------|
| 1–70 | esbuild runtime helpers |
| 70–15K | Lodash utilities |
| 15K–25K | WebSocket, AJV, network libs |
| 63K–66K | OpenTelemetry tracing |
| 100K–125K | AWS SDK, gRPC, Azure |
| 120K–180K | React + Ink (terminal UI) |
| **180K–600K** | **Claude Code application** |
| 597K–600K | Entry point, `main()` bootstrap |

### Key Functions

| Function | Line | Purpose |
|----------|------|---------|
| `fI()` | ~236587 | API client factory — **all** API calls route through here |
| `h7()` | ~69273 | Provider router (Bedrock/Vertex/Foundry/firstParty) |
| `Cz` | ~157253 | Base HTTP client class |
| `mh` | ~157750 | Anthropic firstParty client |

### API Flow

```
User prompt
  → main() bootstrap (line ~600K)
  → conversation loop
  → fI() creates client (line ~236587)
  → h7() selects provider (line ~69273)
  → client.beta.messages.create({ model, messages, ... })
  → POST https://api.anthropic.com/v1/messages?beta=true
  → SSE stream parsed (message_start → content_block_delta → message_stop)
  → Response rendered via React/Ink
```

## Origins

Extracted from the `@anthropic-ai/claude-code` npm package v2.1.66:

```bash
# How this was created (for reference, don't re-run)
curl -sL https://registry.npmjs.org/@anthropic-ai/claude-code/-/claude-code-2.1.66.tgz | tar xz
npx prettier --write package/cli.js
# Then moved into this directory structure
```
