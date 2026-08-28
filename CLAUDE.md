# Klaudia

CLI agent that helps users with software engineering tasks.

## Build & Test

```bash
CGO_ENABLED=0 go install ./cmd/klaudia   # updates the `klaudia` on your PATH
CGO_ENABLED=0 go build ./...             # compile check only — writes no binary
go test ./internal/...
```

`go build ./...` with multiple packages compiles and *discards* the results: it
is a build check, not an install. `go build ./cmd/klaudia` does write a binary,
but into the working directory, not onto `$PATH`. Running `klaudia` after either
one runs whatever was installed last — a two-month-old binary in one real case.
Use `go install` when you mean to try the change.

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

## Design record

`docs/ux-spec.md` is the authoritative record of the two terminal-UX specs and
where the implementation deliberately differs from them — read it before
"fixing" something that looks unimplemented. `docs/trust.md`, `docs/jobs.md` and
`docs/working-tree.md` cover the three subsystems in detail.

## Rules

- Pure Go: builds must stay `CGO_ENABLED=0`-clean (no cgo, no system libs).
- Keep `charmbracelet/bubbles`, `bubbletea`, and `lipgloss` on the **v1** line.
  The TUI depends on textarea/renderer internals that v2 changes (v2
  `textarea.SetHeight` repositions the viewport; bubbletea v2 rewrites the inline
  renderer), which would break the input-wrap and last-column workarounds. A v2
  bump is a deliberate project, not a routine `go get -u` (see CHANGELOG).
- The retired JavaScript reference lives on the `js-reference` branch; consult it
  with `git checkout js-reference` (not present on this branch).
