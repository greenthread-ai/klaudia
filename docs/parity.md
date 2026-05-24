# Klaudia Parity Audit

Tracks the JS→Go port. The JS reference (`src/sections/*.js`, extracted from
Claude Code v2.1.66) is the golden reference for differential testing until
parity is reached, then retired to a `js-reference` branch.

**Status legend**

| Status | Meaning |
| --- | --- |
| ✅ done | Ported, tested, behaviourally equivalent to the JS reference. |
| 🟡 partial | Ported but narrower than JS (gaps noted). |
| 🔀 divergent | Intentionally differs from JS (a deliberate Klaudia design choice). |
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
| Read (text) | 05-app-core | `tools/read.go` | ✅ done | Line range, cat -n format; directory → clean error. |
| Read (image) | 05-app-core | `tools/read.go` | ✅ done | Returns `ResultImage`; flows to vision via `toolResultWithImages`. |
| Read (PDF) | 05-app-core | `tools/read.go` + `native/pdf` | 🔀 divergent | Pure-Go text extraction via gopdf (JS rendered pages to images for the model). |
| Write | 05-app-core | `tools/write.go` | ✅ done | |
| Edit | 05-app-core | `tools/edit.go` | ✅ done | Exact-match + replace_all. |
| Glob | 05-app-core | `tools/glob.go` | ✅ done | |
| Grep | 05-app-core | `tools/grep.go` + `native/search` | ✅ done | ripgrep-style; pure-Go backend. |
| Bash | 05-app-core | `tools/bash.go` + `sandbox/` | ✅ done | local / os-confined / container executors. |
| BashOutput / KillShell (background shells) | 05-app-core | — | 🔜 planned | No background-shell tool yet; Bash runs to completion. |
| TodoWrite | 05-app-core | `tools/todowrite.go` | ✅ done | |
| Task tools (Create/List/Get/Update) | 07-app-features | `tools/task*.go` + `tasks/` | ✅ done | |
| NotebookEdit | 05-app-core | `tools/notebookedit.go` | ✅ done | |
| AskUserQuestion | 05-app-core | `tools/askuser.go` | ✅ done | Interactive via `Asker` seam. |
| ExitPlanMode | 05-app-core | `tools/exitplan.go` | ✅ done | Via `Planner` seam. |
| Agent (Task / sub-agents) | 07-app-features | `tools/agent.go` + `subagent/` | ✅ done | Dynamic description from `subagent.Builtin()`. |
| Memory | 07-app-features | `tools/memory.go` + `memory/` | 🔀 divergent | Auto-memory + recall via `.klaudia/memory/`. |
| ToolSearch (deferred loading) | 07-app-features | `tools/toolsearch.go` | ✅ done | Per-turn `buildToolParams` filter + `Context.Reveal`. |
| SlashCommand (model-invoked) | 07-app-features | — | 🔜 planned | Skill system (workstream 2) covers this. |
| WebSearch / WebFetch | 03-providers / 05 | `agent/webtools.go` | 🟡 partial | Server-side Anthropic betas only; no local impl for custom providers. See server-side-tools.md. |

## Agent loop & streaming

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Agentic tool loop | 05-app-core | `agent/loop.go` | ✅ done | Emitter, per-turn tool params, Approver/Asker/Planner seams. |
| Provider abstraction | 03-providers | `api/provider.go` | 🔀 divergent | Multi-provider interface (Anthropic + OpenAI-compatible); JS was Anthropic-centric. |
| Anthropic Messages (Beta, streaming, caching) | 02/03 | `api/client.go` | ✅ done | |
| OpenAI-compatible Chat Completions | — | `api/openai*.go` | 🔀 divergent | Translation shim; SSE stream:true. Image tool_results not yet translated. |
| Prompt caching | 03-providers | `api/client.go` | ✅ done | |
| stream-json output (authoritative envelope) | 08-entry | `streamjson/` + `cli/envelope.go` | ✅ done | |
| stream-json partial deltas | 08-entry | `cli/partial.go` + `api/StreamSink` | ✅ done | Behind `--include-partial-messages`; emits JS `stream_event` lines. OpenAI shim synthesizes the sequence. |

## Sessions, compaction, persistence

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| JSONL transcripts | 07-app-features | `session/` | ✅ done | `~/.claude/projects/<encoded-cwd>/`; EncodePath matches JS K0A. |
| Resume / continue / fork | 07/08 | `session/` + `cli/` | ✅ done | `-r/--resume`, `--continue`, `--fork-session`. |
| Microcompaction (local) | 07-app-features | `compaction/` | ✅ done | |
| Autocompaction (model summary) | 07-app-features | `compaction/` | ✅ done | |
| Persisted summaries + resume seeding | — | `session/summary.go` + `agent.OnSummary` | 🔀 divergent | Compaction summaries persisted to `.klaudia/sessions/<id>.summary.md`; `--resume` seeds from them (token-saving), `--full` replays the transcript. |
| Project knowledge recall (KNOWLEDGE.md) | — | `prompt.recalledKnowledge` | 🔀 divergent | `.klaudia/KNOWLEDGE.md` injected into the system prompt; curated, distinct from free-form memory. |

## Permissions

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| 5 permission modes | 07-app-features | `permission/` | ✅ done | default/acceptEdits/bypassPermissions/plan/dontAsk. |
| Allow/deny rule matching | 07-app-features | `permission/` | ✅ done | e.g. `Bash(git status:*)`. |
| Persisted rules in config | 07-app-features | `config/` | ✅ done | |

## MCP & sub-agents

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| MCP stdio client | 07-app-features | `mcp/` | ✅ done | `.mcp.json` + `.klaudia/.mcp.json` override. |
| MCP tools wrapped (`mcp__*`) | 07-app-features | `mcp/` + `cli/` | ✅ done | Auto-deferred behind ToolSearch. |
| MCP HTTP/SSE transports | 07-app-features | — | 🟡 partial | stdio only so far. |
| Built-in sub-agents | 07-app-features | `subagent/` | ✅ done | |
| Sub-agent tool allowlists (`Type.Filter`) | 07-app-features | `subagent/` | 🟡 partial | Seam present; v1 inherits full toolset. |

## TUI & frontends

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Interactive TUI | 04/06 | `tui/` | 🔀 divergent | Bubble Tea (JS was React+Ink). Single-reader invariant. |
| Headless `-p` / `--print` | 08-entry | `cli/` | ✅ done | text / json / stream-json. |
| stream-json input (embedding frontend) | 08-entry | `cli/` + `streamjson/` | ✅ done | `--input-format stream-json`. |
| @ file autocomplete | 06-app-ui | `tui/tui.go` | 🔀 divergent | Tab-completes the trailing @<path> token (robust completion vs. JS live overlay). |
| Slash command type-ahead | 06-app-ui | `tui/tui.go` | ✅ done | Live suggestions as you type `/…`; Tab completes (unique → fill, many → common prefix). |
| Bracketed paste / drag-drop | 06-app-ui | bubbletea default | ✅ done | Bracketed paste on by default (multi-line pastes atomic). |
| Help bar / keymap / input history | 06-app-ui | `tui/tui.go` | ✅ done | Up/Down ring-buffer history, Tab completion, key hints in /help. |
| Interrupt the model (Esc) | 06-app-ui | `tui/tui.go` + per-turn ctx | ✅ done | Esc cancels the in-flight turn (and any pending approval); loop-level test confirms cancellation propagates. |
| Elapsed-time indicator | 06-app-ui | `tui` + bubbles/stopwatch | 🔀 divergent | Live stopwatch while running + "done in"/"interrupted after" duration (replaces the removed /cost). |
| Welcome intro | 06-app-ui | `tui.intro` | 🔀 divergent | Coloured logo + model/branch + key hints at startup. |

## Slash commands (TUI)

| Command | Klaudia | Status | Notes |
| --- | --- | --- | --- |
| /help, /quit, /exit, /clear | `tui/tui.go` | ✅ done | |
| /model | `tui/tui.go` | ✅ done | |
| /goal | `tui/tui.go` | ✅ done | |
| /memory | `tui/tui.go` | ✅ done | |
| /mcp | `tui/tui.go` | ✅ done | |
| /stats, /status | `tui/tui.go` | ✅ done | |
| /allow, /deny | `tui/tui.go` | ✅ done | |
| /config, /agents, /context | `tui/tui.go` | ✅ done | Group A (render-only). |
| /compact, /cost, /add-dir | `tui/tui.go` + `agent.Loop.Compact` | ✅ done | Group B. |
| /plan, /doctor, /diff, /commit, /export | `tui/tui.go` + `doctor/` | ✅ done | Group C; /commit gated by a y/n confirm. |

## Auth & infra

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| API key / env credential | 03-providers | `api/auth.go` | ✅ done | |
| OAuth keychain session | 07-app-features | `api/auth.go` | ✅ done | macOS `security` CLI. |
| OAuth token refresh | 07-app-features | `api/oauth.go` | ✅ done | Workstream 1d; refresh on expiry, write-back preserves unknown fields. |
| Host sandbox confinement | — | `sandbox/seatbelt.go`, `bwrap.go` | ✅ done | Workstream 1a (divergent: JS had no host confinement). |
| Container sandbox | — | `sandbox/container.go` | 🔀 divergent | New capability: run/validate code in docker/podman. |
| Telemetry / analytics | 00/07 | — | ⛔ skipped | Intentionally not ported. |
| CLAUDECODE env vars | 08-entry | — | ⛔ skipped | Only referenced in JS + test-*.sh; Go never needed them. |
| Auto-updater | 00-runtime | — | ⛔ skipped | Distributed as a static binary; out of scope. |

## Skills

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Skill system (`.klaudia/skills/*.md`) | 07-app-features | `skill/` + `tools/skill.go` | ✅ done | Frontmatter skills as a Skill tool + TUI /<skill>. |

---

## Retire-JS readiness

All differential-testing dependencies on the JS tree are now cleared:

1. ~~**1b — stream-json partial deltas**~~ — ✅ done (`--include-partial-messages`
   emits JS-shaped `stream_event` lines).
2. The remaining step before removing the JS tree is a **final differential
   pass** comparing Klaudia vs `node dist/cli.js` on the golden cases under each
   ⛔/🔀 row, to confirm the divergences are deliberate.

Everything else is either ✅ done, a deliberate 🔀 divergence, or a 🔜 planned
post-parity enhancement that does not require the JS reference to build.

**Retirement is gated on explicit sign-off** — removing `src/sections/*.js`
(~571K lines) is irreversible from `main`. Plan: `git branch js-reference` to
preserve it, then drop the tree from `main` in one commit. Not done
autonomously.
