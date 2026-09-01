# Klaudia Parity Audit

Tracks the JS→Go port. The JS reference (`src/sections/*.js`, extracted from
Claude Code v2.1.66) was the golden reference for differential testing during
the port and has now been **retired to the `js-reference` branch** — the Go
binary is the product. This table is the record of what was ported and where it
deliberately diverges.

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
| Bash run_in_background + BashOutput / KillShell | 05-app-core | `tools/bash.go`, `tools/shells.go` + `sandbox` | ✅ done | Bash `run_in_background` returns a shell id; BashOutput reads new output incrementally (optional regex filter); KillShell stops it. Session-scoped store; all shells killed at session end. Works across all executors via `Executor.Argv`. |
| Code intelligence (LSP) | — | `lsp/` + `tools/lsp_tools.go` | 🔀 divergent | New capability with no JS analogue: Diagnostics/Definition/References via language servers detected on PATH + toolchain dirs (gopls/rust-analyzer/tsserver/pyright/clangd). Lazy-spawned, session-scoped, never downloaded. |
| TodoWrite | 05-app-core | `tools/todowrite.go` | ✅ done | |
| Task tools (Create/List/Get/Update) | 07-app-features | `tools/task*.go` + `tasks/` | ✅ done | |
| NotebookEdit | 05-app-core | `tools/notebookedit.go` | ✅ done | |
| AskUserQuestion | 05-app-core | `tools/askuser.go` | ✅ done | Interactive via `Asker` seam. The TUI always appends a "something else" option (the model writes the choices, so its framing can be wrong); picking it — or just typing — opens free text, and the answer goes back verbatim. |
| ExitPlanMode | 05-app-core | `tools/exitplan.go` | ✅ done | Via `Planner` seam. |
| Agent (Task / sub-agents) | 07-app-features | `tools/agent.go` + `subagent/` | ✅ done | Dynamic description from `subagent.Builtin()`. |
| Memory | 07-app-features | `tools/memory.go` + `memory/` | 🔀 divergent | Auto-memory + recall via `.klaudia/memory/`. |
| ToolSearch (deferred loading) | 07-app-features | `tools/toolsearch.go` | ✅ done | Per-turn `buildToolParams` filter + `Context.Reveal`. |
| SlashCommand (model-invoked) | 07-app-features | `tools/skill.go` | 🔀 divergent | Covered by the Skill tool (user-defined skills the model can invoke). |
| BrowserSearch / BrowserFetch / browser navigation | 03-providers / 05 | `tools/browsersearch.go`, `tools/browserfetch.go`, `tools/browser.go`, `browser/` | 🟡 partial | Local default tools use lazy Chrome + DDG/Google and rendered markdown; search can relaunch headed Chrome with persistent `~/.klaudia/browser/chrome-profile` for user-assisted challenge handling. Permission-gated (ask by default; denied in plan/dontAsk); the engine is session-owned and Closed on exit. Navigation waits for the DOM to settle (readyState + text-stable, bounded) so JS-rendered content is captured. Google parsing filters Google's own nav/account links (DDG remains the stable default). Anthropic server-side betas still available via `agent/webtools.go`. |

## Agent loop & streaming

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Agentic tool loop | 05-app-core | `agent/loop.go` | ✅ done | Emitter, per-turn tool params, Approver/Asker/Planner seams. |
| Provider abstraction | 03-providers | `api/provider.go` | 🔀 divergent | Multi-provider interface (Anthropic + OpenAI-compatible); JS was Anthropic-centric. |
| Anthropic Messages (Beta, streaming) | 02/03 | `api/client.go` | ✅ done | Streamed with an idle watchdog (`KLAUDIA_STREAM_IDLE_TIMEOUT`). |
| OpenAI-compatible Chat Completions | — | `api/openai*.go` | 🔀 divergent | Translation shim; SSE stream:true. Image tool_results translated to `image_url` content parts. |
| Prompt caching | 03-providers | `api/cache.go` | ✅ done | `cache_control` breakpoints on tools + system + a rolling conversation tail; usage surfaced as `cache_read/creation_input_tokens`. Disable with `KLAUDIA_DISABLE_PROMPT_CACHE`. |
| stream-json output (authoritative envelope) | 08-entry | `streamjson/` + `cli/envelope.go` | ✅ done | |
| stream-json partial deltas | 08-entry | `cli/partial.go` + `api/StreamSink` | ✅ done | Behind `--include-partial-messages`; emits JS `stream_event` lines. OpenAI shim synthesizes the sequence. |

## Sessions, compaction, persistence

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| JSONL transcripts | 07-app-features | `session/` | ✅ done | `~/.klaudia/sessions/<encoded-cwd>/` (honors `KLAUDIA_CONFIG_DIR`); reads legacy `~/.klaudia/projects/<encoded-cwd>/`; EncodePath matches JS K0A. |
| Resume / continue / fork | 07/08 | `session/` + `cli/` | ✅ done | Auto-resumes most recent project session by default; `--new-session` starts fresh; `-r/--resume`, `--continue`, `--fork-session` remain. |
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
| MCP reconnect/disconnect (`/mcp`) | — | `mcp.Manager` + `tui` | 🔀 divergent | Interactive `/mcp` picker; reconnect swaps the live session into the existing tool wrappers so a crashed server's tools resume. |
| MCP HTTP/SSE transports | 07-app-features | `mcp/mcp.go` | ✅ done | A server with `url` uses the streamable HTTP transport (or `type:"sse"`); `command` stays stdio. Custom auth headers are a growth point. |
| Built-in sub-agents | 07-app-features | `subagent/` | ✅ done | |
| Sub-agent tool allowlists (`Type.Filter`) | 07-app-features | `subagent/` | ✅ done | Explore/Plan are restricted to read-only Read/Glob/Grep; general-purpose gets the full toolset by design. |

## TUI & frontends

| Feature | JS ref | Klaudia pkg | Status | Notes |
| --- | --- | --- | --- | --- |
| Interactive TUI | 04/06 | `tui/` | 🔀 divergent | Bubble Tea (JS was React+Ink). Single-reader invariant. |
| Headless `-p` / `--print` | 08-entry | `cli/` | ✅ done | text / json / stream-json. |
| stream-json input (embedding frontend) | 08-entry | `cli/` + `streamjson/` | ✅ done | `--input-format stream-json`. |
| @ file autocomplete | 06-app-ui | `tui/tui.go` | 🔀 divergent | Tab-completes the trailing @<path> token (robust completion vs. JS live overlay). |
| Slash command type-ahead | 06-app-ui | `tui/tui.go` | ✅ done | Live suggestions as you type `/…`; Tab completes (unique → fill, many → common prefix). |
| Bracketed paste / drag-drop | 06-app-ui | bubbletea default | ✅ done | Bracketed paste on by default (multi-line pastes atomic). |
| Help bar / keymap / input history | 06-app-ui | `tui/tui.go` | ✅ done | Up/Down ring-buffer history, Tab completion, key hints; /help generated from one command table. |
| Scrollback | 06-app-ui | terminal-native (inline render) | ✅ done | Output is printed into the terminal's own scrollback via `tea.Println`; no alt-screen and no mouse capture, so scrolling, selection, search and tmux copy mode are the terminal's. `/search`, `/outline`, `/errors` add structured lookup on top. |
| Interrupt the model (Esc) | 06-app-ui | `tui/tui.go` + per-turn ctx | ✅ done | Esc cancels the in-flight turn (and any pending approval); loop-level test confirms cancellation propagates. |
| Elapsed-time indicator | 06-app-ui | `tui` + bubbles/stopwatch | 🔀 divergent | Live stopwatch while running + "done in"/"interrupted after" duration (replaces the removed /cost). |
| Welcome intro | 06-app-ui | `tui.intro` | 🔀 divergent | Coloured logo + model/branch + key hints at startup. |
| Markdown rendering | 06-app-ui | `tui` + glamour | ✅ done | Assistant answers stream raw, then render through glamour (headings, bold, syntax-highlighted code) on completion. |
| Throughput readout | 06-app-ui | `tui` | 🔀 divergent | "✓ done in 12.3s · 1.2k tokens · 98 tok/s" from real Usage data. |

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
| /config, /agents, /context | `tui/tui.go` | ✅ done | Group A (render-only); friendly permission labels. |
| /mode, /permissions | `tui/tui.go` + `permission.Mode.Label` | ✅ done | Interactive numbered picker (or direct set) to change permission mode mid-session. |
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
