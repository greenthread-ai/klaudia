# Klaudia

CLI agent that helps users with software engineering tasks.

## Build & Test

```bash
CGO_ENABLED=0 go build ./cmd/klaudia
go test ./internal/...
```

## Internal Package Layout

- **agent** - Main event loop, orchestration, and sub-agent spawning
- **api** - Provider abstraction: Anthropic client + OpenAI-compatible shim
- **tools** - Local tool implementations
- **browser** - Lazy headless-Chrome engine + web search
- **permission** - Permission modes + allow/deny rules (a leaf package)
- **trust** - Zones, command/tool classification, session-scoped grants
- **session** - Transcripts, resume, persisted compaction summaries
- **compaction** - Context/history compression (micro + auto)
- **mcp** - Model Context Protocol support
- **subagent** - Built-in sub-agent types
- **skill** - User-defined skills (`.klaudia/skills`)
- **memory** - Auto-memory store
- **doctor** - `/doctor` environment diagnostics
- **streamjson** - Bidirectional stream-json frontend
- **tui** - Terminal UI (Bubble Tea)
- **prompt** - Prompt construction
- **cli** - CLI entry point and wiring
- **native** - Pure-Go search / bash-parsing / PDF
- **sandbox** - Local / OS-confined / container Bash execution
- **schema** - Type/schema definitions
- **version** - Version info

## Rules

- Pure Go: builds must stay `CGO_ENABLED=0`-clean (no cgo, no system libs).
- The retired JavaScript reference lives on the `js-reference` branch; consult it
  with `git checkout js-reference` (not present on this branch).
