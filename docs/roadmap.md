# Roadmap: where agentic tooling is going, and what Klaudia should do about it

Written 2026-10-27. A recommendation, not a record of what is built — the
items here move to their subsystem docs as they land. Sources are dated; a
claim marked *(secondary)* came from a third-party write-up rather than the
vendor or the spec.

## The short version

1. **Extract the loop.** Every reusable agent core has converged on the same
   shape, Klaudia already has it, and nothing in Go fills the slot: Claude's
   Agent SDK is Python/TypeScript and tells other languages to shell out to
   `-p --output-format json`; Google ADK Go, Eino and Genkit are frameworks
   without a coding harness (permissions, sandbox, worktrees, compaction); ACP
   lists no Go SDK. The extraction is mostly untangling, not design — see
   [The extraction](#the-extraction).
2. **Make the sandbox the security boundary, not the gate.** The industry has
   stopped pretending a classifier is one (Willison, 2026-08-27; Codex 0.163's
   pre-DNS network proxy). Klaudia's trust zones are good; egress control is
   the missing half.
3. **Background and nested sub-agents.** The default in Claude Code since
   June; Klaudia's sub-agents still block the parent.
4. **Traces.** OTel GenAI spans are what an embedder will plug in first, and
   Klaudia emits none.

## What the field shipped this year

**Claude Code** (code.claude.com/docs/en/whats-new, changelog, fetched
2026-10-27). Auto mode — a classifier in place of prompts — default for new
sessions from 2026-08-14; parameter-matching deny/ask rules (Jun). Sub-agents
background by default (Jun 29), nest up to 5 levels (Jun), fork mode inheriting
the conversation by default (Aug), and background workers raise prompts in the
main session instead of auto-denying (Jun). `/goal` (May), self-paced `/loop`,
a Monitor tool streaming background events into the conversation (Apr),
scheduled/triggered cloud routines, cross-session messaging (Aug). Idle
compaction above 200K (Oct 14), rewind with "summarize up to here", tool search
on by default (Oct). Plugins bundling skills/agents/hooks/MCP, `plugin eval`
against a no-plugin baseline (Sep), fail-closed plugin hooks and validated
`updatedInput` (Oct). `fallbackModel` chains, `maxEffortLevel`, OTel GenAI span
attributes and safety events (Oct).

**Others.** Codex CLI 0.163–0.164 (Oct): mid-turn steering, parallel
read-only tools, a network proxy that checks permission before DNS, forked
sub-agents keeping parent history, lifecycle hooks on model requests. OpenAI's
Agents SDK added swappable sandboxes and a Temporal extension for durable
execution (temporal.io, Apr 15). Gemini CLI sub-agents as Markdown+YAML files
(Apr 15). Cursor's background agents became VM-per-agent "cloud agents". Zed
1.0 (Apr 29): parallel threads with optional worktree isolation *(secondary)*.
OpenCode: a headless server with an OpenAPI spec and SSE event stream, the TUI
just one client *(secondary)* — the closest analogue to what Klaudia would
become.

**Protocols.** MCP 2026-07-28: stateless (no `initialize`, capabilities in
`_meta`), Multi Round-Trip Requests replacing server→client requests, Tasks
moved to the `io.modelcontextprotocol/tasks` extension, cacheable list results
and deterministic tool order for prompt caching, `traceparent` in `_meta`;
Sampling, Roots and Logging deprecated. ACP stabilised `session/list`,
`resume`, `close`, `delete`, usage updates, `$/cancel_request` and elicitation
across 2026; Rust/TS SDKs hit 1.0 on Jun 25, v2 is in draft, remote transports
are being designed. AGENTS.md, MCP and goose sit under the Linux Foundation's
AAIF (Dec 2025). Agent Skills (`SKILL.md`, agentskills.io) is the cross-tool
skill format. OTel GenAI conventions (`invoke_agent`, `execute_tool`) are still
at Development stability.

## Where Klaudia already stands

Ahead of or level with the field: zone-based trust with `RequestHostChange`
(more principled than a classifier), MCP 2026-07-28 client with elicitation,
ACP agent with `session/list`, tool search over deferred tools, worktree
isolation per sub-agent, `/goal`, hooks with project-hook trust, AGENTS.md and
`SKILL.md`, background Bash jobs, micro + auto compaction with spill, one
`agent.Turn` contract across three frontends, `/undo` scoped to Klaudia's own
edits.

Missing: OTel; egress control beyond the container's `--network`; background,
nested or forked sub-agents; conversation rewind; fallback models;
idle compaction; MCP Tasks; any way to *serve* Klaudia over MCP; a public API.

## Recommendations, ranked

Size is a guess: S ≈ a day, M ≈ a week, L ≈ several.

| # | What | Why now | Size |
|---|------|---------|------|
| 1 | **Extraction stage 0–1** (below) | Unblocks embedding and costs nothing in behaviour; every later item lands in the right place if this goes first | M |
| 2 | **OTel GenAI tracing** in the loop: `invoke_agent` per turn, `execute_tool` per call, token usage as attributes, `traceparent` into MCP `_meta` | The first thing an embedder asks for, and the best tool for debugging the loop-breaker and compaction issues the git log keeps fixing. Pin the semconv version — it is not stable | M |
| 3 | **Egress control**: an in-process HTTP(S)/DNS proxy for the OS and container sandboxes, allowlist from config, decisions surfaced as trust events | The consensus security boundary. Trust zones decide what may change; nothing yet decides where data may go — the prompt-injection exfiltration path | L |
| 4 | **Background + nested sub-agents** — *background landed (TUI); nested, and the other frontends, not yet*: the Agent tool returns immediately and notifies on completion; depth cap; a background child's permission ask is routed to the parent frontend, never auto-denied | Parallelism is where agent throughput comes from now. The `Approver` plumbing already exists | M |
| 5 | **Fork sub-agents** (inherit the parent conversation) and `session/fork` | Cheap once sessions are append-only logs; the default in Claude and Codex | S–M |
| 6 | **Conversation rewind**: rewind to a turn, restoring files via the existing checkpoint stack, optionally "summarise up to here" | `/undo` already does the hard file half | M |
| 7 | **MCP serve mode**: `klaudia mcp serve` exposing "run an agent task" as a tool, long runs as Tasks | Makes Klaudia callable by every MCP host, which is most of them | M |
| 8 | **Job events into the conversation** (Monitor-style): a background job's output lines matching a pattern wake the loop | Klaudia's jobs are richer than most; this is the missing half that makes "start the server, watch for errors" work | S–M |
| 9 | **Fallback model chain** and an effort cap in config | Small, and the 5.5-sizing incident showed provider-side surprises are real | S |
| 10 | **Idle compaction** above a threshold, before the cache expires | Cheaper than compacting under pressure mid-turn | S |
| 11 | **Eval mode**: `--restricted` (no user config, no memory, no hooks) plus replaying saved sessions as regression tests | The transcript already records sizing per line; turning sessions into tests catches loop regressions before users do | M |
| 12 | **TUI**: links on the live region and in `/doctor`'s output, a hyperlink row in `/doctor` | Small follow-ups to the OSC 8 work | S |

Not recommended: plugin bundles (skills + agents + hooks + MCP in one
installable package) — not the direction for Klaudia, by decision 2026-10-27;
skills, hooks and MCP stay separately configured. And a classifier-based auto
mode. Klaudia's zones already
remove the approval fatigue it exists to fix, and the field's own experience is
that it is a convenience, not a boundary.

## The extraction

Measured from `go list` and grep on 2026-10-27. Today no core package imports
a frontend, and `Loop`/`Options`/`Event`/`Turn`/`api.Provider`/`tools.Tool`/
`tools.Registry` already have library shape. The obstacles are specific.

**Embed-blockers** — process-wide behaviour an embedder cannot control:

- ~~`agent/loop.go` read `DISABLE_*COMPACT`; `api` read
  `KLAUDIA_MAX_RETRIES`, `KLAUDIA_STREAM_IDLE_TIMEOUT`,
  `KLAUDIA_DISABLE_PROMPT_CACHE`~~ — done: `agent.Options.NoMicrocompact` /
  `NoAutocompact` and `api.Tuning`. Environment settings were treated as
  disposable (CI and debugging use, by decision): removed outright, not
  preserved behind a shim.
- `api/auth.go` reads credential variables; credential discovery belongs to
  the host and moves out with the rest of the product code.
- ~~`tools/output.go:142` spills to `session.ConfigRoot()/outputs` — so the
  loop's microcompact writes into `~/.klaudia` — and `tools/joblog.go:22`
  duplicates `ConfigRoot`~~ — done: the host passes both directories in,
  `tools.SpillStore` via `agent.Options.Spill` (and `Spawner.WithSpill`) and
  the job-log root via `tools.NewJobStore`. Unset, the core writes nothing:
  output is clamped without a spill file and job logs stay in memory.
  `agent`, `tools` and `compaction` no longer call `ConfigRoot` or read
  `KLAUDIA_CONFIG_DIR`.
- Model-facing error strings name `.klaudia/config.toml`
  (`agent/loop.go`, `api/errors.go`).
- ~~`agent` imports `trust`, `hooks`, `subagent`, `worktree` and `session`
  concretely~~ — done (stage 1): `agent` imports only `api`, `compaction`,
  `permission`, `plan`, `schema` and `tools`. `hooks` still imports `config`
  (the one upward edge).

**Stage 0 — internal plumbing.** Env reads become `Options` /
provider-constructor fields (done for the loop and providers). Inject the
spill and job-log directories (done).
Move `MessagesFromEntries` from `agent` to `session` (done: `session.Messages`).
Cut `hooks → config`.
Product strings behind an injectable hint function.

**Stage 1 — interfaces at the seams. Done.** `HostGate` becomes a pre-dispatch gate
interface with trust as Klaudia's implementation (done: `agent.Gate`, with the
whole host gate — card, approvers, hooks prompt — in `internal/hostgate`;
`ApprovalRequest.Detail` carries the card, so the core has no trust
vocabulary. Rationale from the maintainer: trust protects a harness running
on a person's own host; a sandboxed embedder has nothing to protect that way
and passes no gate); `hooks.Runner` a hooks
interface (done: `agent.Hooks`, adapted in `cli/hooks.go`); the spawner takes a sub-agent-type resolver and an optional
`Isolator` (worktrees) — done: `Spawner.WithTypes` and `WithIsolator`, with
Klaudia's implementations in `cli` (`subagentTypes`, `gitIsolator`) so neither
`subagent` nor `worktree` imports `agent`. Both gates are already nil-safe, so
the defaults are "none".

**Stage 2 — `pkg/` in this module, v0. Done.** The core is `pkg/agent`,
`pkg/api` (the provider layer — kept the name `api` rather than churn every
reference to `provider`), `pkg/tool`, `pkg/compaction`, `pkg/permission`,
`pkg/plan` and `pkg/schema`, with Klaudia as its first consumer; CI fails if
anything in `pkg/` depends on `internal/`. Not a separate module until the API
stops moving — two modules double the release work while every change is
still breaking. Built-in tools stay internal at first (`internal/tools`
aliases the contract), then an opt-in `pkg/tools/builtin`. The Anthropic
client in `pkg/api` is neutral; Klaudia's request shaping and credential
discovery are `internal/anthropicauth`.

~~Left from stage 0: product text in the core.~~ Done: `api.Hints` (neutral by
default; Klaudia's is `config.Hints`) and `agent.Options.Hints`. Was: `api.FriendlyError` advises
`.klaudia/config.toml` keys (`maxRetries`, `streamIdleTimeout`,
`contextWindow`, `baseURL`) and a Claude-Code-OAuth token-sharing cause, and
two loop notes name `maxTokens in .klaudia/config.toml`. Only Klaudia's
frontends call FriendlyError, so the fix is a hints value the host supplies
(neutral by default) and an `Options` field for the loop's setting name.

**Stage 3 — adapters.** `agent.Event` is already the stream-json wire schema;
version it and give it typed constructors. Lift ACP into `pkg/acp` — which
would be the first Go ACP SDK — and add the MCP-server adapter (#7).

**Message types — decided:** the library consumes anthropic-sdk-go directly
and the OpenAI-compatible API through the existing shim, with no re-export or
neutral layer. `anthropic.BetaMessageParam` stays the public message type.
What matters is provider optionality — the same loop over either backend —
and that is a property of `api.Provider`, not of owning the message structs.

Sessions as append-only logs with fork (#5) are the prerequisite for durable
execution (checkpoint, crash, resume elsewhere) — worth designing during stage
2 even if built later, because it shapes `Recorder`.

## TUI audit (2026-10-27)

The real binary in a pty against a scripted OpenAI-compatible mock (no model
spend), snapshotted through a small VT emulator at 100×30 and 60×20.

**Bugs**

Status 2026-10-27: 1–3 and 15 are done (6bc7909, ce5b5fc + cda401c, 469e853,
ec7625a), each with an e2e scenario that fails on the code before the fix;
6 and 7 in 7062b4d, 8 in 51afd62, and 4, 5, 9, 12 and 14 with unit tests.

1. **AskUserQuestion takes the next prompt as its answer.** Arrow keys and
   Enter do nothing; only a digit or free text answers, so a user who types
   their next message while a question is up answers the question with it.
   `/model`'s picker already does ↑↓ + Enter + digits + filter — use the same
   control for both.
2. **Plain lines hard-wrap mid-word** ("Klaudi|a's", "selec|ted"): non-Markdown
   lines reach `fitScrollback`'s hard wrap unwrapped. Word-wrap prose lines
   (notices, `/context`, the intro hint) first; keep the hard wrap for code
   and tool output. The intro hint is wider than 100 columns on its own.
3. **Absolute paths everywhere in display**: Glob results, "Edited /…/main.go",
   Read errors, `/context`'s cwd — each a two-row line. Show paths relative to
   the working directory on screen; the model keeps absolute ones.
4. **Interrupt notice prints above the text it interrupted** ("⊘ interrupting…"
   then the partial reply).
5. **`/context` lists files that do not exist** as Active / Recently inspected
   (a failed Read counts as an inspection).
6. **Plurals**: "1 file(s)", "1 replacement(s)", "1 turns", "(1 files)".
7. **"turns" counts API round trips** — one prompt with five tool steps reads
   "6 turns".
8. **Quitting prints no resume command**, and leaves the input box drawn in
   scrollback. Claude Code prints the `--resume` line.

**Improvements**

9. **One line per tool call.** Today: `⚙ Read main.go` then `✓ Read · 0ms:`
   and the whole file. Collapse Read/Glob/Grep/TodoWrite to a summary
   ("✓ Read main.go · 7 lines"), keep previews for Bash, drop sub-10ms timings.
10. **Edit shows only the two strings.** Render a real hunk with line numbers
    and a line of context.
11. **Markdown spacing**: two or three blank lines between blocks, tables
    stretched to the full width, Go code indented at 8 columns. Collapse blank
    runs, size tables to content, expand tabs at 4 inside code blocks.
12. **Startup card is ~10 lines on every start** (resume, working tree,
    trust, "previous approvals were not restored"). Auto-resume is deliberate;
    make the card one line ("continuing · 28 messages · 2h ago — /clear for a
    fresh start") and leave the rest to `/status`.
13. **`/help` is ~45 lines of scrollback.** Group it, or show it in the live
    region and dismiss it.
14. **Verification footer** says "nothing was run" when a command ran before
    the edit — "nothing was run after the last edit".
15. **Make the harness permanent**: the mock provider plus pty driver is an
    end-to-end TUI test that needs no API key — worth porting to Go under
    `internal/tui` or `scripts/` so these regressions are caught in CI.

Worked well: slash menu filtering, `@` completion, paste chips, multi-line
input, two-press Ctrl+C, Esc interrupt keeping the partial reply, long-output
clamp with `/last`, ✗ on failed tools, the verification footer, the `/model`
picker, and OSC 8 links on paths and URLs. Not observed: the live plan panel
(the mock finished too fast).

## Plan mode and goal runs: one work loop

Driven end to end against the mock (`TestPlanApprovalRuns`,
`TestGoalRunCompletesWithOneSummary`). They are two halves of the same
thing, each missing what the other has.

| | `/plan` | `/goal run` |
|---|---|---|
| Front half | read-only exploration, `ExitPlanMode`, y/n | goal-setting chat drafts a spec |
| Artefact | `~/.klaudia/plans/<session>.md`, checkbox format, kept in step by `TodoWrite` | `PRD.md` / `.klaudia/GOAL.md`, ticked by the model's edits |
| Execution | one turn; stops wherever the model stops | up to N iterations on a branch |
| Done means | nothing checks | no unchecked boxes, then a verification turn |
| Live UI | plan panel (Ctrl+T) | "↻ iteration k/N" lines only |

Seen in the battletest: a plan the model abandoned at 1/3 just stops, and
its panel then sits on screen through an unrelated goal run that has no
panel of its own.

**Recommendation — one loop, two front halves:**

1. **Plan approval uses the question picker** (`askView`) instead of y/n:
   1) *Approve — run it*, 2) *Approve — keep going until every step is
   done*, 3) *Keep planning — say what to change*. Option 3 does not exist
   today: "n" has no way to say why. This is also Claude Code's shape.
2. **Option 2 is the goal loop with the approved plan as its spec.** The
   plan file is already in goal's checkbox format and `goal.CountUnchecked`
   already reads it — the latent wiring CLAUDE.md describes. It needs an
   iteration prompt that records progress with `TodoWrite` (the store owns
   the file) rather than by editing it, and no branch switch: a plan is
   approved on the user's branch.
3. **One panel.** The live panel renders whichever work list is active: the
   plan store, or during `/goal run` the spec's checkboxes with
   "iteration k/N" in its header. That also retires the stale-panel case.
4. One completion block per run — done for goal runs (b2e95c1).

Keep the two front halves: exploring read-only and writing a spec are
different jobs.

**Status:** 1–4 landed (f9cba47), as D5 in docs/plan-mode.md. The running-
jobs panel below landed too; background sub-agents join it with #4.

**Running work below the prompt.** The status line counts running jobs;
nothing lists them. A panel in the same live-region slot — each job's name,
elapsed time and last output line, and later each background sub-agent
(#4) — is what Claude Code shows under its prompt and the natural home for
#4's results.
