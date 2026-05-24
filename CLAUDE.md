# Klaudia

CLI agent that helps users with software engineering tasks.

## Build & Test

```bash
CGO_ENABLED=0 go build ./cmd/klaudia
go test ./internal/...
```

## Internal Package Layout

- **agent** - Main event loop and orchestration
- **api** - Anthropic API client
- **tools** - Local tool implementations
- **permission** - Permission/authorization checks
- **session** - Session and state management
- **compaction** - Context/history compression
- **mcp** - Model Context Protocol support
- **subagent** - Sub-agent launching
- **streamjson** - JSON streaming parser
- **tui** - Terminal UI components
- **prompt** - Prompt construction
- **cli** - CLI entry point
- **native** - Native bindings
- **sandbox** - Sandboxed execution
- **schema** - Type/schema definitions
- **version** - Version info

## Rules

- `src/sections/` (JS reference implementation) is read-only
