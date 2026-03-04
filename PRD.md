# Klaudia — Product Requirements Document

## Vision

Klaudia is a locally-buildable, extensible agentic coding tool forked from the Claude Code npm bundle. The long-term goal is to replace bundled third-party tools (ripgrep, tree-sitter, etc.) with custom Go implementations, making the tooling layer fully extensible and self-contained via `go run`.

## Current State

Claude Code is distributed as a single minified JavaScript bundle (`cli.js`, ~600K lines prettified, 11MB). It's built with esbuild, uses React + Ink for terminal UI, and bundles all dependencies inline. There is no public source repo.

We have extracted and prettified the v2.1.66 npm package into `npm-package/`.

### Go Tools Progress

| Tool | Status | Binary | Wired In | Replaces |
|------|--------|--------|----------|----------|
| Search | Done | `vendor/klaudia-search` | Yes (`G81()` ~L58793) | ripgrep |
| PDF | Done | `vendor/klaudia-pdf` | Yes (`hX1()` pdfinfo, `pT8()` pdftoppm) | pdfinfo + pdftoppm |
| Bash Parser | Done | `vendor/klaudia-bash-parser` | Yes (fallback in `Ac8()` ~L527728) | tree-sitter-bash.wasm |
| Screenshot | Done | `vendor/klaudia-screenshot` | Yes (`J0q()` ~L511382) | resvg.wasm |

All Go tools are built with `CGO_ENABLED=0` (pure Go, no C dependencies).

## Phase 1: Unbundle & Build Locally (Current)

**Goal:** Get a locally-buildable version that runs identically to the original.

### Steps
1. Create `klaudia/` project with proper `package.json` listing all dependencies
2. Extract the ~420K lines of application code (lines ~180K–600K of `cli.js`)
3. Replace references to bundled third-party modules with real npm `import` statements
4. Set up esbuild to rebundle into a working `cli.js`
5. Verify `node klaudia/dist/cli.js` runs and behaves like the original

### Identified Dependencies

**Anthropic (core):**
- `@anthropic-ai/sdk` — Claude API client
- `@anthropic-ai/claude-agent-sdk` — Agent framework

**Terminal UI:**
- `react` — UI framework
- `ink` — React renderer for terminal (provides useStdin, useInput, useApp, etc.)
- `yoga-layout` / `yoga-wasm-web` — Flexbox layout engine for Ink

**CLI:**
- `commander` — CLI argument parsing

**Cloud Providers:**
- `@aws-sdk/client-bedrock-runtime` — AWS Bedrock
- `@aws-sdk/credential-providers` — AWS auth
- `@azure/identity` — Azure auth
- `@azure/msal-node` — Azure OAuth
- `google-auth-library` — Google Cloud auth (Vertex AI)
- `gaxios` — Google HTTP client

**Networking & Protocols:**
- `ws` — WebSocket client
- `@grpc/grpc-js` — gRPC client
- `@grpc/proto-loader` — Protocol buffer loading
- `node-fetch` — HTTP client
- `https-proxy-agent` — Proxy support
- `axios` — HTTP client

**Parsing & Processing:**
- `marked` — Markdown parsing
- `zod` — Schema validation
- `ajv` — JSON Schema validation

**Terminal Utilities:**
- `ora` — Spinners
- `chalk` — Colors
- `strip-ansi` — ANSI escape removal
- `string-width` — Unicode string width
- `cli-boxes` — Box drawing

**Observability:**
- `@opentelemetry/api` — Tracing API
- `@opentelemetry/sdk-trace-base` — Tracing SDK

**Utilities:**
- `lodash` — General utilities
- `uuid` — UUID generation
- `semver` — Version comparison
- `diff` — Text diffing

**WASM Modules (vendored, keep as-is):**
- `resvg.wasm` — SVG rendering
- `tree-sitter.wasm` — Code parsing
- `tree-sitter-bash.wasm` — Bash grammar

**Native Tools (vendored, keep as-is initially):**
- `vendor/ripgrep/` — Code search binary

## Phase 2: Go Tool Layer (In Progress)

**Goal:** Replace vendored native tools with Go implementations.

### Completed
- **Search** (`tools/search/main.go`) — Ripgrep-compatible Go search tool. Supports all flags used by the app: `-l`, `-c`, `-n`, `-i`, `-e`, `-A`/`-B`/`-C`, `-U`, `--multiline-dotall`, `--glob`, `--type`, `--hidden`, `--files`, `--sort=modified`, `--max-depth`. Exit codes match ripgrep (0=matches, 1=none, 2=error). Wired into `G81()`.
- **PDF** (`tools/pdf/main.go`) — Pure Go PDF tool using `pdfcpu`. Modes: `info` (page count, pdfinfo-compatible output), `render` (image extraction), `text` (content extraction). Wired into `hX1()` and `pT8()` with fallback to system binaries.
- **Bash Parser** (`tools/bash-parser/main.go`) — Pure Go bash parser using `mvdan.cc/sh/v3`. Reads bash from stdin, outputs JSON: `{"command","args","envVars","hasPipe","partial","lastWord"}`. Wired into `Ac8()` as fallback when tree-sitter WASM isn't loaded — builds mock AST nodes that satisfy `qc8()`, `IJz()`, and `xJz()`.

- **Screenshot** (`tools/screenshot/main.go`) — Pure Go text-to-PNG renderer using `golang.org/x/image`. Bypasses SVG entirely — takes parsed ANSI text spans as JSON, renders directly to PNG with monospace fonts, rounded corners, and 4x scaling. Wired into `J0q()` with fallback to resvg WASM.

### Remaining
- **File watcher** — Replace chokidar with a Go file watcher (lower priority)
- **Sandbox** — Replace `@anthropic-ai/sandbox-runtime` seccomp setup with Go (lower priority)

### Architecture
```
klaudia/
  src/sections/     # Source split into 9 section files
    00-runtime.js   # esbuild E()/C()/s1() helpers
    01-lodash.js    # Lodash, Zod, early utils
    02-network.js   # ws, AJV, Axios
    03-providers.js # OpenTelemetry, AWS, gRPC, Azure
    04-react-ink.js # React, Ink, yoga-layout, tool name constants
    05-app-core.js  # Tools, permissions, MCP, API client
    06-app-ui.js    # UI, dialogs, WebSearch/WebFetch tools
    07-app-features.js # Tree-sitter, screenshot, voice, REPL
    08-entry.js     # Main bootstrap
  tools/            # Go tool source
    search/main.go  # ✅ Replaces ripgrep
    pdf/main.go     # ✅ Replaces pdfinfo + pdftoppm
    bash-parser/    # ✅ Replaces tree-sitter-bash.wasm
    screenshot/     # ✅ Replaces resvg.wasm
  vendor/           # Built Go binaries + original fallbacks
  dist/cli.js       # Built output (sections concatenated)
  docs/             # Architecture docs
    server-side-tools.md  # Server-side tool inventory
  build.mjs         # Section concatenator (+ watch mode)
  package.json
  go.mod
```

### Integration Pattern
Go tools are spawned as child processes. Each follows the conventions of what it replaces:
- **Search**: CLI args and stdout format match ripgrep exactly
- **PDF**: CLI args match pdfinfo/pdftoppm; output format matches pdfinfo (`Pages:  N`)
- **Bash Parser**: Reads bash from stdin, outputs JSON to stdout

The JS layer checks for Go binaries first (`vendor/klaudia-*`), falling back to the original tools if not found.

## Phase 2.5: Tool Separation (Done)

**Goal:** Cleanly separate local tools from server-side tools for future replacement.

### Completed
- **Renamed tool constants** — 9 tool name constants renamed from mangled 2-char identifiers to readable names (`BASH_TOOL_NAME`, `READ_TOOL_NAME`, `WEB_SEARCH_TOOL_NAME`, etc.) across all section files
- **Renamed server-side tool objects** — `webSearchTool`, `webFetchTool`, `buildWebSearchToolDef`, schemas, and init functions
- **Renamed dispatch functions** — `streamApiRequest` (main API request builder), `buildToolSchema` (tool schema assembler)
- **Marked tool boundaries** — `// Klaudia:` comment markers at tool merge point, response discrimination, and server-side tool definitions
- **Documented server-side tools** — Full inventory in `docs/server-side-tools.md` with request/response formats

### Key Architecture (for future replacement)

**Tool merge point:** `06-app-ui.js` in `streamApiRequest()`:
```
h = [...N, ...(w.extraToolSchemas ?? [])]
//    ^local    ^server-side (web_search, web_fetch, code_execution)
```

**Response discrimination:** Same function handles `server_tool_use` (no-op locally) vs `tool_use` (needs local dispatch).

**To swap WebSearch for a local implementation:**
1. Replace `webSearchTool.call()` in `06-app-ui.js` — currently delegates to API
2. Implement local search (e.g., SearXNG API, Brave Search API)
3. Return results matching the `webSearchOutputSchema` format

**To swap WebFetch for a local implementation:**
1. Replace `webFetchTool.call()` in `06-app-ui.js`
2. Implement local HTTP fetch + content extraction
3. Return results matching `webFetchOutputSchema` format

See `docs/server-side-tools.md` for full request/response schemas.

## Phase 3: Server-Side Tool Replacement

**Goal:** Replace Anthropic server-side tools with local implementations.

### Priority
1. **WebSearch** — Local search via SearXNG or Brave Search API
2. **WebFetch** — Local HTTP fetch with content extraction (node-fetch + readability)
3. **MCP** — Already works locally via stdio; keep as-is

### Lower Priority
4. **Code Execution** — Local sandboxed execution (Docker/nsjail)
5. **Browser Automation** — Local browser bridge (Playwright MCP server)

## Phase 4: Extended Tooling

**Goal:** Add custom tools beyond what Claude Code ships with.

- Custom code analysis tools
- Project-specific linters
- Integration with internal services
- Custom MCP servers written in Go

## Non-Goals

- Reimplementing the Anthropic SDK or Claude API
- Rebuilding React/Ink (the terminal UI framework works fine)
- Replacing cloud provider SDKs (AWS/Azure/GCP auth is complex)
- Making this a general-purpose fork — this is for our team's workflow
