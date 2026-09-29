# Upstream review: Claude Code 2.1.67 → 2.1.284

Assessment date 2026-09-29. Klaudia's fork point is **2.1.66** (`internal/version`).
Upstream is at **2.1.284**: 218 releases and about 5,800 changelog lines in the gap.

## How to read this

**What upstream publishes.** `github.com/anthropics/claude-code` does not contain
Claude Code's source. It holds:

- `CHANGELOG.md`
- `plugins/`: markdown agents, commands and skills
- `mods/`: TypeScript hook modules written against upstream's plugin engine
- `examples/`: settings, MDM and hooks examples

So there is no code to port. Every item here is a behaviour, re-implemented in Go
from a changelog line. That is how the language barrier plays out:

- **The changelog tells us which bugs to look for.** Upstream spent 218 releases
  finding edge cases in the same problems Klaudia has: stream errors, transcript
  shape, compaction, permission matching and MCP. Most of the value is checking
  whether Klaudia's port has the same bug, not adopting features.
- **JS-only bugs do not transfer.** Examples: `String.replace` `$`-patterns,
  lone surrogates, Bun proxy handling, Ink rendering, fd exhaustion. Go's
  `encoding/json`, `net/http` and `os` avoid these. Each chunk report lists
  what it discarded for this reason.
- **`plugins/` markdown ports almost verbatim** into `.klaudia/skills` or
  sub-agent prompts. **`mods/` does not port**: it is TS written against a
  hooks and plugin engine that Klaudia doesn't have.

**Method.** The changelog was split into five chunks. Each was triaged by an
agent that checked Klaudia's Go code. About 85–90% of entries were dropped as
out of scope: claude.ai and billing, gateway, IDE, Windows, Bedrock, Vertex and
Foundry, cloud and remote, telemetry, the plugin marketplace, Ink rendering,
hooks and the auto-mode classifier.

**Verification.** Items marked ✔ were re-checked by hand in this review. The
rest are the agents' findings, with file references, and have not been
reproduced.

Effort: **S** is under a day, **M** is a few days, **L** is larger.

---

## Tier 0: security gaps (do first)

| # | Gap | Where | Upstream refs | Effort |
|---|---|---|---|---|
| 1 ✔ | **Project config can raise its own privileges.** `./.klaudia/config.toml` overrides `permissions.mode` (including `bypassPermissions`), `trust.mode`, `sandbox.mode`, `baseURL`, `apiKey`/`apiKeyEnv` and `extraHeadersEnv` through `merge()`. A cloned repo can switch off the gates, or point the OpenAI-compatible provider at an attacker so the user's key is sent there. | `config/config.go:275-366` | 2.1.257, 2.1.251 | S–M |
| 2 ✔ | **Bash rules match only the first command.** `Prefix()` reads `Commands[0]`, name plus first non-flag argument. With `Bash(git status:*)` allowed, `git status && curl … \| sh` runs. A `Bash(rm:*)` deny misses `ls && rm -rf x`. Wrappers such as `sudo` and `env` are not stripped. Multi-word rules like `npm run test:*` never match. A parse error falls back to the raw string, which fails open. All five chunk reviews found this independently. | `tools/bash.go:81`, `native/bashparser/bashparser.go:139`, `permission/permission.go:193` | 2.1.72/77/98/113/145/163/214/216/223/246/257/268/282 | M |
| 3 ✔ | **In plan mode, an allow rule approves writes and commands.** `Check` runs deny, then bypass, then **allow**, then the intrinsic check. Plan mode's block exists only in the intrinsic step, so any matching allow rule skips it. | `permission/permission.go:250-262`, `tools/permissions.go` | 2.1.136 | S |
| 4 ✔ | **Project `.mcp.json` servers start without approval.** Every stdio `command` is spawned at startup, and again on hot reload. Opening a cloned repo runs its code. `${VAR}` expansion also lets a project URL carry secrets out. | `mcp/mcp.go:100`, `cli/root.go:916`, `mcp/watch.go` | 2.1.69, 2.1.238, 2.1.246 | M |
| 5 | **Path rules don't work as path rules.** Read, Glob and Grep send an empty specifier, so a `Read(~/.ssh/**)` deny is inert. Edit and Write send the raw `file_path`: no `~` expansion, no cwd resolution, no `**` globbing, and a symlinked path goes unresolved. Trust zones partly compensate. | `tools/read.go:126`, `glob.go:46`, `grep.go:53`, `permission.go:193` | 2.1.162/163/172/176/214/268/280 | M |
| 6 | **Files that grant execution are auto-writable** under acceptEdits and autonomous modes: `.git/hooks`, `.git/config`, `.klaudia/`, `.mcp.json`, `.envrc`, `.npmrc`, `.pre-commit-config.yaml`, `.vscode/tasks.json`. Writing one of these means code runs later. | `tools/permissions.go:9`, `trust/paths.go:171` | 2.1.78, 2.1.90, 2.1.160 | S |
| 7 | **The approval prompt truncates and doesn't sanitise.** Commands are cut at 220 runes, and zero-width and bidi characters aren't escaped, so a dangerous tail can sit out of view. | `tui/tui.go:2645` | 2.1.211, 2.1.223 | S |
| 8 | **Klaudia's own git probes obey repo config.** `git status` at startup and in `/diff` will run a repo-local `core.fsmonitor` or filter driver. | `tui/tui.go:557,2147,2172`, `prompt/prompt.go:153` | 2.1.265 | S |
| 9 ✔ | **The bwrap sandbox can read credentials.** `--ro-bind / /` leaves `~/.ssh` and `~/.aws` readable. Mask the trust package's credential directories with `--tmpfs`, and add seatbelt `deny file-read*` rules. | `sandbox/bwrap.go:27` | 2.1.187, 2.1.224 | S–M |
| 10 | **Injected text is not sanitised.** Invisible Unicode and markup-imitating tags pass through from memory, KNOWLEDGE.md and CLAUDE.md. Sub-agent results come back without a framing header. MCP tool descriptions are uncapped. | `prompt/`, `memory/`, `tools/agent.go:112`, `mcp/tools.go:140` | 2.1.84, 2.1.277, 2.1.280, 2.1.284 | S |

## Tier 1: correctness bugs (upstream fixed them; Klaudia has them)

### Streaming and retry

- **Mid-stream `overloaded_error` or `api_error` ends the turn**, and whatever
  text had arrived is discarded. `streamRetrying` retries only idle stalls
  before any output. Retry when nothing has been delivered yet. After partial
  output, keep it with an "incomplete" marker or continue the turn.
  `api/provider.go:122`, `agent/loop.go:265`. Upstream 2.1.199/246/257/284. M.
- **A stream that ends without `message_stop` is treated as a complete
  answer.** A proxy's clean close yields a silently truncated reply.
  `agent/loop.go:273,314`. Upstream 2.1.281. S.
- **Other `Accumulate` errors abort the turn raw**, instead of being reported as
  an interrupted response. `provider.go:198`. Upstream 2.1.281/284. S.
- **The OpenAI path never reads `Retry-After`**, although the comment at
  `api/openai.go:418` says it does. Five retries run out in about 15s.
  Upstream 2.1.94/98. S.

### Transcript sanitiser (`agent/sanitize.go`)

Each of these produces a permanent 400 on every later turn. S for all three:

- **Orphan `tool_result`**: its `tool_use` is gone. Only the reverse direction
  is repaired today. Upstream 2.1.69, 2.1.274.
- **Empty or whitespace-only text blocks** next to other content.
  `assistant.ToParam()` is appended without filtering. Upstream 2.1.92/229/251/277.
- **Tool names longer than 200 characters** remain in history.
  Upstream 2.1.281.
- ~~**Stale thinking blocks after `/model` switches.**~~ *Withdrawn.* Upstream
  2.1.152/156 stripped them, but the current Claude API guidance is the
  opposite: pass thinking blocks back unchanged when switching models. The
  API drops what the target model can't read, at no cost, and stripping them
  yourself can cause ordering or signature 400s. The related real risk is
  preserved thinking on Fable 5.1 and Opus 5.5, where editing earlier turns
  invalidates their blocks (Vogt WI-578).

### Compaction

- **Overflow dead end.** On "prompt too long", `autocompact` sends the same
  over-limit history plus a summary instruction. That request overflows too,
  returns `false` without saying so, and the session dies. Shrink the history
  and retry: drop the oldest turns, head-and-tail a huge first prompt. Also
  sanitise the summary request. `compaction/autocompact.go:16`,
  `agent/loop.go:396-472`. Upstream 2.1.85, 2.1.269, 2.1.281, 2.1.284. M.
- **No circuit breaker.** A failing auto-compaction retries every turn, and
  nothing detects "full again straight after compacting". Upstream 2.1.76/89. S.
- **The summary is capped at a hard-coded 4096 tokens** (`loop.go:444,458`),
  which is thin for long sessions. Upstream 2.1.69. S.

### Sessions

- ✔ **Auto-resume can pick another project's session.** `EncodePath` maps every
  non-alphanumeric character to `-`, so `/a/my_proj` and `/a/my-proj` share a
  directory. `MostRecent` never checks the recorded `cwd`. Klaudia
  **auto-resumes by default**, so this is worse here than upstream.
  `session/session.go:45,264`. Upstream 2.1.239. S.
- **A transcript line over 16 MB** (for example an image result) truncates the
  resumed history. `session.go:244`. Upstream 2.1.257/267. S.
- **Transcript write errors are discarded** (`_ = r.Record(...)`, `loop.go:1019`),
  so a full disk loses the session with no warning. Upstream 2.1.217. S.

### MCP

- **`textOf` drops all non-text content.** Image, resource and
  `structuredContent` results disappear, and an image-only result becomes `""`.
  Map images to `ResultImage`, which Read already supports. `mcp/mcp.go:454`.
  Upstream 2.1.113/128/136/268/283. S–M.
- **MCP results have no size cap.** `clampOutput` and spill-to-disk are wired
  only into Bash. The same gap applies to Grep and Glob, which also ignore ctx.
  `tools/bash.go:287`, `tools/grep.go:61`. Upstream 2.1.91/98/105/275. S.
- **Timeouts are missing.** There is no per-call timeout (`mcp/tools.go:104`),
  and startup connects servers one after another with no deadline, so one hung
  server blocks launch. Use `context.WithTimeout` per call and an errgroup for
  connect. Upstream 2.1.142/187/206/232. S.
- **`tools/list` is fragile.** Only one page is read (no cursor), and a failure
  is silently `continue`d, which looks like zero tools. go-sdk's `sess.Tools()`
  iterator paginates. Upstream 2.1.132/144/147/191. S.
- **Reconnect gaps:**
  - no HTTP→SSE fallback on a 4xx
  - no transparent reconnect-and-retry after a session error
  - no `/mcp reconnect all`
  - connection failures are not reported to the model
  
  Upstream 2.1.247/265/273/274/283/284. S each.
- **A `.mcp.json` that is a FIFO hangs startup.** Lstat it and require a regular
  file. Upstream 2.1.257. S.

### Tools

- **Write converts CRLF to LF** when overwriting a CRLF file. Edit already
  handles CRLF. `tools/write.go:83`. Upstream 2.1.77. S.
- **Writes are not atomic.** `os.WriteFile` truncates in place. Write to a temp
  file, fsync, then rename, keeping the file mode and resolving symlinks first.
  `write.go:83`, `edit.go:183`. Upstream 2.1.181. S.
- **Read problems:**
  - Rune-unsafe `line[:readMaxLineLen]`.
  - A line over 1 MB fails the whole read.
  - Images are typed by extension only, with no magic-byte check, zero-byte
    check or size cap. One bad image in history can make every later request
    fail.
  
  `tools/read.go`. Upstream 2.1.122/126/144/145/157/166. S for the text fixes,
  M for images.
- **Bash exit 1 is always an error.** For grep, diff, test and cmp, exit 1 means
  "no match". Counting it as an error feeds the failure-streak steering.
  `tools/bash.go:167`. Upstream 2.1.144. S.
- **Bash output buffers are unbounded** until clamped at the end, so `yes` can
  exhaust memory. `sandbox/sandbox.go:127`. Upstream 2.1.247/252/265. S–M.
- **A failed sub-agent loses its partial work.** `spawner.go:153` returns
  `"", err`. Upstream 2.1.199/200/246. S.

### Skills

- **Strict YAML frontmatter rejects `description: Triggers include: X`**, which
  many real SKILL.md files contain. A UTF-8 BOM also makes a file silently
  ignored. `skill/skill.go:182,215`. Upstream 2.1.69, 2.1.239. S.

## Tier 2: features worth considering

| Feature | Notes | Effort |
|---|---|---|
| ✔ **Project-instruction loading** | `loadProjectInstructions` (`prompt/prompt.go:170`) reads only `~/.claude/CLAUDE.md`, the git root and the cwd. It has no `@import` expansion, so this workspace's `CLAUDE.md`, which is just `@AGENTS.md`, is injected as a literal line. It also has no parent-directory walk (the workspace-root CLAUDE.md is missed), no `AGENTS.md` (upstream 2.1.277 and the `mods/agents-md` option matrix), no `.claude/rules/`, and doesn't strip HTML comments (2.1.72). High value for this estate specifically. | S–M |
| **Model table** | Add `claude-opus-5-5` (the upstream default Opus), `claude-sonnet-5-5` and `claude-fable-5-1`, and update the `fable` alias. `api/client.go:51-124`. | S |
| **Effort / thinking settings** | Klaudia has none. Upstream added adaptive thinking and `xhigh` effort. | M |
| **`fallbackModel` chain** | Retry once on a fallback model, and switch for the session on "model not found". Fits `api.Provider`. | M |
| **User-defined sub-agents** | `subagent.Builtin()` is fixed at 3 types. Loading `.klaudia/agents/*.md` would let the `plugins/*/agents/*.md` prompts (pr-review-toolkit, feature-dev) be dropped in. Also make `subagent_type` lookup case-insensitive (2.1.140). | M |
| **Skill niceties** | `${CLAUDE_SKILL_DIR}`, `disallowed-tools` frontmatter, `/reload-skills`. | S each |
| **`/goal` resilience** | Retry with backoff on API errors instead of ending the run (`cli/loop.go:104`). | S |
| **Sandbox options** | `failIfUnavailable`, `deniedDomains`, an opt-in memory cgroup, killing `setsid`-detached children (subreaper or cgroup). | S–M |
| **`--safe-mode` / `--restricted`** | Start while ignoring project config. Pairs with Tier 0 #1. | S–M |
| **MCP `alwaysLoad`, HTTP `headers` with `${VAR}`** | parity.md already lists headers as a growth point. | S–M |
| **LSP `workspaceSymbol`** | Adds to Definition, References and Diagnostics. | S–M |
| **Read-before-edit / staleness guard** | Klaudia has none. Upstream is loosening its own version, so treat this as a design choice rather than a port. | M |

**Content from upstream's `plugins/` directory:**

- **Port as skills or agent prompts (cheap, S):** `code-review`, `pr-review-toolkit`
  (6 reviewer agents), `feature-dev` (explorer, architect, reviewer),
  `commit-commands`.
- **Needs hooks, which Klaudia lacks:** `security-guidance` (pattern warnings on
  Edit/Write, plus an LLM diff review at end of turn), `hookify`, and the
  output-style plugins. The pattern-warning layer of `security-guidance` could
  instead be a small built-in check in `tools/edit.go`/`write.go`.

## Deliberately not transferring

- **Hooks, plugins, marketplace, mods engine.** These are Klaudia design
  choices. Revisit only if user extensibility becomes a goal: it would be an L
  project, and `mods/` shows how far upstream took it.
- **Parallel tool dispatch fixes** (sibling cancellation and so on). Klaudia
  runs tools one at a time.
- **zsh-specific rule bypasses.** Klaudia always runs bash.
- **Already handled in Klaudia:**
  - LSP shutdown, idle watchdog and h2 ping
  - interrupted tool calls on resume
  - long-path session hashing
  - MCP stderr bound
  - the context-overflow retry loop
  - Bash timeout clamp
  - job duplicate detection after compaction
  - symlinks in trust zones
  - `rm -rf "$(…)"` bulk detection

## Suggested sequencing

1. **Security batch (about 1 week).**
   - Tier 0 #1 (project config), #3 (plan mode), #6, #7 and #8. All are S and
     each is a self-contained test in `permission/`, `config/` or `tui/`.
   - Then #2 (per-command Bash rules): one design with #5 (path rules), sharing
     the matcher rewrite.
   - Then #4 (MCP approval).
2. **Transcript robustness (a few days).** The sanitiser trio, the stream
   `message_stop` and mid-stream retry, and the compaction overflow dead end.
   These are what kill long sessions. The e2e `FakeModel` can script each 400
   shape and stream fault, so each fix gets a hermetic regression test.
3. **MCP hardening.** Content types, result cap, timeouts, pagination, reconnect.
4. **Instruction loading and model table.** `@import`, parent walk, AGENTS.md,
   `.claude/rules`; new model IDs.
5. **Features as wanted.**

Afterwards, update `docs/parity.md` and bump the "reference" version note, since
the version string tracks the upstream reference.
