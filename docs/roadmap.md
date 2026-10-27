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
| 4 | **Background + nested sub-agents**: the Agent tool returns immediately and notifies on completion; depth cap; a background child's permission ask is routed to the parent frontend, never auto-denied | Parallelism is where agent throughput comes from now. The `Approver` plumbing already exists | M |
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
- `agent` imports `trust`, `hooks`, `subagent`, `worktree` and `session`
  concretely; `hooks` imports `config` (the one upward edge).

**Stage 0 — internal plumbing.** Env reads become `Options` /
provider-constructor fields (done for the loop and providers). Inject the
spill and job-log directories (done).
Move `MessagesFromEntries` from `agent` to `session`. Cut `hooks → config`.
Product strings behind an injectable hint function.

**Stage 1 — interfaces at the seams.** `HostGate` becomes a pre-dispatch gate
interface with trust as Klaudia's implementation; `hooks.Runner` a `ToolHooks`
interface; the spawner takes a sub-agent-type resolver and an optional
`Isolator` (worktrees). Both gates are already nil-safe, so the defaults are
"none".

**Stage 2 — `pkg/` in this module, v0.** Move the core to `pkg/agent`,
`pkg/provider`, `pkg/tool`, `pkg/compaction`, `pkg/permission`, `pkg/plan`,
with Klaudia as its first consumer. Not a separate module until the API stops
moving — two modules double the release work while every change is still
breaking. Built-in tools stay internal at first, then an opt-in
`pkg/tools/builtin`.

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
