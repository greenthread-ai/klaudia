# Klaudia Parity Audit

Tracks the JS→Go port. The JS reference (`src/sections/*.js`, extracted from
Claude Code v2.1.66) was the golden reference for differential testing during
the port and has now been **retired to the `js-reference` branch** — the Go
binary is the product. This table is the record of what was ported and where it
deliberately diverges.

Parity was the method, not the destination. It was how the port was kept honest
while the JS tree was still there to diff against; it stopped being a goal the
moment the reference was retired. A 🔀 row is therefore a finished decision, not
an outstanding one — where a whole area has been redesigned rather than ported
(permissions, most visibly) the section says so in prose above its table.

**Status legend**

| Status | Meaning |
| --- | --- |
| ✅ done | Ported, tested, behaviourally equivalent to the JS reference. |
| 🟡 partial | Ported but narrower than JS (gaps noted). |
| 🔀 divergent | Intentionally differs from JS (a settled Klaudia design choice, including features deliberately removed). |
| ⛔ skipped | Not ported and not planned (with rationale). |
| 🔜 planned | On the current plan, not yet implemented. |

JS section map: `00-runtime` (bootstrap), `01-zod-state` (schemas/state),
`02-network` (HTTP/SSE), `03-providers` (model providers), `04-react-ink` (TUI
framework), `05-app-core` (agent loop/tools), `06-app-ui` (TUI screens),
`07-app-features` (slash commands, MCP, sessions, …), `08-entry` (CLI entry).

---

## Tools

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Read (text) | 05-app-core | `tools/read.go` | ✅ done | Line range, cat -n format; lines of any length (cut at 2000 bytes on a rune boundary, with a note); notice when the window stops short of EOF; binary (NUL in first 8 KB) → size only; directory → clean error. |
| Read (image) | 05-app-core | `tools/read.go` | ✅ done | Returns `ResultImage`; flows to vision via `toolResultWithImages`. Typed from the bytes, not the name; empty, non-image, over 5 MB or over 8000 px a side is refused before sending. |
| Read (PDF) | 05-app-core | `tools/read.go` + `native/pdf` | 🔀 divergent | Pure-Go text extraction via gopdf (JS rendered pages to images for the model). |
| Write | 05-app-core | `tools/write.go` | ✅ done | |
| Edit | 05-app-core | `tools/edit.go` | ✅ done | Exact-match + replace_all. |
| Glob | 05-app-core | `tools/glob.go` | ✅ done | |
| Grep | 05-app-core | `tools/grep.go` + `native/search` | ✅ done | ripgrep-style; pure-Go backend. |
| Bash | 05-app-core | `tools/bash.go` + `sandbox/` | ✅ done | local / os-confined / container executors. |
| Bash run_in_background + BashOutput / KillShell | 05-app-core | `tools/bash.go`, `tools/shells.go` + `sandbox` | ✅ done | Bash `run_in_background` returns a shell id; BashOutput reads new output incrementally (optional regex filter); KillShell stops it. Session-scoped store; all shells killed at session end. Works across all executors via `Executor.Argv`. |
| Code intelligence (LSP) | — | `lsp/` + `tools/lsp_tools.go` | 🔀 divergent | New capability with no JS analogue: Diagnostics/Definition/References via language servers detected on PATH + toolchain dirs (gopls/rust-analyzer/tsserver/pyright/clangd). Lazy-spawned, session-scoped, never downloaded. |
| TodoWrite | 05-app-core | `tools/todowrite.go` + `plan/` | ✅ done | Backed by `plan.Store`, one per conversation (`agent.Turn.Plan`; sub-agents get their own). Every frontend renders it from the loop's `plan` event: a live panel in the TUI (Ctrl+T), a `plan` event over stream-json, a native plan in ACP. An **approved** plan is also written to `~/.klaudia/plans/<session-id>.md` (as Claude Code's `~/.claude/plans`; no `plansDirectory` override yet), so it survives compaction and `--resume`. See `docs/plan-mode.md`. |
| Task tools (Create/List/Get/Update) | 07-app-features | — | 🔀 divergent | **Retired.** They backed a second, independent task store that nothing read — not the TUI, not stream-json, not ACP — so Klaudia had two parallel task systems and neither was authoritative. `TodoWrite` and `plan.Store` are the one answer. See `docs/plan-mode.md`. |
| NotebookEdit | 05-app-core | `tools/notebookedit.go` | ✅ done | |
| AskUserQuestion | 05-app-core | `tools/askuser.go` | ✅ done | Interactive via `Asker` seam. The TUI always appends a "something else" option (the model writes the choices, so its framing can be wrong); picking it — or just typing — opens free text, and the answer goes back verbatim. |
| ExitPlanMode | 05-app-core | `tools/exitplan.go` | ✅ done | Via `Planner` seam. |
| Agent (Task / sub-agents) | 07-app-features | `tools/agent.go` + `subagent/` | ✅ done | Dynamic description from `subagent.Builtin()`. `run_in_background` (TUI, headless and stream-json; ACP runs sub-agents in the foreground) and `fork` (starts from a copy of the conversation, `Spawner.SpawnFork`). |
| Memory | 07-app-features | `tools/memory.go` + `memory/` | 🔀 divergent | Auto-memory + recall via `.klaudia/memory/`. |
| ToolSearch (deferred loading) | 07-app-features | `tools/toolsearch.go` | ✅ done | Per-turn `buildToolParams` filter + `Context.Reveal`. |
| SlashCommand (model-invoked) | 07-app-features | `tools/skill.go` | 🔀 divergent | Covered by the Skill tool (user-defined skills the model can invoke). |
| BrowserSearch / BrowserFetch / browser navigation | 03-providers / 05 | `tools/browsersearch.go`, `tools/browserfetch.go`, `tools/browser.go`, `browser/` | 🟡 partial | Local default tools use lazy Chrome + DDG/Google and rendered markdown; a search *or a fetch* that comes back as a bot-challenge page (Cloudflare, DataDome, captcha) escalates once to headed Chrome on the persistent `~/.klaudia/browser/chrome-profile`, announces why, and closes the window when the page comes through. Permission-gated (allowed in autonomous; denied in plan); the engine is session-owned and Closed on exit. Navigation waits for the DOM to settle (readyState + text-stable, bounded) so JS-rendered content is captured. Google parsing filters Google's own nav/account links (DDG remains the stable default). Anthropic server-side betas still available via `agent/webtools.go`. |

## Agent loop & streaming

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Agentic tool loop | 05-app-core | `agent/loop.go` | ✅ done | Emitter, per-turn tool params, Approver/Asker/Planner seams. |
| Permission mode in the prompt | — | `agent/mode.go` | 🔀 divergent | The active mode is a system-prompt clause rebuilt per turn (approving a plan flips it mid-run), appended as a separate block so the cached prefix stays stable. Autonomous adds nothing. Changes emit a `permission_mode` event for headless frontends. |
| Parallel tool dispatch | 05-app-core | `agent/dispatchgroup.go` | ✅ done | Adjacent `tools.ConcurrencySafe` calls run together, 5 at a time. Groups are maximal runs so nothing is reordered; a call whose gate or permission check would ask is never grouped (one `Approver`, one prompt at a time); a group's events are delivered in call order with the head streaming live. `Read`/`Glob`/`Grep` are marked. |
| Per-message tool-result cap | 05-app-core | `agent/batchcap.go` | 🔀 divergent | One turn's combined tool-result text held to 200 KB, on top of the 30 KB per-result cap. Shares are water-filled, not split evenly; images and an existing spill notice are left alone. See `docs/compaction.md`. |
| Provider abstraction | 03-providers | `api/provider.go` | 🔀 divergent | Multi-provider interface (Anthropic + OpenAI-compatible); JS was Anthropic-centric. |
| Anthropic Messages (Beta, streaming) | 02/03 | `api/client.go` | ✅ done | Streamed with an idle watchdog (`streamIdleTimeout` in config). |
| OpenAI-compatible Chat Completions | — | `api/openai*.go` | 🔀 divergent | Translation shim; SSE stream:true. Image tool_results translated to `image_url` content parts. |
| Prompt caching | 03-providers | `api/cache.go` | ✅ done | `cache_control` breakpoints on tools + system + a rolling conversation tail; usage surfaced as `cache_read/creation_input_tokens`. Off with `api.Tuning.NoPromptCache`. |
| stream-json output (authoritative envelope) | 08-entry | `streamjson/` + `cli/envelope.go` | ✅ done | |
| stream-json partial deltas | 08-entry | `cli/partial.go` + `api/StreamSink` | ✅ done | Behind `--include-partial-messages`; emits JS `stream_event` lines. OpenAI shim synthesizes the sequence. |
| stream-json control requests | 08-entry | `streamjson/driver.go` | 🔀 divergent | `can_use_tool` is the JS subtype; `ask_user` and `exit_plan` are added, because without them AskUserQuestion and ExitPlanMode were dead over the one channel whose purpose is a client with a user in front of it. The ask also carries `tool_use_id`, specifier, suggestion and the host change, which the JS payload omits. |
| Agent Client Protocol (ACP) v1 | — | `acp/` | 🔀 divergent | New capability with no JS analogue: `--input-format acp` serves ACP over stdio for Zed / `acp.nvim` / JetBrains. Sessions and modes map onto Klaudia's own (ACP session ids *are* transcript ids; the mode picker is `/mode`, per thread) and skills become the client's commands; `TodoWrite` becomes a native plan (from the stored list, not the call's input) and Edit/Write carry diffs. `fs/read_text_file` is used (unsaved buffers), while client-side writes, `terminal/*` and `session/delete` are declined. See `docs/acp.md`. |
| One frontend contract (`agent.Turn`) | — | `agent/turn.go` | 🔀 divergent | TUI, stream-json and ACP all run turns through one struct instead of a per-frontend `RunFunc`. A capability is a field a frontend sets; the old positional signatures made "does not support it" indistinguishable from "was never wired". |

## Sessions, compaction, persistence

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| JSONL transcripts | 07-app-features | `session/` | ✅ done | `~/.klaudia/sessions/<encoded-cwd>/` (honors `KLAUDIA_CONFIG_DIR`); reads legacy `~/.klaudia/projects/<encoded-cwd>/`; EncodePath matches JS K0A. |
| Resume / continue / fork | 07/08 | `session/` + `cli/` | ✅ done | Auto-resumes most recent project session by default; `--new-session` starts fresh; `-r/--resume`, `--continue`, `--fork-session` remain. |
| Microcompaction (local) | 07-app-features | `compaction/` | ✅ done | |
| Autocompaction (model summary) | 07-app-features | `compaction/` | ✅ done | |
| Persisted summaries + resume seeding | — | `session/summary.go` + `agent.OnSummary` | 🔀 divergent | Compaction summaries persisted to `~/.klaudia/sessions/<encoded-cwd>/<id>.summary.md` (the old project-local `.klaudia/sessions/` path is still read), plus a `compact_boundary` line in the transcript; `--resume` seeds from the last summary **and the entries after it** (token-saving), `--full` replays the transcript. |
| Project knowledge recall (KNOWLEDGE.md) | — | `prompt.recalledKnowledge` | 🔀 divergent | `.klaudia/KNOWLEDGE.md` injected into the system prompt; curated, distinct from free-form memory. |

## Permissions

Parity was abandoned here on purpose, so read this section differently from the
rest. The JS permission model — five modes plus allow/deny rule matching — was
ported faithfully, shipped, and then **removed** once `trust/` existed, because
two overlapping gates asking about the same tool call is worse than either one
alone. The rows below record what JS had and what replaced it, not a gap to be
closed. `docs/trust.md` is the design record.

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| 5 permission modes | 07-app-features | `permission/` | 🔀 divergent | Deliberately three, not five: autonomous/plan/bypassPermissions. default/acceptEdits/dontAsk asked the user to take a stance on tool categories; zones answer that instead. |
| Allow/deny rule matching | 07-app-features | `permission/` | 🔀 divergent | Removed. Replaced by the host gate + session-scoped approvals: `/trust` grants by what an operation does, not by matching command text. `--allowedTools`/`--disallowedTools` went with it. |
| Persisted rules in config | 07-app-features | `config/` | 🔀 divergent | Removed with the rules. `[permissions]` keeps only `mode`; there is no `[trust]` section. Grants are session-scoped by design — a trust decision made for one task should not silently outlive it. |
| Host-change gate (zones, grants, ledger) | — | `hostgate/` + `trust/` | 🔀 divergent | New capability with no JS analogue: classifies a call by what it would change (project / user files / the machine) and asks only when it leaves the working tree. |
| Lifecycle hooks | 07-app-features | `hooks/` + `agent/hooks.go` | 🔀 divergent | Five events (SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Notification) against Claude Code's thirty-plus; Stop/SubagentStop deliberately absent. Claude Code's stdin field names and exit-2-blocks convention are kept so existing hook scripts run unedited. PreToolUse runs *after* the host gate, so a hook can narrow but never widen. Hooks declared in a project's config are confirmed once per fingerprinted set; editing the set re-asks. See `docs/hooks.md`. |

## MCP & sub-agents

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| MCP stdio client | 07-app-features | `mcp/` | ✅ done | Global `~/.klaudia/.mcp.json`, then `.mcp.json` + `.klaudia/.mcp.json` override. Project servers wait for a remembered approval, as Claude Code's project-scope servers do (see `docs/hooks.md`). |
| MCP tools wrapped (`mcp__*`) | 07-app-features | `mcp/` + `cli/` | ✅ done | Auto-deferred behind ToolSearch. |
| MCP reconnect/disconnect (`/mcp`) | — | `mcp.Manager` + `tui` | 🔀 divergent | Interactive `/mcp` picker; reconnect swaps the live session into the existing tool wrappers so a crashed server's tools resume, then rebuilds the registries so a server that failed at startup gets its tools at all. |
| MCP self-healing | — | `mcp.Manager.revive` | 🔀 divergent | A call that fails on the wire probes the server and relaunches it if it has stopped answering. The probe decides, so a bad argument cannot restart a healthy server. Read-only tools are retried; anything else is reported with the ambiguity stated, since the call may have run before the server died. Serialised per server, one attempt per 15s. |
| MCP HTTP/SSE transports | 07-app-features | `mcp/mcp.go` | ✅ done | A server with `url` uses the streamable HTTP transport (or `type:"sse"`; with no `type`, streamable then SSE); `command` stays stdio. `headers` are sent on every request, with `${VAR}` expansion. SSE is deprecated by the spec — `/doctor` warns when a server is still on it. |
| MCP elicitation (form) | — | `mcp/elicit.go` | 🔀 divergent | Protocol 2026-07-28. Form elicitation only, advertised as such, and only in an interactive session; URL mode is declined because a terminal cannot open a link out of band. One question per schema field through the same prompt `AskUserQuestion` uses; a type that will not coerce cancels rather than guessing. Serialised, because the SDK fulfils one round's requests concurrently and the frontend has a single question slot. |
| MCP sampling / roots / logging | 07-app-features | — | ⛔ not planned | All three are deprecated as of protocol 2026-07-28 (SEP-2577). Roots was never populated, so the capability is no longer advertised either. |
| Built-in sub-agents | 07-app-features | `subagent/` | ✅ done | |
| Sub-agent tool allowlists (`Type.Filter`) | 07-app-features | `subagent/` | 🔀 divergent | Explore/Plan get `Read`/`Glob`/`Grep` **and `Bash`**, and are held read-only by `Type.Mode` instead; general-purpose gets the full toolset by design. Withholding the shell also withheld `git log`, `git diff` and `go doc` — the commands research is made of — so the restriction is now the mode, which refuses mutation rather than execution. Not applied over `bypassPermissions`. See `docs/plan-mode.md`. |
| Sub-agent working-tree isolation | — | `worktree/` + `agent/spawner.go` | 🔀 divergent | New capability with no JS analogue: a sub-agent whose toolset can write gets its own `git worktree` under `~/.klaudia/worktrees/`, seeded with the parent's uncommitted work (not HEAD) and adopted back with `git apply` — index untouched, conflicts named to both the user and the model. Explore/Plan share the tree, and now take no worktree at all: a child that cannot write has nothing to isolate. Off with `[subagents] worktree = false`. See `docs/working-tree.md`. |

## TUI & frontends

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Interactive TUI | 04/06 | `tui/` | 🔀 divergent | Bubble Tea (JS was React+Ink). Single-reader invariant. |
| Headless `-p` / `--print` | 08-entry | `cli/` | ✅ done | text / json / stream-json. |
| stream-json input (embedding frontend) | 08-entry | `cli/` + `streamjson/` | ✅ done | `--input-format stream-json`. |
| @ file autocomplete | 06-app-ui | `tui/pathmenu.go` | ✅ done | Live dropdown as you type `@…` (ranked, ↑↓ to pick, Tab cycles, Esc dismisses). The earlier Tab-only completion was recorded here as a deliberate divergence; it was really invisible — typing `@` showed nothing, so the feature was reported as missing. Paths with spaces are inserted quoted, and references outside the working directory complete from the filesystem. |
| Slash command type-ahead | 06-app-ui | `tui/input.go` | ✅ done | Live suggestions as you type `/…`; Tab completes (unique → fill, many → common prefix). |
| Bracketed paste / drag-drop | 06-app-ui | bubbletea default | ✅ done | Bracketed paste on by default (multi-line pastes atomic). |
| Help bar / keymap / input history | 06-app-ui | `tui/keymap.go` + `tui/input.go` | ✅ done | Up/Down ring-buffer history, Tab completion, key hints; /help generated from one command table. |
| Scrollback | 06-app-ui | terminal-native (inline render) | ✅ done | Output is printed into the terminal's own scrollback via `tea.Println`; no alt-screen and no mouse capture, so scrolling, selection, search and tmux copy mode are the terminal's. `/search`, `/outline`, `/errors` add structured lookup on top. |
| Interrupt the model (Esc) | 06-app-ui | `tui/keys.go` + per-turn ctx | ✅ done | Esc cancels the in-flight turn (and any pending approval); loop-level test confirms cancellation propagates. |
| Elapsed-time indicator | 06-app-ui | `tui` + bubbles/stopwatch | 🔀 divergent | Live stopwatch while running + "done in"/"interrupted after" duration (replaces the removed /cost). |
| Welcome intro | 06-app-ui | `tui.intro` | 🔀 divergent | Coloured logo + model/branch + key hints at startup. |
| Markdown rendering | 06-app-ui | `tui` + glamour | ✅ done | Assistant answers stream raw, then render through glamour (headings, bold, syntax-highlighted code) on completion. |
| Throughput readout | 06-app-ui | `tui` | 🔀 divergent | "✓ done in 12.3s · 1.2k written · 98 tok/s" from real Usage data (output tokens; the status bar counts input, cache included, too). |

## Slash commands (TUI)

| Command | Klaudia | Status | Notes |
| --- | --- | --- | --- |
| /help, /quit, /exit, /clear | `tui/commands.go` | ✅ done | |
| /model | `tui/commands.go` | ✅ done | |
| /goal | `tui/commands.go` | ✅ done | |
| /memory | `tui/commands.go` | ✅ done | |
| /mcp | `tui/commands.go` | ✅ done | |
| /stats, /status | `tui/commands.go` | ✅ done | |
| /allow, /deny | — | 🔀 divergent | Removed with the rule model; `/trust` is the replacement. |
| /config, /agents, /context | `tui/commands.go` | ✅ done | Group A (render-only); friendly permission labels. |
| /mode | `tui/commands.go` + `permission.Mode.Label` | ✅ done | Interactive picker (or direct set) to change permission mode mid-session. The picker (`choice.go`) is a live filterable dropdown shared by `/model`, `/theme`, `/mode` and `/mcp`. |
| /compact, /add-dir | `tui/commands.go` + `agent.Loop.Compact` | ✅ done | Group B. |
| /cost, /permissions | — | 🔀 divergent | Not present: /cost was replaced by the per-turn "done in · written · tok/s" line and `/stats`; /permissions by `/mode` and `/trust`. |
| /plan, /doctor, /diff, /commit, /export | `tui/commands.go` + `doctor/` | ✅ done | Group C; /commit gated by a y/n confirm. /plan also takes `run [N]` and `stop` (plan runs, docs/plan-mode.md D5). |

## Auth & infra

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| API key / env credential | 03-providers | `anthropicauth/auth.go` | ✅ done | |
| OAuth keychain session | 07-app-features | `anthropicauth/auth.go` | ✅ done | macOS `security` CLI. |
| OAuth token refresh | 07-app-features | `anthropicauth/oauth.go` | ✅ done | Workstream 1d; refresh on expiry, write-back preserves unknown fields. |
| Host sandbox confinement | — | `sandbox/seatbelt.go`, `bwrap.go` | ✅ done | Workstream 1a (divergent: JS had no host confinement). |
| Container sandbox | — | `sandbox/container.go` | 🔀 divergent | New capability: run/validate code in docker/podman. |
| Telemetry / analytics | 00/07 | `pkg/agent/trace.go`, `cli/tracing.go` | 🔀 divergent | Usage analytics intentionally not ported. OpenTelemetry tracing (GenAI semconv, OTLP when `OTEL_EXPORTER_OTLP_*` is set) is opt-in and goes only where the user points it. |
| CLAUDECODE env vars | 08-entry | — | ⛔ skipped | Only referenced in JS + test-*.sh; Go never needed them. |
| Auto-updater | 00-runtime | — | ⛔ skipped | Distributed as a static binary; out of scope. |

## Skills

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Skill system (`.klaudia/skills/*.md`) | 07-app-features | `skill/` + `tools/skill.go` | ✅ done | Frontmatter skills as a Skill tool + TUI /<skill>. |

---

## JS retirement (done)

All differential-testing dependencies on the JS tree were cleared (the last was
stream-json partial deltas, now emitted behind `--include-partial-messages`).
The Go build and full test suite pass with the JS tree removed — definitive
proof nothing in it was load-bearing.

The JavaScript reference (`src/sections/*.js`), its build (`build.mjs`,
`package.json`), the pre-`native` Go sidecar tools (`tools/`, `vendor/`,
`*.wasm`), and the differential test scripts (`test-*.sh`) were removed from this
branch and **preserved on `js-reference`**. Consult them with
`git checkout js-reference`.

Everything in the tables above is ✅ done, a deliberate 🔀 divergence, a 🟡
partial with noted gaps, or a 🔜 planned post-parity enhancement that does not
require the JS reference.
