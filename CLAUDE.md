# Klaudia

CLI agent that helps users with software engineering tasks.

## Build & Test

```bash
CGO_ENABLED=0 go install ./cmd/klaudia   # updates the `klaudia` on your PATH
CGO_ENABLED=0 go build ./...             # compile check only — writes no binary
go test ./...
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

## Package Layout

`pkg/` is the embeddable core — the agent loop and what it needs, importable by
other products (see docs/roadmap.md, "The extraction"); it must not import
`internal/`. `internal/` is Klaudia the product.

- **pkg/agent** - The agent loop: dispatch, gate/hooks/isolator interfaces, sub-agent spawning, background runs
- **pkg/compaction** - Context/history compression (micro + auto)
- **pkg/api** - Provider abstraction: Anthropic client + OpenAI-compatible shim, model sizing, tuning
- **pkg/permission** - The three permission modes (a leaf package)
- **pkg/plan** - The session plan: step list + the approved-plan artefact
- **pkg/schema** - Type/schema definitions
- **pkg/tool** - The tool contract: `Tool`, `Registry`, `Context`, `Result`, the spill store and the approver interfaces (`internal/tools` aliases them)

- **anthropicauth** - Klaudia's Anthropic client: credential discovery (env, the `--login` store, Claude Code's login from the Keychain or `~/.claude/.credentials.json`) and the request shaping it sends; kept out of `pkg/api`
- **tools** - Klaudia's built-in tool implementations (the contract is `pkg/tool`)
- **browser** - Lazy headless-Chrome engine + web search
- **trust** - Zones, command/tool classification, session-scoped grants
- **hostgate** - Klaudia's `agent.Gate`: the host gate, its approval card (`CardOf`), headless approver
- **session** - Transcripts, resume, persisted compaction summaries
- **mcp** - Model Context Protocol client (2026-07-28) + form elicitation
- **subagent** - Built-in sub-agent types
- **worktree** - Per-sub-agent git checkouts: seed, adopt, conflict reporting
- **textsafe** - Cleans text that reaches the model from files and servers Klaudia did not write (invisible characters out, lengths capped)
- **gitprobe** - The guard on git commands Klaudia runs on its own (status, diff, checkpoints, worktrees): no fsmonitor, hooks or external diff drivers from the repository's config
- **skill** - User-defined skills (`.klaudia/skills`)
- **hooks** - Lifecycle hooks: the five events, project-hook trust, execution
- **memory** - Auto-memory store
- **doctor** - `/doctor` environment diagnostics
- **streamjson** - Bidirectional stream-json frontend
- **acp** - Agent Client Protocol v1 agent (editor-driven sessions)
- **tui** - Terminal UI (Bubble Tea v2, with its own inline renderer in `inline.go`);
  `tui/e2e` drives the real binary in a pty against a scripted mock provider,
  including window resizes (`KLAUDIA_E2E=1 go test ./internal/tui/e2e/...`)
- **prompt** - Prompt construction
- **replay** - Replays recorded sessions through today's loop (recorded model
  replies and tool results, nothing executed): request checks, schema drift,
  and golden snapshots of a corpus the e2e suite generates (`KLAUDIA_E2E_KEEP`).
  `KLAUDIA_REPLAY_DIR=~/.klaudia/sessions go test ./internal/replay -run Local`
  sweeps your own sessions
- **cli** - CLI entry point and wiring
- **native** - Pure-Go search / bash-parsing / PDF
- **sandbox** - Local / OS-confined / container Bash execution
- **egress** - `[sandbox] egress`: the allowlisting proxy commands are pointed at (enforced under the macOS OS sandbox)
- **config** - `config.toml` schema and loading (user file under the config root, project overlay)
- **goal** - Goal specs and the loop's prompts and gates (also drives plan runs)
- **lsp** - Language-server pool behind the code-intelligence tools
- **fuzzy** - Fuzzy matching for `@` completion and pickers
- **version** - Version info

## Design record

`docs/ux-spec.md` is the authoritative record of the two terminal-UX specs and
where the implementation deliberately differs from them — read it before
"fixing" something that looks unimplemented. `docs/trust.md`, `docs/jobs.md`,
`docs/working-tree.md`, `docs/hooks.md`, `docs/acp.md`, `docs/effort.md`,
`docs/plan-mode.md`, `docs/releasing.md`, `docs/compaction.md`,
`docs/memory-architecture.md`, `docs/server-side-tools.md` and
`docs/models.md` (model kinds, the media tools, `[models] deny`) cover
subsystems in detail; `docs/parity.md` maps features against Claude Code, and
`docs/roadmap.md` is the forward plan (recommendations, not records).

`docs/plan-mode.md` records five decisions, all landed. In brief: a permission refusal
feeds neither loop-breaker (a refusal is a decision, not a malfunction, and
unlike a host-gate refusal it is revoked mid-run by routes the loop never
sees), and plan mode gates **mutation** rather than execution via
`trust.InspectCommand`. That classifier has three verdicts and the third is the
point: a command it cannot read *asks* rather than being refused, because an
allowlist that denies what it has not got round to classifying is the bug plan
mode opened with. D3 landed too: `pkg/plan` is now the one answer to
"where does the plan live", replacing three unconnected ones (plan-mode prose,
goal mode's checkbox file, and an in-memory todo list built inside the registry
that only ACP rendered — from the tool *input*, not from any store). A fourth,
the `Task*` tools over `internal/tasks`, was registered and read by nothing and
is retired. Read that doc before adding another. The store is owned by the
caller and passed in with `tools.WithPlanStore`; an **approved** plan is
written to `~/.klaudia/plans/<session-id>.md` (session-keyed: a per-project
file let concurrent sessions clobber each other). It uses goal mode's checkbox
format so `goal.CountUnchecked` *can* read it. Nothing calls it on a plan: a
plan run (D5 — the goal loop over the approved plan) gates on the store's
snapshot, not the file. Treat the format as latent, not wiring, but don't drift
it. D4 landed: the loop emits a `plan`
event when the store's revision moves, and the TUI, stream-json and ACP all render from it. The store is
**per conversation** — `agent.Turn.Plan` → `Options.Plan` → `tools.Context.Plans` — because a
process-wide one let ACP threads share a list and let a sub-agent's `TodoWrite` replace the user's plan. D5: plan
approval is a picker (run it / keep going / keep planning), and "keep going" or
`/plan run` drives the goal loop over the approved plan — one loop, plan-specific
prompts, no branch switch.

Two subsystems execute a string from a config file: hooks, and the MCP servers
in a project's `.mcp.json`. Both take the same trust prompt — the global file
is the user's own, a project's is confirmed once per set, remembered
(`trust.Approvals`) until the set changes — and `docs/hooks.md` is where the
reasoning lives. A hook runs *after* the host gate, never instead of it:
anything that lets a config file grant permission is a bug, not a feature
request.

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

Model sizing has **one resolver, `api.Resolve`**, used at startup and by
`/model` alike: config → provider → the offline tables → floor, for the output
cap and the context window together. The provider's figures come from a
once-a-day on-disk cache of `GET /v1/models` (`api.ModelCache`,
`~/.klaudia/cache/`), so they beat the table without a network call on start;
only a model the table lacks pays a live lookup. `/model` used to size models
itself, with the opposite precedence and no config — a configured `maxTokens`
stopped applying after any switch. When the provider contradicts the table,
`/doctor` says so. The three tables in `client.go`/`effort.go` are an
allowlist of model ids, and an allowlist goes stale the day a model ships — the
whole Claude 5.5 lineup was missing from all three at once, so Opus 5.5 ran at
`max_tokens=8192` against a cap of 128000 and compacted as if it had 200k of
1M. Neither failure looked like a stale map. So: add a model to all three
tables or none, and run `KLAUDIA_LIVE_MODELS=1 go test ./pkg/api` (with
`ANTHROPIC_API_KEY` set) when you touch them. It is not in CI — the repository holds no API key — so the
runtime is the backstop: the provider's figures beat the table (cached daily),
and `/doctor` warns when the two disagree.
`DefaultMaxOutputTokens` is a floor for a model nothing can size, not a
default. Every assistant line in the transcript records the figures that turn
ran with and the `LimitSource*` behind each, stamped by `baseOptions` so all
five frontends get it from one place — the next sizing bug is answerable from
the session file instead of from what the user remembers choosing.

## Rules

- Pure Go: builds must stay `CGO_ENABLED=0`-clean (no cgo, no system libs).
- The TUI is on Bubble Tea, Bubbles, Lip Gloss and Glamour **v2**
  (`charm.land/...`). Three things about it are not what the upstream
  examples suggest. The live region is drawn by `internal/tui/inline.go`, not
  Bubble Tea's renderer — v2's, like v1's, strands frames on resize; read the
  header before changing how frames reach the terminal. Lip Gloss v2 styles
  always emit full colour, so anything Klaudia writes itself goes through
  `downsample` (`colorprofile.go`). And Lip Gloss v2 expands tabs in any
  multi-line string despite `NoTabConversion`, which is what the `chrome`
  style type in `tui.go` works around.
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

