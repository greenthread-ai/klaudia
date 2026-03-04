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
  src/cli.js        # Prettified bundle (Go tools wired in via spawn)
  tools/            # Go tool source
    search/main.go  # ✅ Replaces ripgrep
    pdf/main.go     # ✅ Replaces pdfinfo + pdftoppm
    bash-parser/    # ✅ Replaces tree-sitter-bash.wasm
      main.go
    screenshot/     # ✅ Replaces resvg.wasm
      main.go
  vendor/           # Built Go binaries + original fallbacks
    klaudia-search
    klaudia-pdf
    klaudia-bash-parser
    klaudia-screenshot
    ripgrep/        # Fallback
  dist/             # Built output
  build.mjs         # esbuild config
  package.json
  go.mod
```

### Integration Pattern
Go tools are spawned as child processes. Each follows the conventions of what it replaces:
- **Search**: CLI args and stdout format match ripgrep exactly
- **PDF**: CLI args match pdfinfo/pdftoppm; output format matches pdfinfo (`Pages:  N`)
- **Bash Parser**: Reads bash from stdin, outputs JSON to stdout

The JS layer checks for Go binaries first (`vendor/klaudia-*`), falling back to the original tools if not found.

## Phase 3: Extended Tooling

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
