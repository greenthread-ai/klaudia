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

`klaudia --version` prints the constant and then the commit the binary was
built from, so you can check which one you are running rather than inferring it
from mtimes. Releases are a tag push; `docs/releasing.md` has the steps and the
reason `-buildvcs=false` must never appear in the build flags.

## Internal Package Layout

- **agent** - Main event loop, orchestration, and sub-agent spawning
- **api** - Provider abstraction: Anthropic client + OpenAI-compatible shim
- **tools** - Local tool implementations
- **browser** - Lazy headless-Chrome engine + web search
- **permission** - The three permission modes (a leaf package)
- **trust** - Zones, command/tool classification, session-scoped grants
- **session** - Transcripts, resume, persisted compaction summaries
- **compaction** - Context/history compression (micro + auto)
- **mcp** - Model Context Protocol client (2026-07-28) + form elicitation
- **subagent** - Built-in sub-agent types
- **worktree** - Per-sub-agent git checkouts: seed, adopt, conflict reporting
- **skill** - User-defined skills (`.klaudia/skills`)
- **hooks** - Lifecycle hooks: the four events, project-hook trust, execution
- **memory** - Auto-memory store
- **doctor** - `/doctor` environment diagnostics
- **streamjson** - Bidirectional stream-json frontend
- **acp** - Agent Client Protocol v1 agent (editor-driven sessions)
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
"fixing" something that looks unimplemented. `docs/trust.md`, `docs/jobs.md`,
`docs/working-tree.md`, `docs/hooks.md`, `docs/acp.md`, `docs/effort.md` and
`docs/releasing.md` cover seven subsystems in detail.

Hooks are the one subsystem that executes a string from a config file, so
`docs/hooks.md` is where the reasoning for the trust prompt lives. A hook runs
*after* the host gate, never instead of it: anything that lets a config file
grant permission is a bug, not a feature request.

There are three frontends — TUI, stream-json, ACP — and one contract between
them: `agent.Turn`, applied by `Turn.Apply`. A new capability is a **field on
`Turn`**, not another parameter on a per-frontend `RunFunc`; the per-frontend
signatures are what let `AskUserQuestion` and `ExitPlanMode` sit dead over
stream-json for months, because a frontend that omitted a capability was
indistinguishable from one that did not want it. `docs/acp.md` records what the
ACP frontend declines (client-side writes, `terminal/*`, `session/delete`) and
why those are decisions rather than gaps.

Effort is **one setting translated per provider**, not a per-provider setting:
`api.Effort` carries the five levels, the Anthropic path writes
`output_config.effort` and the OpenAI shim writes `reasoning_effort`, and
unset sends nothing on both. Keep it that way — a provider-shaped knob would
let the same config mean different things on the two backends, which is the
capability skew `docs/effort.md` exists to prevent.

## Rules

- Pure Go: builds must stay `CGO_ENABLED=0`-clean (no cgo, no system libs).
- Keep `charmbracelet/bubbles`, `bubbletea`, and `lipgloss` on the **v1** line.
  The TUI depends on textarea/renderer internals that v2 changes (v2
  `textarea.SetHeight` repositions the viewport; bubbletea v2 rewrites the inline
  renderer), which would break the input-wrap and last-column workarounds. A v2
  bump is a deliberate project, not a routine `go get -u` (see CHANGELOG).
- The retired JavaScript reference lives on the `js-reference` branch; consult it
  with `git checkout js-reference` (not present on this branch).

## Commits

`area: lowercase sentence` subject, then a body explaining the reasoning and
separating what was measured from what was assumed. Sign your work with:

```
Co-Authored-By: Klaudia <noreply@greenthread.ai>
```

Nothing enforces this — there is no `commit.template` and no hook, and the
author identity comes from the user's `~/.gitconfig` — so it lives here instead.
Earlier commits carry `noreply@anthropic.com` (Claude Code's default) or a model
name in place of `Klaudia`; both are history, not the convention. Use the same
trailer on patches sent upstream on the maintainer's behalf.

