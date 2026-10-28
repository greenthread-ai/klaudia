# Changelog

Notable changes to Klaudia. The version tracks the Claude Code reference the Go
port mirrors (see `internal/version`).

## Unreleased

### Added

- **Language-server tools: hover, document symbols, rename preview and
  implementation**, loaded on demand through tool search so they cost nothing
  until used (#95).
- **`extraHeadersEnv` for OpenAI-compatible endpoints gated by headers.**
  A map of header name to the name of an environment variable (never a
  value in the file, as `apiKeyEnv`), sent on every request, so an endpoint
  behind a Cloudflare Access service token is reachable, with or without a
  key. An unset variable is a startup error naming it. User config only: a
  project config's is ignored with a warning, since it sends environment
  variables to your provider. From PR #10.
- **Grep takes context lines (`-A`/`-B`/`-C`), a file-type filter and a
  result limit** (#93), and Grep and Glob honour `.gitignore`, nested files
  included, while still reaching a hidden path the search names (#70).
- **Skills:** `$1`…`$N` arguments, `argument-hint` and `allowed-tools` in
  frontmatter, `${KLAUDIA_SKILL_DIR}`/`${CLAUDE_SKILL_DIR}`, a base-directory
  line in the result, prose frontmatter and a leading byte-order mark read
  (#19, #35).
- **Memory:** a `remove` operation; index lines no longer carry frontmatter;
  the recalled index is capped at 200 lines or 25 KB, as Claude Code caps its
  own (the top is kept, with a line saying where the rest is) (#77).
- **`-p` reads a piped prompt** and refuses an empty one (#60).
- **stream-json's result line carries the turn's token usage** (#12).
- **Notifications when Klaudia needs you.** A permission prompt, a question,
  a plan to approve, or a turn that ran 20s or more finishing now raises a
  desktop notification (iTerm2, WezTerm, Ghostty) or rings the bell
  (elsewhere and in tmux), only while the terminal is not focused. `[tui]
  notify` chooses (`auto`, a list of `bell`/`osc9`/`osc777`, `all`, `off`).
  A new `Notification` hook event fires at the same moments with the reason
  and message, for alerts sent elsewhere. Built on PR #96.
- **`klaudia --login`, and Claude Code's login on Linux.** `--login` stores
  an Anthropic API key (prompted without echo, or piped) in `credentials.json`
  under `~/.klaudia`, 0600, read after the environment variables. On Linux,
  Claude Code's `~/.claude/.credentials.json` is now read as the Keychain is on
  macOS, with an expired token refreshed and written back atomically. A flag
  rather than a `login` subcommand, since a positional argument is a prompt.
  From PR #100.
- **`${VAR}` in `.mcp.json`.** `${VAR}` and `${VAR:-default}` are expanded
  from the environment in a server's command, args, env, url and headers, as
  Claude Code does, so an HTTP server's token need not be a literal in the
  file. The project-trust card shows the raw templates and names the
  variables each server reads and the host a remote one sends them to. From
  PR #8, with headers added.

### Fixed

- **Read handles long lines, long files, binaries and bad images.** A
  single line over 1 MB (minified JavaScript, a data dump) failed the whole
  read with "token too long"; any length now reads, a long line cut at 2000
  bytes on a character boundary with a note of how much was left out. A
  read that stops at its line limit says so and gives the offset to continue
  from, with the file's total when it was read from disk, so the first 2000
  lines are not taken for the whole file. A binary file is reported by size
  instead of dumped as noise. An image is typed from its bytes rather than
  its name, and an empty one, one that is not an image, or one over the
  API's 5 MB or 8000-pixel limits is refused before it is sent, since a
  rejected image stays in the conversation and fails every later request.
  From PRs #46 and #72.
- **bwrap that is installed but cannot run is noticed at start.** Where
  unprivileged user namespaces are off (many containers, some hardened
  hosts), `sandbox.mode = "os"` found bwrap on the PATH and then failed
  every Bash call. It is now tried once at start and, if it cannot run,
  Klaudia falls back to local execution with a warning saying why. A new
  `[sandbox] failIfUnavailable = true` refuses to start instead, for any
  sandbox mode that cannot be used; a project config can set it but not
  clear it. From PR #52.
- **An unanswered stream-json control request no longer hangs the turn.**
  A `can_use_tool`, `ask_user` or `exit_plan` request waited only for the
  answer or cancellation, so a client that never implemented the control
  protocol, or stalled, left the agent wedged with no output and no exit.
  The wait is now bounded by `--ask-timeout` (default ten minutes, a
  judgement rather than a measurement; `0` waits forever), after which the
  tool is denied, or the question or plan fails, with a message saying the
  client did not answer. A late answer is dropped, not applied to a later
  request. From PR #7.
- **One overloaded response no longer ends a goal run.** `--loop` stopped
  at the first iteration that failed, so a 529 or a dropped connection
  three hours in ended the run. An iteration (and the closing summary) that
  fails transiently (provider unavailable after the fallback chain, a 429,
  a stalled or cut-off stream, a network error) is now retried after 30s,
  1m and 2m, with a line on stderr each time; anything else still stops
  the run at once. From PR #43.
- **An unknown model says which model, where, and what to do.** A typo'd
  id ended the turn with the raw provider body (`openai endpoint 404:
  {"error":…"model_not_found"}`). It now reads "Model not found: X isn't
  served by Y", says how to choose another, and quotes the provider. A 404
  that is not about a model points at the baseURL instead. `/model <id>`
  warns when the id is missing from the list an earlier `/model` fetched,
  and still applies it. From PR #79.
- **Claude aliases expand only on the Anthropic provider.** With
  `provider = "openai"`, `--model sonnet` reached the endpoint as
  `claude-sonnet-5` and was sized from the Claude tables (128000 output
  tokens). Another provider now receives the string as written, sized from
  what it reports or the floor, with a warning at startup or in `/model`
  when it is an alias. With no model set at all, the Claude default is
  still sent, for gateways that serve Claude's ids. From PR #80.
- **A message you queue mid-turn is shown where the model read it.** It
  left the queued hint when the turn took it in after a tool call and was
  written nowhere else, so the transcript showed the work it caused with no
  sign of the instruction (reported from a `/goal run`). It now appears as
  `› …` at that point, in the form you typed it (a paste stays a chip).
- **A picker opened mid-turn no longer starts a second turn.** `/mode`,
  `/mcp` and a `/model` list can open while Klaudia works, but closing the
  picker always went to idle, so the next Enter started a second turn
  alongside the running one; and Esc, meant to dismiss the picker,
  interrupted the turn. The picker now returns to the state it opened over,
  Esc closes it first, a turn that ends under it leaves it open, and an
  approval, question or plan from the turn closes it and says so. From PR
  #67.
- **A transcript that cannot be written is reported.** A full disk or a
  removed sessions directory used to lose the session without a word, found
  out only when `--resume` had nothing to resume. The first failed write in
  a turn is now a notice, in every frontend (stderr when headless). Under
  stream-json, a failing transcript no longer stops the message envelopes on
  stdout either. From PR #44.
- **`AskUserQuestion` is offered only when someone can answer.** A headless
  run or a sub-agent has no one to ask, so the tool is left out of its
  requests rather than offered and failing on every call. From PR #13.
- **A sub-agent that fails keeps what it found.** Its last completed reply
  goes back with the error, marked as possibly incomplete, instead of only
  "Sub-agent failed". From PR #42.
- **Compaction reads the conversation from the prompt cache.** The summary
  request was sent without the system prompt and tools, so it shared no
  cached prefix with the conversation and paid for the whole history at the
  moment it was largest; idle compaction, which exists to catch the cache
  while it is warm, got nothing from it. Summary requests now carry the
  conversation's prefix. Measured on Opus 5.5: 0 tokens read from cache
  before, the full 20.5k prefix after, at the summary's capped effort as
  well. From PR #66.
- **A reply that is cut off is reported as cut off, and kept.** A stream
  that a proxy closed without its final event, or that carried an
  overloaded or server error after it began, used to end the turn as if the
  half-built message were the whole answer, or with a raw error. Now, if
  nothing had been shown it is retried; if text had been shown it is
  reported as cut off and not retried (that would repeat what you read), and
  the text you saw is kept in the conversation, closed by a line saying it
  was cut off or interrupted, so the model knows what it said. A cut-off
  turn is not handed to a fallback model either. The OpenAI-compatible path
  does the same for a stream that closes early, keeps text it had streamed
  when the body read fails, and now honours `Retry-After`. From PRs #29 and
  #53.
- **Tool input is validated before the host gate and any approval** (#97),
  so a malformed call is refused as malformed instead of being shown to you
  first.
- **Files are replaced atomically, and Write keeps a file's CRLF endings**
  (#40).
- **Command output is bounded in memory while it is captured**, and a
  command that detached a process with `setsid` has that process stopped
  when it is cancelled on Linux (#41, #51).
- **Bash:** exit 1 from `grep`, `diff` and `test` is an answer, not an error
  (#49); a line is parsed before a self-backgrounded service is refused, so
  `&>` and names like `server` no longer trip it (#73); the non-interactive
  forms of terminal programs are no longer refused (#74); `git commit` flag
  clusters like `-am` are parsed rather than matched (#64).
- **The host gate catches flag-driven package installs** (pacman, rpm, dpkg,
  nix-env), `git config --global`/`--system`, `sudo pip install`, `sudo make
  install` and `yarn global`; `pacman -Sy` now asks too (#76).
- **Edit refuses an empty `old_string`** (#61), **memory adds new bullets
  above the linked-memory section** (#57), **a language server that never
  publishes diagnostics is not reported as clean** (#68), and **an MCP stdio
  server's stderr is forwarded** rather than discarded (#15).
- **A resumed session with an orphaned tool result or blank text can be
  sent again.** A tool result whose call was lost (an interrupted write, a
  compaction boundary) and blank text in a user message are each rejected by
  the API on every later request; the request sanitiser now drops them. Blank
  text in an assistant turn is left alone: the API accepts it back. From
  PR #26.
- **`/copy` no longer loses its clipboard escape** when the screen updates
  twice before the next draw: escapes for the terminal now go out on their
  own, not on the front of a frame the next one could replace.
- **`[input] enter = "newline"` takes effect.** The section was read and
  never merged, so the setting did nothing.

### Security

- **A repository's config can no longer loosen yours.** `.klaudia/config.toml`
  arrives with the repo, yet it could start Klaudia in `bypassPermissions`,
  take the sandbox off, add writable directories, widen the network
  allowlist, choose the browser binary or profile, or set `provider`,
  `baseURL` and the API key, which decides where your credentials are sent.
  A project config may now only tighten: `plan` mode, confinement where you
  chose none, no network, no mounted working tree, a narrower allowlist.
  Anything else is ignored with a warning naming the key and where to put it
  instead.
- **Approvals show the whole command, with nothing hidden.** Commands were
  cut at 220 characters, so a dangerous tail could be padded out of view,
  and control and invisible characters (an ESC or CR that redraws the line,
  zero-width spaces, bidi overrides) passed through. They are now shown in
  full with such characters as `\u{…}` escapes, on host-change cards too,
  and an approval repeats a command its tool line could only show in part.
- **Project knowledge is written only with your approval.** The model's
  writes to `.klaudia/KNOWLEDGE.md` (`add` with `scope=project`, or
  `promote`) now ask first and show the exact text, because it is read into
  every later session: a prompt injection that talked the model into writing
  "always run this first" would otherwise outlive the session that read it.
  A headless run refuses the write. The prompt now introduces the file as
  notes to check against the code, not "established facts". From PR #71.
- **Klaudia's own git commands no longer run a repository's programs.** A
  repository's `.git/config` travels with an archive or a vendored checkout,
  and git runs what it names: `core.fsmonitor` on every `status`, external
  diff and textconv drivers on every `diff`, hooks on `git worktree add`.
  Klaudia runs those commands itself (the branch in the prompt, ownership,
  `/diff`, checkpoints, sub-agent worktrees), so opening such a folder ran
  them. They now run with fsmonitor, hooks and diff drivers off.
  `/commit` and the goal loop's commits still run plain git, hooks
  included, because you asked for them. From PR #24; upstream Claude Code
  fixed the same class in 2.1.265.
- **Hidden text no longer reaches the model from files.** Project
  instructions (`CLAUDE.md`, `AGENTS.md`), recalled memory and knowledge, and
  MCP tool descriptions could carry instructions in invisible characters (tag
  characters that spell out text, zero-width and bidi characters) that a
  person reviewing the file would never see. They are stripped, ZWJ and ZWNJ
  kept for the scripts and emoji that need them; an MCP tool description is
  also capped at 2 KB. A sub-agent's result is introduced as its report, so
  text it quotes from what it read is not taken as the user speaking. From
  PR #30.
- **A session id cannot be a path.** `--resume` and an ACP client's
  `session/load` took the id into a file path unchecked, so `../..` reached
  outside the sessions directory. Ids are validated where they come in, and
  every path built from one keeps only its last element.

## v0.4.0 — 2026-10-28

### Added

- **`/rewind` can summarise up to a prompt** instead of going back to before
  it: everything earlier becomes a summary, and that prompt and what followed
  stay word for word. Files are not touched. It is recorded so a resumed
  session comes back the same way.

- **Session replay as regression tests** (`internal/replay`). A recorded
  session is driven through today's agent loop with the recording's model
  replies and tool results, so nothing runs and the result is deterministic.
  Every request the loop builds is checked for what the API would reject
  (orphaned or unanswered tool calls, consecutive assistant turns, blank
  text, a request over the context window), and every recorded tool call is
  checked against today's schema. A corpus of transcripts generated by the
  e2e suite runs in CI with golden snapshots of what the model is sent, so a
  change to how the conversation is assembled shows up as a reviewable diff;
  `KLAUDIA_REPLAY_DIR` sweeps a local session directory the same way.
  Messages typed while the model worked are replayed where they arrived,
  not mistaken for new prompts.

### Fixed

- **Sub-agents fall back too.** The `fallbackModels` chain covered only the
  main loop, so a 529 the parent survived by switching model still failed
  every sub-agent it started. They now get the same chain.
- **Release binaries are published again.** v0.2.0 and v0.3.0 were tagged
  but their release runs stopped at the test step, so neither has prebuilt
  binaries (`go install …@v0.3.0` was unaffected). Two tests passed on macOS
  and failed on the Linux runners: one committed without a git identity,
  which a runner does not have, and the screenshot check compared an image
  whose key labels depend on the OS it was generated on. Both now pin what
  they depend on, and the suite was run on Linux before this release.
- **`/rewind` after a compaction no longer cuts the conversation in the wrong
  place.** Its recorded prompts were positions in a history that `/compact`,
  autocompact and `/clear` replace; it now forgets them when that happens and
  says why, and "summarise up to here" moves the later ones to where they
  now are.

## v0.3.0 — 2026-10-27

### Added

- **Shift+Return inserts a newline** (and, with `input.enter = "newline"`,
  Ctrl+Return sends) on terminals that can tell those chords from Return:
  Klaudia now asks for the Kitty keyboard protocol and xterm's
  modifyOtherKeys, which Bubble Tea v2 parses. `/help` names the chord once
  the terminal has confirmed it, so Apple Terminal — where Shift+Return is
  still plain Return — is never told to press something that sends.
  `Option+Return`, `Ctrl+J` and the trailing `\` work as before.

### Changed

- **The TUI moved to Bubble Tea, Bubbles, Lip Gloss and Glamour v2.** The
  live region stays on Klaudia's own renderer: v2's was put through the same
  resize tests and stranded a frame on a fast narrowing, as v1's did, and lost
  an editor's output across `/open`. Lip Gloss v2 leaves colour downsampling
  to the writer, so scrollback and frames are downsampled to the terminal's
  profile (NO_COLOR included) by Klaudia. The input box sizes itself
  (Bubbles v2 `DynamicHeight`), which retires a port of the v1 textarea's
  wrap algorithm kept only to measure it. Glamour v2 links every Markdown URL
  itself; those links are removed so Klaudia's own (which honour the
  hyperlink setting) stay the only ones. Lip Gloss v2 expands tabs in
  multi-line text even when told not to, which would cost tabular tool output
  its columns, so chrome styles render such text a line at a time. Binary
  +1.4 MB.

### Fixed

- **Resizing the window no longer strands input boxes or erases history.**
  The previous fix predicted how far the terminal would rewrap Bubble Tea's
  frame, and only held for slow resizes: once the e2e emulator could resize,
  a drag with resizes 10–40ms apart (an ordinary drag) left stacked input
  boxes, a faster one erased lines of conversation, and on a terminal that
  does not rewrap (xterm) any narrowing erased everything visible. The live
  region is now drawn by Klaudia's own renderer, which keeps the cursor at the
  frame's top — where every terminal leaves it through a resize — and draws
  with autowrap off, so no prediction is needed. Frames are also wrapped in
  synchronized-output marks, which terminals that support them use to show
  each frame whole. The e2e suite now drags rewrapping and non-rewrapping
  terminals, changes the height and resizes mid-stream.

## v0.2.0 — 2026-10-27

### Highlights
- **The TUI reads like a review, not a log.** One line per tool call (reads
  batched), numbered edit hunks, a one-line resume card, a 21-line `/help`,
  paged and coloured `/diff`, honest context and token figures.
- **`/rewind`** goes back to before an earlier prompt, files and conversation
  together; **`/commit`** drafts its own message.
- **Long-running work**: background jobs report their exit (and, with
  `watch`, matching output) to the model in every frontend, `BashOutput` can
  wait instead of polling, logs rotate, and background and forked sub-agents
  work in the TUI, headless and stream-json.
- **MCP**: project servers ask before they start, HTTP `headers`, images and
  structured results, titled and multi-select elicitation, and a dozen fixes.
- **Operations**: OpenTelemetry tracing, a fallback model chain, `maxEffort`,
  `--restricted` for reproducible runs, `[sandbox] egress` (enforced on macOS),
  idle compaction.
- **The core is embeddable**: the agent loop and what it needs live in
  `pkg/`, with no dependency on Klaudia's own packages.

### Changed (breaking)
- **The core's advice is neutral unless the host words it.** `api.Hints`
  carries a host's names for its settings, its compact command and the
  sign-in whose token is often shared; `api.FriendlyError` uses neutral
  wording and `Hints.FriendlyError` the host's. The loop takes the same value
  as `agent.Options.Hints` (and `Spawner.WithHints`) for its two notes about
  the output-token setting. Klaudia passes `config.Hints`, so its messages
  are unchanged word for word.
- **The provider layer is `pkg/api`, and the core's leaf packages are in
  `pkg/`** (`permission`, `plan`, `schema`, and `tool` — the tool contract,
  aliased from `internal/tools`). The Anthropic client there is neutral: no
  Claude Code beta, `x-app` header, OAuth beta or billing block. Klaudia
  adds exactly what it sent before through the client's new `WithPrepare` /
  `WithRequestOptions` / `WithListOptions` hooks, in
  `internal/anthropicauth`, which also owns credential discovery
  (`ResolveCredential`). Live API tests (`KLAUDIA_LIVE_MODELS=1`) now take
  `ANTHROPIC_API_KEY` from the environment and no longer run by default —
  two of them used to call the real API with the Keychain login whenever
  one was found.
- **The agent loop no longer knows about Klaudia's product packages.**
  `internal/agent` now imports only `api`, `compaction`, `permission`,
  `plan`, `schema` and `tools` (it also imported `trust`, `hooks`,
  `subagent`, `worktree` and `session`). The seams are interfaces with
  Klaudia's implementations in the wiring: `agent.Gate` (the host gate moved
  wholesale to `internal/hostgate`, and `ApprovalRequest.HostChange` became
  `Detail`, read with `hostgate.CardOf`), `agent.Hooks`, `Spawner.WithTypes`,
  `Spawner.WithIsolator`, and `session.Messages` in place of
  `agent.MessagesFromEntries`. Behaviour is unchanged, except that `/trust`
  no longer counts a parallel-batched read-only call (a credential read, say)
  twice.
- **`--dangerously-skip-permissions` is removed.** It was a shorthand for
  `--permission-mode bypassPermissions`, and the docs and the `--loop` error
  still pointed unattended runs at it long after autonomous mode (the
  default) plus `--allow-host-changes` covered them. `bypassPermissions`
  remains — via `--permission-mode` or `/mode` — as the one, explicit way
  past the gate; it just no longer has a flag that reads like the normal
  route. `scripts/smoke.sh` uses the explicit form.
- **Environment settings the core read at runtime are gone.** They were
  process-wide, which an embedded loop cannot live with.
  `KLAUDIA_MAX_RETRIES` and `KLAUDIA_STREAM_IDLE_TIMEOUT` are now the config
  keys `maxRetries` and `streamIdleTimeout` (seconds); the error messages that
  suggested the variables now name the keys. `KLAUDIA_DISABLE_PROMPT_CACHE`
  and `DISABLE_COMPACT` / `DISABLE_MICROCOMPACT` / `DISABLE_AUTO_COMPACT` were
  debugging switches and have no CLI replacement; embedders get
  `api.Tuning.NoPromptCache` and `agent.Options.NoMicrocompact` /
  `NoAutocompact`. `api.New` and `api.NewOpenAIProvider` take an `api.Tuning`.
- **The host chooses where the core writes spills and job logs.** The agent
  loop, the tools and compaction no longer resolve `~/.klaudia` (or
  `KLAUDIA_CONFIG_DIR`) themselves. `tools.Spill`, `tools.Cap` and
  `tools.CapTo` are now methods on `tools.SpillStore{Dir}`, which reaches the
  loop as `agent.Options.Spill`, Bash as `tools.Context.Spill` and sub-agents
  through `Spawner.WithSpill`. `tools.NewJobStore(ctx, root, session)` takes
  the job-log root. The CLI passes the same `outputs/` and `jobs/` directories
  as before, so nothing moves for users; an embedder that passes none gets
  clamped output with no spill file and in-memory job logs.

### Added
- **`maxEffort`** in config caps every effort setting — `--effort`,
  `KLAUDIA_EFFORT`, `effort`, `[subagents] effort` and `/effort`, which
  refuses a level above it. An unset effort (the provider's default, often
  high) is sent as the cap for models known to take effort.
- **Egress control.** `[sandbox] egress = ["github.com", "*.npmjs.org", …]`
  routes every command through an in-process proxy that forwards to those
  hosts and refuses the rest with a 403 naming the host and the setting.
  Enforced under the macOS OS sandbox (the profile allows outbound traffic to
  the proxy only); advisory in local mode and under bubblewrap; not applied —
  with a warning — in a container or with `network = "none"`. `/doctor`
  shows which, and the hosts refused.
- **`--restricted`** for evals and reproducible runs: an empty config root for
  the process (no user config, global instructions or MCP, skills,
  approvals, session history or job logs, and nothing written back; nor
  `~/.claude` skills and `CLAUDE.md`), and no memory or hooks. Project files
  still apply.
- **OpenTelemetry tracing.** With `OTEL_EXPORTER_OTLP_ENDPOINT` (or the
  traces-specific variable) set, Klaudia exports GenAI-convention spans over
  OTLP/HTTP: `invoke_agent` per turn, `chat` per model request with token
  usage and finish reason, `execute_tool` per tool call, sub-agents nested.
  MCP tool calls pass the trace on as `traceparent` in `_meta`. `pkg/agent`
  depends only on the OTel API; with no provider the spans are no-ops.
- **Background sub-agents outside the TUI.** A headless `-p` run starts them
  in the background and does not end while one is working: their results go
  to the model mid-run, or as one more turn once they arrive, and the result
  line totals every turn. Over stream-json they report to the model with the
  next turn and to the peer as `system/agent_done`.
- **`/rewind`.** Pick one of this session's earlier prompts: every file
  Klaudia changed from that turn on goes back to how it was before it, the
  conversation is cut back to before the prompt, and the prompt returns to
  the input box. The plan is shown first, files you also edited are left
  alone, and the cut is recorded in the transcript so `--resume` stays
  rewound. Checkpoints now carry their turn, so two turns with the same
  label no longer merge into one `/undo` step.
- **Forked sub-agents.** The Agent tool takes `fork`: the child starts from a
  copy of the conversation so far (through the turn that made the call), so it
  knows what has been read and decided, and returns only its answer; the
  main thread is untouched. Tools are handed the conversation as
  `tool.Context.Conversation`, and a spawner opts in with `SpawnFork`.
- **`/doctor` says whether links are on, and why**: the override
  (`KLAUDIA_HYPERLINKS`), the config, the terminal it detected — or "terminal
  not recognised — KLAUDIA_HYPERLINKS=1 if it supports OSC 8".
- **Idle compaction.** When the conversation is at 85% or more of the
  compaction point, the TUI compacts it after four idle minutes — before the
  prompt cache expires, so the summary is cheap — instead of making the next
  turn stop for it. Skipped while you are typing; `idleCompact = false` turns
  it off. `/compact` and idle compaction now also refresh the status bar's
  context reading.
- **Fallback models.** `fallbackModels = ["sonnet", "haiku"]` in config, or
  `--fallback-model`: when the model is overloaded (529) or the provider keeps
  failing with a 5xx after retries, the run continues on the next model, with
  effort and the output cap refitted to it, and a notice names the switch.
  4xx errors and 429 quotas never fall back. (`api.IsUnavailable`,
  `agent.Options.FallbackModels`.)
- **Watched jobs.** `Bash` with `run_in_background` takes `watch`, a regex;
  output lines that match reach the model as they appear, so "start the
  server and tell me if it errors" works without polling. Reports are batched
  (first at once, then at most every 30s, 20 lines with a count of the rest),
  and stream-json peers get a `system/job_output` line.
- **Job exits reach the model outside the TUI too.** Headless `-p`, `--loop`
  and stream-json runs queue a job's exit in `agent.Inbox`, polled through
  `Turn.Interject`, so it arrives mid-run or with the next turn; a
  stream-json peer also gets a `system/job_exit` line when it happens. A
  background report injected mid-turn is now framed as news ("Background work
  you started has reported back") rather than as the user interrupting with
  an instruction.
- **Long-running background jobs.** A job that exits on its own is delivered
  to the model, as a background agent's result is (mid-turn as steering, or a
  turn of its own when idle, with the end of its output), so the model can
  start a 40-minute provisioning run, end its turn and be woken by it. A job
  stopped on request is not. `BashOutput` takes `wait_seconds` (up to 600)
  to wait for exit or a `filter` match in one call. `Jobs`, `BashOutput`
  and the jobs panel show run time and time since the last output. `/quit`
  and the double Ctrl+C name the running work that quitting stops.
- **`/commit` drafts the message.** A bare `/commit` asks the model, in one
  call outside the conversation (`agent.Loop.Complete`), for a message
  describing exactly what will be committed, in the style of the
  repository's recent subjects. It is shown, and committing still needs a y.
  `/commit <message>` works as before.
- **Stream-json opens with a `system/init` line**, as Claude Code's does:
  session id, model, cwd, permission mode, tools — and, on the bidirectional
  driver, `schema_version`, which versions `agent.Event` as a wire format
  (`agent.EventSchemaVersion`, with named `agent.Event*` type constants; new
  types and optional fields keep the version, renames and removals bump it,
  and a golden test pins the field names).
- **Background sub-agents.** The Agent tool takes `run_in_background`: the
  sub-agent starts, the tool returns its id at once, and the model carries on.
  In the TUI the agent is listed under the prompt with its latest step and
  counted in the status line; it lives as long as the session (Esc ends the
  turn, not the agent); and when it finishes its result reaches the model as a
  steering message mid-turn, or as a turn of its own when idle. `/stopjob
  agent_N` stops one. The runner is `agent.Background`, a field on
  `agent.Turn`, so frontends that do not supply one (headless, stream-json,
  ACP) run the sub-agent in the foreground and the tool says so.
- **System prompt guidance**: when to keep a TodoWrite list (three or more
  steps — the user watches it), report failures with their output and never
  claim something works that was not run, refer to code as `path:line` (which
  the terminal now makes clickable), and run servers in the background after
  checking Jobs. Plan mode's list of available commands is no longer Go-only,
  and names only commands the classifier actually allows (pinned by a test).
- **Plan runs.** Plan approval is now a picker: *Yes — run it* (`y`, as
  before), *Yes — and keep going until every step is done*, and *No — keep
  planning* (`n`), which asks what should change and sends it to the model —
  previously "n" could only refuse. "Keep going", and `/plan run [N]` later,
  run the goal loop over the approved plan: a model that stops with steps left
  is sent back to them, every step done triggers a verification turn, and the
  run ends with one completion block. `/plan stop` (or `/goal stop`, or Esc)
  halts it. During `/goal run` the live panel shows the spec's checklist and
  the iteration, instead of whatever plan was last on screen.
- **Running jobs are listed under the prompt**: name, age, port, and the
  command, oldest first, up to three (then "+N more · /jobs"). The status line
  only counted them. It ticks once a second while a job runs and not at all
  otherwise.
- **Clickable links.** URLs and paths to existing files in the transcript are
  OSC 8 hyperlinks, so Cmd/Ctrl+click opens them. A URL glamour wrapped across
  rows is one link carrying the whole URL (terminal detection opened the first
  fragment), and a relative `path:line` resolves against the working directory.
  On in terminals known to support OSC 8; `KLAUDIA_HYPERLINKS=0/1` or
  `[links] enabled` overrides. `[links] file` sends file clicks to an editor at
  the line (`vscode://file/{path}:{line}:{col}`). Screen only: the transcript
  that `/copy`, `/export` and `$PAGER` read is unchanged.
- **The plan is visible in every frontend.** The loop emits a `plan` event
  whenever the step list changes. The TUI, which showed nothing, now draws a
  live panel above the prompt (`Ctrl+T` collapses it; it hides once every step
  is done) and folds each `TodoWrite` result to `plan updated · n/m done`.
  stream-json clients get the event with a snake_case payload (`items`,
  `content`, `status`, `active_form`, `approved`, `path`, `rev`; a cleared plan
  is `"items":[]`). ACP's native plan is now built from the stored list rather
  than from `TodoWrite`'s input.
- **The session transcript now records the sizing each turn actually ran
  with**: the resolved model id, `maxTokens`, `contextWindow` and the source
  label behind each figure, on every assistant line. This is the gap the
  Opus 5.5 output-cap bug exposed from the other side — the saved transcript
  named the permission mode, the version, the git branch and the request id,
  and not one of the three numbers that decided what the turn could do, so the
  diagnosis rested on the user remembering which model they had picked. Written
  per entry rather than as a session header because `/model` changes the answer
  mid-session. Five `omitempty` fields, so a transcript whose sizing was never
  set is byte-identical to one written before they existed and `--resume` is
  unaffected.
- **A `klaudia` theme, and it is the default.** Sky blue and bronze on
  near-black. The startup colours were previously a palette no theme owned:
  `currentThemeID()` returned `"dark"`, which is not in `renderThemes`, so the
  theme picker marked nothing current and switching away from the default was
  a one-way door — there was no name to switch back to. The default is now a
  real entry, `defaultChromePalette` is derived from it rather than written
  out twice, and a test fails if the two ever disagree.

- **An approved plan is now a durable artefact.** `ExitPlanMode` used to return
  the sentence "Plan approved. Plan mode is now off — proceed with the
  implementation" and throw the plan away: it survived only as prose in the
  message history, where compaction could summarise it away and `--resume`
  never saw it. Approval now writes `~/.klaudia/plans/<session-id>.md` — Claude
  Code's shape: one file per session, outside the project, so two sessions in
  one repo cannot overwrite each other's plan and `--resume` keeps writing the
  same file. The user can edit it when the plan is wrong. The body uses goal
  mode's checkbox format, so `goal.CountUnchecked` *could* read it, but
  nothing calls it on a plan yet. The approval result also tells
  the model to record the steps with `TodoWrite` before starting the first one
  and to revise them as it goes, because approving a plan should begin working
  through a visible list rather than merely unlock the tools. A failed write is
  reported to the model but never costs it the plan: the list stays in memory
  and a read-only `.klaudia` degrades to a session with no artefact. Headless
  `-p` runs still have no approver, so a plan there is recorded but not
  approved and so not persisted.

### Changed
- **Starting a background job is one line** (`✓ Bash ./provision.sh ·
  started as provision.sh`); the how-to text stays with the model. The done
  line's tok/s now excludes the time tools were running, so a turn that
  waited 39s on a job no longer reads "7 tok/s", and it is left out when the
  model itself ran for under a second.
- **`/diff` pages and colours.** It printed the whole diff, uncoloured, into
  scrollback. It now uses git's colours (none under `NO_COLOR`), opens a long
  diff in `$PAGER` as `/last` does, and names untracked files, which `git
  diff` leaves out and which are often what Klaudia just created.
- **An approval shows the change once, and a Write shows what it changes.**
  An Edit awaiting approval printed its numbered hunk above the card and then
  again inside it as `replace "old" → "new"`; the card now adds only what the
  line above does not say. A Write over an existing file (`~/.zshrc`, say)
  previews the lines it changes, numbered with context and `+N -M`, instead
  of the new file's first three lines.
- **Context in the status bar only when it matters.** It is measured against
  the auto-compact point rather than the raw window: hidden under 50%, a
  quiet `ctx 62%` up to 80%, then amber `12% to auto-compact`, moved up the
  line so a narrow terminal keeps it.
- **A batch of reads is one line**: `✓ Read 3 files · main.go, main_test.go,
  go.mod`. Consecutive successful Reads are held until the next thing is
  written (shown in the live region meanwhile); a lone Read keeps its line
  count.
- **Edits show a real hunk.** An Edit printed all of `old_string` as `-`
  lines and all of `new_string` as `+` lines, with no position and the
  indentation collapsed. A one-word change inside ten lines read as twenty
  changed lines. Now only the lines that differ are marked, numbered where
  they sit in the file, with a line of context either side, indentation
  kept and tabs at four columns. The Edit tool reports the line its change
  starts on (`tool_result` events gain an optional `data` field carrying
  `tool.Result.Data`), so the numbers survive the next call rewriting the
  same file while this one renders.
- **`/help` is 21 lines instead of 60.** Commands are listed by group
  (Session, Work, Context, Shell, Find, Setup, and Skills when any are
  loaded), names only, followed by the keys. `/help <command>` (or a skill
  name) prints its usage and description. Typing `/` still filters every
  command with its description.
- **Resuming says so, in one line.** A resumed session with a clean working
  tree showed nothing, so the model carried on from history the user could
  not see. With anything to reconcile, the card was ten lines restating the
  mode. Every resume now opens with `↻ continuing the last session here · N
  messages · approvals start fresh · /clear to start over`, plus one line
  each for a goal, uncommitted work and stopped jobs when there are any.
- **One line per tool call.** A call's line used to be written when it started
  (`⚙ Read main.go`) and its result under it (`✓ Read · 0ms:` and eight
  lines of the file). It is now written once, when the result is in:
  `✓ Read main.go · 35 lines`, `✓ Glob **/* · 3 files`, `✓ Write main.go ·
  103 lines`, `✓ Edit main.go` over its hunk, `✓ Bash go test ./... · 7.2s`
  over its output. Durations appear from one second up, timed per call
  rather than matched by tool name. The live status line names the running
  call. The Agent tool still writes its line first, so its steps have
  something to sit under, and a call that stops for approval writes its
  line and diff before the card. Calls still running when a turn stops
  are listed as stopped.
- **Project MCP servers ask before they start.** A server in a project's
  `.mcp.json` or `.klaudia/.mcp.json` launched as soon as Klaudia started in
  the clone. It now waits for the same remembered approval project hooks get:
  the first message shows each server's command or URL (and env names), the
  answer is kept in `~/.klaudia/mcp-approvals.json` until the set changes, and
  a no lasts the session. Servers in `~/.klaudia/.mcp.json` start as before.
  `/mcp` lists held servers with their commands and can approve them before
  the first message (or after a no). Headless runs start project servers
  only with `--allow-host-changes`. The
  approval card's `hooks` flag became `repo_config` (`"hooks"` or `"MCP
  servers"`) on stream-json and ACP; `hooks: true` is still sent for hooks.
- **Newline is Option+Return (Alt+Enter off the Mac), or end the line with
  `\`.** The help and intro named Ctrl+J, which nobody remembers; it still
  works but is no longer advertised. A trailing backslash then Return now
  inserts a newline in any terminal with no setup, as in Claude Code — the
  one chord Apple Terminal (Option+Return needs *Use Option as Meta*, which
  costs `#` on a UK keyboard) and Windows Terminal (Alt+Enter is fullscreen)
  cannot swallow. Only at the very end of the input, and only on plain
  Return.
- **`/doctor` says how old the cached model list is.** The provider's
  figures beat the built-in tables through a once-a-day cache
  (`~/.klaudia/cache/`), so its age says how current the sizing is: a
  `model list` row reads e.g. "14 models, checked 31m ago", and past a day
  adds "stale, refreshes on the next start". No cache, no row.
- **The provider's model figures now beat the built-in tables, and `/model`
  sizes models the same way startup does.** The tables were only overruled for
  models they did not list, so a wrong entry was never contradicted at runtime
  (claude-sonnet-4-5 claimed a 1M window for months against the API's 200k).
  The provider's list is now cached for a day under `~/.klaudia/cache/`
  (fetching it costs about 300ms, measured) and a fresh answer wins for every
  model it lists; a stale cache refreshes in the background, so startup stays
  offline. `/model` used to size a model on its own, with the opposite
  precedence and without the config file — so a configured `maxTokens`
  silently stopped applying after any switch (reproduced: 64000 from the
  provider instead of the configured 4096). Both now go through `api.Resolve`.
  `/doctor` warns when the provider contradicts the table, so the stale entry
  gets fixed.
- **A dropped web search or fetch is a note to the model, not a notice to you.**
  The on-screen "an earlier web fetch was interrupted… re-run it if the answer
  depends on it" line is gone. It fired in sessions where nothing had been
  interrupted (see Fixed), and the model is the one that has to act on a lost
  result. It is still told: the dropped exchange is replaced with a note in
  the conversation, once, and that note is never re-added on later turns.
- **A tool call cut off at the output limit now says where that limit came
  from.** The refusal used to read `max_tokens=8192` and nothing else; under
  the Opus 5.5 stale-table bug that number was the unknown-model floor, and the
  note gave no way to tell it from the model's real cap. It now names the
  source (`config override`, `provider`, `model default`, the floor, or the
  new `provider rejected the requested cap` once the loop has adopted a figure
  from a 400). Only the two caps that are ours rather than the model's (a
  config override and the floor) add a hint that `maxTokens` in
  `.klaudia/config.toml` can raise it. `agent.Options` gained `MaxTokensSource`,
  filled from the resolved limits by the CLI and by sub-agent spawning. The
  loop's own default path now goes through `api.ResolveLimits` so the number
  and its label come from one lookup.
- **The four `Task*` tools are retired, and `TodoWrite` is backed by the new
  `pkg/plan` store.** Klaudia had two parallel task systems, both
  registered and neither authoritative: `TodoWrite`'s whole-list store, and
  `TaskCreate`/`TaskGet`/`TaskList`/`TaskUpdate` over `internal/tasks`. Nothing
  read the second one — not the TUI, not stream-json, not ACP; it was
  registered and given label-table entries, and that was the whole of its
  existence. The first was barely better off: it was constructed *inside*
  `DefaultRegistry` and handed to nobody, so the only frontend that rendered it
  was ACP, and it did so from the tool input rather than from the store. The
  store now belongs to the caller (`tools.WithPlanStore`), which is what lets a
  frontend show the plan at all.
- **The default model is now `claude-opus-5-5`** (was `claude-opus-5`), and the
  short aliases follow: `opus` → `claude-opus-5-5`, `sonnet` →
  `claude-sonnet-5-5`, `haiku` → `claude-haiku-5-5`, `fable` →
  `claude-fable-5-1`. Full ids still pass through unchanged, so
  `--model claude-opus-5` continues to pin the older generation.
- **Stale model tables are now a test failure rather than a silent downgrade.**
  `TestModelTablesAgreeWithEachOther` runs offline and requires every sizing
  entry to exist in all three tables with the paired values (1M→128k,
  200k→64k), in both directions, including every alias and `DefaultModel`;
  it is what caught the Sonnet 4.5 context error above.
  `TestLiveModelTablesMatchProvider` and `TestLiveMaxTokensTooLargeWording`
  (both `KLAUDIA_LIVE_MODELS=1`) check the tables against `GET /v1/models` and
  re-measure the over-cap 400's wording, the latter because the retry parses
  that message and a reword would silently switch the recovery off. The live
  test reports what to add, not just that something is wrong.
- **chromedp v0.16.0 → v0.21.0** (and cdproto to v0.158.3), which is an API
  rewrite, not a bump: actions are generic (`Action[T]`), `Run` takes one
  action and returns its value, `Location`/`Title` return strings instead of
  filling pointers, and the remote allocator moved to `chromedp/remote`. Taken
  deliberately — the intermediate releases do not build either, so the break
  predates the fix and there was no cheaper step. Upstream now handles the four
  DOM events that used to be logged as errors on every ad-carrying page; the
  local filter in `internal/browser/log.go` stays, because the hand-written
  event switch it works around is still hand-written and cdproto is still
  generated. Headed Chrome now launches with `VisibleWindow` rather than
  `Flag("headless", false)`, which also drops `hide-scrollbars`, `mute-audio`
  and the automation infobar — the only reason Klaudia ever runs headed is a
  search challenge a human has to solve, and that is a window they have to be
  able to use. The browser package now has live Chrome tests
  (`internal/browser/live_test.go`, skipped with `-short` or without Chrome),
  because the whole port compiled on the first attempt and compiling says
  nothing about whether a navigation still ends with the page in hand.
- **CI actions moved to their current majors**: `actions/checkout` v4 → v7,
  `actions/setup-go` v5 → v7, `goreleaser/goreleaser-action` v6 → v7. All
  three are node24/ESM migrations with no input changes — v7 of
  goreleaser-action still takes the same `version` and `args`. The one
  behavioural change that matters here is in checkout v6, which moved
  persisted credentials out of `.git/config` into `$RUNNER_TEMP`; the release
  job pushes with those credentials, and the coupling is now commented at the
  checkout step so a future `persist-credentials: false` is not mistaken for
  hardening.

### Fixed
- **Security updates.** Built with Go 1.26.10 and `golang.org/x/net` v0.60.0:
  govulncheck found 12 reachable standard-library and x/net advisories under
  Go 1.26.6 (net/http, crypto/tls, crypto/x509, mime/multipart,
  html/template); none remain.
- **Job logs are bounded.** A log grew without limit until the weekly prune,
  and `BashOutput` read everything since its last call into memory. Logs now
  rotate at 64 MiB (one previous file kept) and a read returns at most the
  newest 4 MiB, saying how much was skipped. Background agents show "quiet
  Xm" in the panel, as jobs do.
- **Inline code copies as itself.** The colour themes padded it with a space
  either side, which pasted as " go test "; without colour it rendered as
  bare words. The padding is gone (the tint stays), and without colour it
  keeps its backticks.
- **A resumed session tells the model which jobs stopped.** The resume card
  said so to the user, but the model's history still read "Started job
  provision (bash_1)", so its first move was to read an id that no longer
  existed. The first turn after a resume now carries a note naming them.
- **The status bar's numbers are real.** "ctx N%" estimated the message
  history alone, without the system prompt and tools, and read "ctx 0%" on a
  1M window. "N tokens" counted only the uncached input, a small tail under
  prompt caching ("1.4k tokens" for a turn that sent 60k). Usage events now
  carry `context_tokens`, the prompt each call actually sent: the context
  share comes from it (and a share under 1% reads "<1%"), and the token
  count includes cache reads and writes, as `/stats` now says. `/context`
  calls the top level "(project root)" rather than `*`. The turn's done line says
  "1.2k written" for its output tokens, so it no longer reads as a second,
  smaller total beside the status bar's.
- **Markdown spacing.** A code block or list at a streaming chunk boundary
  sat between two blank lines: glamour pads blank lines with spaces, so
  trimming newlines kept them. Rendered blocks now lose their visually blank
  edges. Tables are sized to their content instead of the full width.
- **Parallel tool calls print in order.** When the first call of a parallel
  batch finished while the second was still running, the second's result
  printed above its own `⚙` line (its buffered tool_use was left behind).
- **The turn summary reads compound commands.** `cd /abs/project && go test
  ./... 2>&1 | tail` was reported as a run scoped to `/abs/project`, with a
  "Not verified — the full tests" note under a full run. The check's own
  segment is read now, and redirections are not targets. The Bash line also
  drops a leading `cd <working directory> &&`, which hid the command.
- **The turn summary counts only checks run after the last edit.** A test
  run before the change was reported as verifying it, and with no check at
  all the footer said "nothing was run" even when commands had run. It now
  says "nothing was run after the last edit" or "no build or tests were run".
- **The interrupt notice follows the text it interrupted.** Esc wrote
  "⊘ interrupting…" to scrollback at once, above the partial reply still
  being streamed. The live view says "cancelling…" until the turn stops, and
  the transcript gets the partial reply and then one "⊘ interrupted after …".
- **`/context` lists only files that exist.** A path was noted when a tool
  call started, so a Read of a file that does not exist showed up under
  Recently inspected and Active. Paths are now noted only when the call
  succeeds, and a file deleted since drops out. The count reads "1 file".
- **MCP fixes** (from an audit; no behaviour changes beyond the bugs):
  tool names that break the API's `[A-Za-z0-9_-]{1,64}` (dots, long names)
  are rewritten to fit instead of failing every request once revealed;
  `/mcp` Disconnect is no longer undone by the model's next call; legacy-SSE
  servers survive `/mcp` reconnect and auto-revive (the handshake timeout
  context was killing their stream); tool and resource lists read every page;
  startup and config-reload connects time out (60s) instead of hanging; and a
  reload no longer orphans a session a revive installed meanwhile.
- **MCP tool schemas keep their `$defs` and top-level unions.** Only
  `properties` and `required` reached the API, so a server built on Pydantic
  or zod — whose properties say `"$ref": "#/$defs/X"` — sent refs to nothing.
  `$defs`, `definitions`, `anyOf`, `oneOf` and `allOf` now travel with them;
  built-in tool requests are byte-for-byte unchanged.
- **MCP tool results keep more than their text.** Every non-text block was
  dropped, so a screenshot tool answered with an empty string. Images
  (PNG/JPEG/GIF/WebP) now reach the model as images, from tool calls and
  from `ReadMcpResource`; embedded text resources are included; a result
  with only `structuredContent` sends its JSON; audio, binary blobs and
  resource links are named in one line rather than vanishing.
- **`/mcp` Reconnect of a server that failed at startup gives it tools.** It
  connected, but the registries were built while it had none, so it stayed
  useless until a restart. Reconnect now rebuilds them, as a config reload
  does. `ToolSearch` also searches only the tools it can load — it used to
  offer back the built-ins already in every request.
- **MCP elicitation reads the whole form schema.** Titled enums (`oneOf` /
  `anyOf` of `const` + `title`) offered no choices and took any text;
  multi-select (`array` of enum items) and `default` were ignored. Titles are
  now the choices and the `const` is sent; a multi-select takes a
  comma-separated list checked against `minItems`/`maxItems`; a default is
  shown and can be accepted. A choice literally named Skip or Cancel is now a
  value, and the escape rows are reworded for that field.
- **A silent MCP server no longer hangs tool listing.** Listing a connected
  server's tools or resources had no deadline, so one server that stopped
  answering stalled startup, config reload, `/mcp` and reconnect. Each
  server's listing now gives up after 15s and contributes nothing.
- **HTTP MCP servers get their `headers`.** `.mcp.json` `headers` (e.g.
  `Authorization`) were decoded and dropped, so a server needing a token
  refused every request with nothing pointing at the config. They are now
  sent on every request, over streamable HTTP and SSE.
- **One broken `.mcp.json` no longer hides the others.** A file that failed to
  parse stopped loading at that point, so a typo in a project's file took the
  global servers with it. It is reported and skipped; the rest load.
- **Read-only MCP tools work in plan mode, and so in Explore and Plan.** Those
  sub-agents run in plan mode and are handed MCP tools marked read-only, but
  plan mode refused every MCP call, so they could never use them. Plan mode
  now allows MCP tools marked read-only (`readOnlyHint` or `.mcp.json`) and
  refuses the rest, as it does for shell commands.
- **`KLAUDIA_CONFIG_DIR` moves everything.** `hooks.json`, `skills/` and the
  global `AGENTS.md` were still read from `$HOME/.klaudia`.
- **Help text**: `/commit` said it stages all changes (it commits the files
  Klaudia changed, or what you staged); `--loop` said it needs
  `--dangerously-skip-permissions` (autonomous, the default, is enough).
- **Sub-agents know the project.** A sub-agent's system prompt was its
  one-paragraph role and nothing else — no working directory, date or
  platform, no AGENTS.md / CLAUDE.md, no security clause, no host-change
  rules. It now gets all of them, with the environment for its own directory
  (an isolated checkout's, not the parent's).
- **`/model` no longer leaves the old model named in the system prompt**,
  which was built once at startup; the model line is now added per turn.
- **OpenAI-compatible providers: steering and interrupted prompts.** A user
  message's text blocks were concatenated with no separator ("go slow" +
  "hello again" → "go slowhello again"); they are now separated by a blank
  line. And text sent alongside tool results (a message typed mid-turn) was
  placed before the `role:"tool"` messages, which Chat Completions requires
  to follow the assistant's `tool_calls` directly; tool results now come first.
- **A question no longer takes your next message as its answer.** Typing at
  an `AskUserQuestion` used to open free text seeded with the keystroke, so a
  message typed ahead while the model worked answered whatever question
  appeared under it. The options are now a live picker like `/model`'s: ↑↓
  (or Ctrl+P/N) and Enter, digits still pick, "Something else" opens free
  text. Typing is ignored and explained, and the Enter that ends typed text
  does not confirm. Scrollback keeps the question and the answer only.
- **Paths on screen are relative.** Tool headers and result previews show
  paths under the working directory relative to it, and others under the home
  directory as `~/…`; `/context` shows the working directory under `~`.
  Display only — the model, `/last` and the transcript keep absolute paths, and
  OSC 8 links still resolve.
- **Long lines wrap between words**, with continuation rows keeping the
  line's indent, instead of splitting words at the last column. The startup
  hint fits in 80 columns.
- **Microcompact no longer re-elides its own placeholders every turn.** It
  never skipped results it had already elided. While the placeholder was one
  fixed string that was harmless; since placeholders carry a preview and a
  spill path (6 October), re-pricing the old ones without their path cleared
  the savings floor once a session passed ~1,180 elisions. Every turn after
  that rewrote every placeholder: a new spill file each (~1,200 per turn,
  measured), a placeholder nested inside a placeholder, "elided 1208… 1209…"
  with a negative saving — and each original reachable only by one hop per
  turn since it was elided, so in practice lost. Already-elided results are now
  skipped, and the savings estimate allows for the path.
- **Two different commands failing silently no longer read as a wedged shell.**
  A Bash failure with no output is just `[exit code 1]`, the same text for
  every silent failure (grep with no match, a failing test). Two in a row
  looked like one error across different inputs, and the repeat-error breaker
  refused the next command — the `git status` the model was running to see
  what had landed. A bare exit status is now compared together with its
  command: the same command failing repeatedly is still stopped.
- **Plan mode no longer asks about `cd`, and says why when it does ask.** The
  mutation classifier had no entry for `cd`/`pushd`/`popd`, so the commonest
  research line there is — `cd <repo> && grep … | cut …` — came back as
  "cannot classify" and prompted for permission, although every other part of
  it was read-only (reported from a goal session in another repo). Moving
  directory writes nothing; a redirect or mutating program after it is still
  judged on its own. Separately, a Bash permission prompt showed the command
  but not the reason, which the classifier always supplies; it now shows both.
- **The per-tool display maps are checked against the real tools.** ACP's
  title, kind and location maps and the TUI's header-summary map are keyed by
  tool name and point at input fields, and nothing verified either. ACP still
  carried `WebSearch` and `WebFetch`, which no tool is called; they are
  removed. A test now requires every key to be a real tool
  (`tools.BuiltinSchemas`, plus `mcp.ResourceToolSchemas`) and every field to
  be one that tool's schema declares.
- **Enter in the `@` file menu takes the file instead of sending.** The menu
  handled `↑`/`↓`/`Esc` but not Enter, which fell through to submit: the
  half-typed prompt went out with its partial `@alp` reference, and the menu
  stayed open over the reply. Enter now inserts the highlighted path, closes
  the reference with a space, and closes the menu; the next Enter sends.
- **A successful web fetch is no longer reported as dropped, and no longer
  makes the model re-ask questions.** When a fetch ran alongside a local tool,
  its result arrived at the start of the next assistant message, and the live
  loop added that message to history with `ToParam()` alone. That keeps a
  healthy `web_fetch` result in a form the repair code reads as corrupt, so it
  dropped the fetch and the turn it came in, printed "an earlier web fetch was
  interrupted before it returned…" in a session where nothing had been
  interrupted, and rewound the conversation far enough that a goal-setting
  session asked the user the same `AskUserQuestion`s three times (measured: one
  fetch delivered in 9 assistant messages). The live history now goes through
  the same JSON round-trip a resumed session rebuilds from, which is also why
  replaying the transcript never reproduced it.
- **Sessions damaged by the bug above can be resumed again.** Each time the
  fetch was dropped the API delivered it again, and the transcript kept every
  copy — one session holds one `web_fetch` call and ten results. Resuming sent
  them all, and the API rejected the second with `unexpected tool_use_id found
  in web_fetch_tool_result blocks`, so the session could not continue at all.
  Sanitising now sends only the first result for each server-tool call.
  Verified live: that session, resumed from a scratch copy, answers again.
- **`--resume` no longer forgets everything since the last compaction.** A
  compacted session was resumed from the summary file alone, and every entry
  recorded after it was dropped — measured on one restart as 572 entries and
  80 minutes of work, which the resumed session could not remember doing.
  Each compaction (auto or `/compact`) now writes a `compact_boundary` line
  carrying the summary into that conversation's own transcript, and resume
  rebuilds the last summary plus everything after it. Older transcripts are
  cut at the summary file's `_Compacted <ts>_` stamp, so sessions compacted
  before this change recover too. The seeded summary also ends with an
  explicit closing line, since the next prompt is merged into the same
  message: a summary ending `**Goal m` reached the model as
  `**Goal malright…`.
- **A sub-agent can no longer overwrite your plan.** The plan store was one per
  process, wired into the registry every conversation shares. A
  general-purpose sub-agent gets every tool, so its own `TodoWrite` replaced
  the user's step list — and after approval, rewrote the plan file. The store
  is now per conversation (`agent.Turn.Plan`, passed to the tools as
  `tools.Context.Plans`); each sub-agent gets a private in-memory one, and each
  ACP session its own store and its own `~/.klaudia/plans/<session-id>.md`
  instead of sharing the process's.
- **ACP no longer shows a plan the tool rejected.** It posted the plan from
  `TodoWrite`'s input, which arrives before the tool validates anything; a
  call refused for, say, two in-progress steps still drew its list in the
  editor. The plan now comes from the store.
- **Plan mode no longer refuses `2>/dev/null`.** The mutation classifier
  treated every output redirection as a file write, so `grep … 2>/dev/null`
  was refused during a real planning session. Redirects to the pseudo-devices
  (`/dev/null`, `/dev/stdout`, `/dev/fd/*`, …) now count as writing nothing;
  the list is shared with the host gate, which had already fixed the same
  false positive. `/tmp` stays a write in plan mode (it is a real file), and
  a target is cleaned before matching so `/dev/fd/../../etc/x` cannot pass as
  a descriptor.
- **A web search run alongside a local tool no longer rewinds the conversation.**
  When the model calls `web_search`/`web_fetch` and a client tool in one batch,
  the API returns the search result at the start of the *next* assistant
  message. The pre-send repair read each half of that split pair as an orphan,
  dropped them, and — because that message carries thinking and cannot be
  edited — dropped the whole turn along with the reply to it. The history then
  stood where it was before the result arrived, so the same search came back
  and the model repeated itself. Measured from transcripts: one search
  re-delivered 622 times over six hours, and an `AskUserQuestion` asked four
  times with the user's answers discarded each time. Measured live: the API
  accepts the split pair as-is, so it is now left alone.
- **A compaction summary that runs out of room is no longer accepted as the
  whole history.** Both `/compact` and autocompact requested a fixed 4096
  output tokens and never checked the stop reason. Measured: a real session's
  summary ended mid-word ("**Goal m", 11,317 bytes) and replaced its history,
  losing the user's answers to questions the model then asked again. The
  summary now runs at the session's resolved output cap, and one that hits it
  is refused with a notice; the full history is kept.
- **Plan mode now gates mutation rather than execution, so research works.**
  It refused *every* Bash call, and executing is how you research: `Read`,
  `Glob` and `Grep` cover files, but `git log`, `git diff`, `go doc` and
  `go list -m` have no tool equivalent at all. Measured, a planning session in
  this repository was refused all of those and shipped a plan carrying the
  caveat that it had not looked at what was on the branch it was planning
  against — the mode had made checking impossible. The axis was wrong, not the
  strictness: the host gate already stops changes to this machine in every
  mode, so what plan mode uniquely protects is the user's files. A command is
  now classified by `trust.InspectCommand` and gets one of three verdicts — it
  only inspects and runs silently, it is known to write and is denied with a
  message naming what it would have changed, or it cannot be determined and
  *asks*. The third verdict is load-bearing: the classifier is an allowlist,
  and one that refuses what it has not got round to classifying is this bug
  with a longer fuse. Redirections disqualify a line whatever the program
  (`echo` inspects; `echo x > main.go` does not), every component of a pipeline
  or `&&` chain has to clear the bar on its own, and `bash -c '…'` is read
  inside. `go build` and `go test` are denied: `go build ./cmd/klaudia` drops a
  binary in the working directory and `go test` runs arbitrary code. See
  `docs/plan-mode.md`.
- **The `Explore` and `Plan` sub-agents can run commands.** They were held
  read-only by being denied Bash, which denied them the same `git log` and
  `go doc` the main loop was missing, so delegating research was no way round
  it. They now carry Bash and are held read-only by mode instead — a stronger
  guarantee than a short tool list, and a far less crippling one. Not applied
  under `bypassPermissions`, which means the user has taken
  responsibility for the session. They also stop taking a `git worktree`: a
  child that cannot write has nothing to isolate.
- **Approving a plan no longer leaves the first command refused by the plan
  mode you just left.** A permission refusal was recorded as a tool *failure*,
  and plan mode's message is byte-identical whatever the command was — so two
  refused commands looked to loop-breaker B like one error shape across
  different inputs, which is the environment-is-wedged signature. The second
  refused command latched Bash for the rest of the turn. After `ExitPlanMode`
  flipped the session to autonomous the next call never reached the permission
  check at all: the breaker answered it with a quote of the plan-mode error the
  session had just escaped, wrapped in "Bash has failed with the SAME error 2
  times in a row", blaming the model for a refusal the harness had generated.
  The exclusion already existed for host-gate refusals with reasoning that
  applied verbatim; permission denials were never added to it. They are now
  kept out of *both* breakers rather than just the shape streak, because unlike
  a host-gate refusal a permission decision is routinely revoked mid-run — by
  an approved plan, or by `/mode` — through a route the loop never observes, so
  there is nothing to hang a counter reset on. See `docs/plan-mode.md`.
- **Fetching a page no longer waits for the load event, which on an ad-carrying
  site can never arrive.** `chromedp.Navigate` returns when every subresource
  has finished; a third-party ad or tracker is free never to finish. Measured
  cold against Chrome 152: `theguardian.com` had its full text 650ms after the
  navigation committed and had still not fired load 60 seconds later, and
  `arstechnica.com` fired it at 10.8s with its text complete at 1.25s — so
  fetching Ars failed on the 30s timeout every time, having been readable for
  29 of them. Navigation now commits via `Page.navigate` and the settle poll
  decides when the page is worth reading. Ten cold fetches, before → after:
  Ars `FAIL 30.4s` → `3.2s`, BBC `4.6s` → `2.7s`, Wikipedia `6.2s` → `4.3s`,
  Guardian `2.5s` → `1.7s`.
- **A page that is briefly blank is no longer captured blank.** The settle
  poll accepted two consecutive agreeing measurements, and `readyState` reaches
  `complete` with an empty body on sites that then render: measured,
  `stackoverflow.com/questions` did it 700ms in, so a fetch returned
  Cloudflare's 16-byte "Just a moment..." page instead of the questions —
  plausible-looking, and from a site that was neither slow nor down. Emptiness
  now never counts as settled, bounded by a 3s grace for pages that really have
  no text. The poll also accepts `interactive`, not only `complete`, because
  the Guardian never reaches `complete` at all.
- **A release rejected by branch protection no longer leaves a tag behind.**
  `scripts/release.sh` creates the tag locally and then pushed two refs
  non-atomically, and a protected branch is declined *per ref*: measured
  against a bare repo whose `update` hook declines `refs/heads/main`, the
  remote took `v0.1.1` and refused the changelog commit. The script exits
  non-zero and the publish step is skipped, so nothing is built — but the tag
  is left on the remote pointing at a commit that is not on `main`, and a tag
  pushed by CI starts no workflow run, so nothing fails visibly either. The
  push is now `--atomic`: the remote rejects both refs or neither.
  `docs/releasing.md` claimed this case "stops before tagging"; it did not.
- **Opus 5.5 was asking for 8192 output tokens against a cap of 128000.** A
  real session reported `This tool call was cut off before its arguments were
  complete: the turn reached the output-token limit (max_tokens=8192)` while
  running a 1M-context model, and the number was genuine: the `max_tokens`
  chain is `cfg.MaxTokens` → `modelMaxOutputTokens[model]` →
  `DefaultMaxOutputTokens`, and the floor is reached only when the id is absent
  from that table. The whole Claude 5.5 lineup — `claude-opus-5-5`,
  `claude-sonnet-5-5`, `claude-haiku-5-5`, `claude-fable-5-1` — was missing
  from `modelContextWindows`, `modelMaxOutputTokens` *and* `modelMaxEffort` at
  once, so Opus 5.5 ran 15.6× under its real limit, `ContextWindow` returned
  "unknown" (autocompacting at the 200k default on a 1M model, ~5× early, and
  dropping `ctx N%` from the status bar), and nothing anywhere said so. The
  truncation guard worked; it should not have had to fire.

  All three tables now carry the 5.5 ids, verified against `GET /v1/models`
  with the maintainer's own credential rather than a docs page, and the effort
  ceilings were probed live (all four accept `xhigh` and `max`). `DefaultModel`
  and the `opus`/`sonnet`/`haiku`/`fable` aliases move to the 5.5 generation
  with them.

  Two corrections fell out of the same work. The comment on
  `modelMaxOutputTokens` claimed an over-cap request "can't 400"; that is true
  of input+`max_tokens` against the *context window* and false of `max_tokens`
  against the *output* cap, which is checked up front — measured, Sonnet 4.5 at
  `max_tokens=128000` returns `max_tokens: 128000 > 64000, which is the maximum
  allowed number of output tokens for claude-sonnet-4-5-20250929`. And the
  previous "verified against /v1/models" sweep had overcorrected on exactly one
  model: `claude-sonnet-4-5-20250929` was listed at 1M context when the
  endpoint reports 200k, understating context pressure fivefold.
- **An unlisted model now gets its limits from the provider, not the floor.**
  `api.ResolveLimits` resolves the output cap and context window in one place,
  config → provider → built-in table → floor, and consults the provider *only*
  for a model the table cannot size, so an ordinary run pays no extra network
  at all. Config is applied last, so it can never be outvoted. An unknown
  output cap still floors to 8192 because some number must go on the wire and
  an unknown id may be a genuinely low-capped old snapshot; an unknown context
  window deliberately stays "unknown" rather than being guessed high, because
  guessing high skips compaction until the request 400s.

  This also fixes a quieter bug: the live context window the `/model` picker
  recorded never reached the loop's autocompact threshold, which kept reading
  the config value. `MaxTokens` and `ContextWindow` are now per-turn settings
  carried on the TUI session, so switching models moves both, and `/model <id>`
  prints what it set — it was computing a confirmation line and discarding it,
  making the one path that can land on the 8192 floor the only silent one.

  Sub-agents inherit the parent's resolved figures (`Spawner.WithLimits`)
  instead of re-deriving them. The child runs the parent's model, so
  re-deriving could only lose the provider's answer or a `maxTokens` override
  and fall back to the table — or to the 8192 floor for a model the table does
  not know, which is precisely where a sub-agent writing a large file truncates
  mid-call.
- **`/doctor` reports the output cap on its own line**, with where the figure
  came from (config, provider, model table, or the floor), and warns when it is
  at the 8192 floor. It was previously printed only when an effort level was
  set, so the run that actually failed — the floor, no effort configured —
  showed nothing.
- **An over-cap `max_tokens` no longer ends the session.** The cap comes from a
  table that goes stale, and an entry that is too high fails every resend
  identically. The 400 names the model's real limit, so `api.MaxTokensTooLarge`
  reads it and the loop retakes the turn at that number, once per run, telling
  the user which number was wrong. It is checked ahead of the context-overflow
  branch: compacting cannot help a request refused for its `max_tokens`, and
  the measured wording is one reword away from containing "maximum context".

## v0.1.0 — 2026-10-19

### Added
- **Releases are one click: pick the bump, CI does the rest.** The Release
  workflow takes `minor` (default), `patch` or `major`, and a dry-run tickbox
  that computes the version and builds the matrix without tagging or
  publishing anything. `scripts/release.sh` holds the logic so it can be run
  and read locally rather than living inside YAML.

  The bump is chosen, not derived. Deriving it means Conventional Commits, and
  this project uses `area: lowercase sentence` — documented in `CLAUDE.md` and
  followed by every commit in the history. release-please and semantic-release
  read nothing else, so adopting one would mean changing the commit convention
  across the project to automate a one-word decision. Same reason GoReleaser's
  generated changelog is off.

  Tagging and publishing happen in **one job**, which is not a shortcut.
  GitHub suppresses workflow runs for events raised with `GITHUB_TOKEN` to
  prevent recursion, with `workflow_dispatch` and `repository_dispatch` the
  only exceptions — so the obvious two-workflow shape (dispatch tags, tag
  push publishes) leaves the tag in the repository and never publishes, with
  no failed run to notice. The alternative fix is a PAT or App token, i.e. a
  standing credential to rotate and leak in exchange for a job boundary
  nobody needed. A tag pushed from a laptop is a user event and still
  triggers the workflow normally.

  Releasing an empty `## Unreleased` section is refused — the mistake is
  easiest to make right after a release — and so is a dirty tree, which would
  otherwise be swept into the changelog commit and mark every binary `-dirty`.
  Tests run before the tag is cut, so a failure leaves nothing to clean up.
- **Tagged releases: push `v*`, get built binaries.** The project had no
  release machinery at all — no tags on origin, no pipeline, and `ci.yml`
  triggering only on `push: branches: [main]` and pull requests. GoReleaser now
  builds darwin and linux on amd64 and arm64 from one runner, which is free
  because the project is pure Go and `CGO_ENABLED=0`. `docs/releasing.md` has
  the steps.

  Nothing bumps a version. `version.Version` stays the frozen
  `2.1.66-klaudia` mirroring the retired JS bundle for the parity harness, and
  a release identifies itself by the commit `version.Build()` reads out of the
  toolchain's VCS metadata. The build flags are load-bearing for that, and
  quietly so: measured on this repo, `-trimpath` and `-s -w` preserve the
  stamp, but `-buildvcs=false` strips it and `--version` degrades to the bare
  constant with no error anywhere. A shallow `actions/checkout` breaks it from
  the other end, hence `fetch-depth: 0`.

  The release job runs `go vet` and `go test` itself rather than trusting CI.
  A tag push matches neither of `ci.yml`'s triggers, so without that the
  release build would be the only build in the project that nothing gates.

  Windows is not published, though it cross-compiles cleanly on amd64 and
  arm64. The Keychain credential path, Chrome discovery and sandbox mode `os`
  are all darwin/linux-shaped, and shipping a binary nobody has run would
  invite bug reports against a platform with no maintainer. Two lines in
  `goos:` when that changes.

  GoReleaser's generated changelog is off: this file is written by hand and
  explains reasoning, and the repo's `area: lowercase sentence` subjects are
  not conventional commits, so the grouping would file everything under
  "Others" beside a better document.
- **`--version` reports the commit it was built from.** `klaudia --version`
  said `2.1.66-klaudia` regardless of the binary, because that constant mirrors
  the retired JavaScript bundle for the parity harness and has not moved in the
  lifetime of the Go port — while `go install` reports a pseudo-version like
  `v0.0.0-20261014093743-a32e343be969` that pins the commit exactly. Two
  numbers, one of them real.

  The real one is now shown: `2.1.66-klaudia (Klaudia) a32e343 (2026-10-14)`,
  from the VCS metadata the toolchain embeds, falling back to the commit inside
  the pseudo-version for binaries built from the module cache. A working tree
  with uncommitted changes is marked `-dirty`. `/doctor` reports the same under
  `build`. The constant itself is unchanged and still leads the line, so the
  parity harness sees what it expects.

  This is the question every "is the fix in?" needed answering, and the
  alternative has been comparing the binary's mtime against `git log`.

- **Effort: how hard the model works per response, on both providers.**
  `--effort low|medium|high|xhigh|max`, `KLAUDIA_EFFORT`, `effort = "..."` in
  the config, and `/effort` to change it mid-session. Effort covers the whole
  response — thinking, tool calls and text — and on the current Claude models
  it is *the* control for thinking depth, having replaced `budget_tokens`.
  Klaudia previously sent nothing and silently took whatever the provider's
  default was.

  Unset still sends nothing. That is what keeps the request byte-identical to
  before for anyone who does not set a level, and it is load-bearing for the
  OpenAI-compatible path: older vLLM and llama.cpp builds reject an unknown key
  outright, so an empty `reasoning_effort` would break them.

  One field, translated per provider — `output_config.effort` on Anthropic (GA,
  no beta header) and `reasoning_effort` on OpenAI-compatible endpoints. All
  five levels pass through unchanged in both directions because the OpenAI
  vocabulary is a superset of ours, so nothing is clamped and the same setting
  means the same thing on either backend. Configuring it per provider would
  have been less code and a worse answer.

  The API rejects an unsupported model/level pair with a 400 instead of
  dropping to the nearest level it supports, so a wrong value is a dead session
  rather than a cheaper one. `--effort` is validated against the model at
  startup, `/effort` refuses what the current model cannot do, and `/model`
  lowers the live level to the new model's ceiling and says so. An *unknown*
  model is treated as capable, not incapable: that is every OpenAI-compatible
  endpoint and every Claude snapshot newer than the table, and refusing locally
  would be Klaudia inventing a limit the provider never stated.

  Two costs are surfaced rather than hidden. Changing effort mid-session
  invalidates the conversation's prompt cache, so `/effort` says that the next
  turn re-reads the whole prefix at full rate — on a long session the most
  expensive thing a slash command can do. And `max_tokens` bounds thinking and
  text *together*, so `/doctor` warns when `xhigh` or `max` is set against a
  cap under 64k: against the 8192 floor an unknown model gets, that does not
  error, it truncates mid-thought and reads as the model losing the plot.

  The cache-preserving alternative (per-message effort, beta header
  `mid-conversation-output-config-2026-07-01`) is deliberately not used: it is
  Anthropic-only, and taking it would make a mid-session change cheap on one
  backend and expensive on the other. See `docs/effort.md`.

  Sub-agents inherit the session's level unless `[subagents] effort` says
  otherwise. Inherit is the default because a quieter Explore fan-out is a real
  saving but a silent downgrade degrades results months later with nothing to
  attribute it to; the knob exists so the trade can be measured first.
  Compaction summaries cap at `medium` — summarising is extraction from a
  transcript already in context, so `xhigh` buys nothing there and would be
  paid on every compaction.
- **An editor can drive Klaudia directly: the Agent Client Protocol v1, over
  stdio (`--input-format acp`).** Zed, Neovim's `acp.nvim` and the JetBrains
  plugin all speak it, so the editor keeps its own UI for messages, tool calls,
  diffs and permission prompts while Klaudia does the work. The arithmetic is
  the argument: the stream-json channel needs an adapter written once per
  editor, where ACP support is written once per editor *for every agent*, and
  the editors already shipped theirs.

  Served: `initialize`, `session/new`, `session/load`, `session/list`,
  `session/close`, `session/prompt`, `session/cancel`, `session/set_mode`, and
  `session/update` notifications for assistant text, thoughts, tool calls and
  their outcomes, plans, diffs, token usage and the available commands. The
  three permission modes are exposed as ACP session modes, so the editor's mode
  picker is Klaudia's `/mode` — per session, because a client with three threads
  open has three conversations and one process-wide mode cannot be right for all
  of them.

  `fs/read_text_file` is used when the client offers it, and that is a
  correctness fix rather than politeness: the editor hands back the user's
  *unsaved buffer*, and reading disk while someone looks at unsaved edits is how
  a model ends up reasoning about text that is no longer there.

  Declined, all deliberately:

  - **`fs/write_text_file`.** Reading through the client is safe; writing is
    not. Klaudia's `Edit` matches against what it read and `Write` is what
    `/undo` checkpoints — both of which assume the file on disk is the one they
    touched.
  - **`terminal/*`.** Routing `Bash` through the editor's terminal hands over
    execution, and with it the sandbox, the trust gate, job control and output
    clamping. A nicer widget is not worth any of those.
  - **`session/delete`.** Not advertised. A transcript is the user's record of
    what an agent did on their machine; an editor's "close tab" should not be
    able to erase it.
  - **Image and audio prompt blocks.** The loop takes a string prompt, so there
    is nowhere for the bytes to go, and claiming the capability would have the
    editor send a screenshot that Klaudia silently discarded. `embeddedContext`
    *is* claimed — a resource block carrying text is inlined, which is the whole
    point of pasting the open buffer in.
  - **MCP servers from the client.** Klaudia connects the ones in `.mcp.json`
    itself.

  Decisions worth recording:

  - **v1, not v2.** v2 is published as a Draft and changes real things —
    `tool_call` folded into an upsert-only update, a turn-lifecycle
    `state_update`, agent-owned terminals — but v1 is what every shipping client
    negotiates today. The version is agreed at `initialize`, so v2 is additive
    later: answer 2 to a client asking for 2.
  - **ACP session ids are Klaudia transcript ids.** Not a second numbering with
    a mapping table beside it, which is how `session/load` ends up loading
    something adjacent to what the user picked. `--resume`/`--continue` reaches
    ACP too: the editor's first thread *is* the session the CLI resolved, and
    keeps appending to its transcript.
  - **One transcript per session, not per process.** ACP is the first frontend
    with more than one conversation at a time, and a shared recorder had two
    threads appending to one file — so `session/load` replayed an interleaving
    that never happened.
  - **One prompt at a time, across all sessions.** A client may open several
    threads, but this process has one tool registry, one job store, one executor
    and one sandbox, so two concurrent turns would interleave. A prompt arriving
    while another runs waits, and a cancel while it waits is still honoured.
  - **`TodoWrite` is a plan update, not a tool call.** Clients render plans as a
    checklist, which is the entire point of the tool; left as a tool call the
    user sees a raw JSON array. `Edit` and `Write` carry an ACP diff, and `Edit`
    sends its own `old_string`/`new_string` rather than a reconstructed file —
    reconstructing means reimplementing the replacement rules here, and a
    preview that computes the change differently from the tool is worse than a
    narrower one, because this is the text the permission prompt asks you to
    approve.
  - **Token usage is sent per turn, not per inner call.** Klaudia's `usage`
    event is a delta; ACP's `usage_update` is "resident in the context, out of
    this many". Forwarding deltas showed a session at 300% of its window.
  - **A host-gate refusal maps to `failed`.** v1 has no fourth status, and
    "completed" would claim the tool ran. The refusal's own text goes out as the
    call's content so the user can read what actually happened.
- **Sub-agents that can write get a git checkout of their own.** Two children
  editing one working tree is not a race in the usual sense: nothing is
  corrupted and nothing errors — one writes a file, another reads a half-written
  version and reasons about it, a third rewrites the first one's edit, and every
  step *succeeds*. The alternative was running children one at a time, which is
  the thing concurrency was for. So a sub-agent holding `Write`, `Edit`,
  `NotebookEdit` or `Bash` now runs in a `git worktree` under
  `~/.klaudia/worktrees/<project>/<agent-type>-<timestamp>/`, and `Explore` and
  `Plan` keep sharing the tree — they hold Read, Glob and Grep, so there is
  nothing to isolate them from, and their whole output is paths that a checkout
  would make wrong.

  Not under the project root, because the parent's own Glob and Grep walk it:
  the model would find two of every file and read a copy of the code it is
  editing. And under `~/.klaudia` rather than `TMPDIR` because the trust
  classifier reads a bare `$HOME` path as the project zone, where a temp path
  earns a host prompt per sub-agent.

  **The checkout holds your uncommitted work, not `HEAD`.** `git worktree add
  <dir> HEAD` would hand the child the last commit — the one state nobody asked
  about — and a child told to "test the function I just wrote" would report,
  convincingly, that there is no such function. It is seeded with the tracked
  delta and the untracked non-ignored files, committed on the detached HEAD as
  the baseline the child's work is measured against, with hooks and signing
  skipped: a repository's pre-commit hook is aimed at the user's commits, and
  running their linter every time a sub-agent spawns is a side effect nobody
  asked for. Ignored files are *not* copied — build output is what makes a copy
  expensive, and a tree that builds is a different promise from a tree that
  matches. The cost is real: a child that needs an install step before it can
  test will pay for it, or fail, which is what `[subagents] worktree = false` is
  for.

  Coming back, the child's work is diffed against the baseline and applied with
  `git apply`, which touches files and not the index — same reason `/undo`
  writes loose objects instead of stashing, and your staging area is exactly as
  you left it. `git apply` is all-or-nothing, which is the behaviour worth
  having (a patch that no longer fits is one whose file moved underneath us), so
  it is retried file by file and what did not fit is named to both you and the
  model. The model is told because it asked a child to change files, and a
  silent conflict has it carry on describing work that is not there. A checkout
  survives any conflict — it holds the only copy of that version — and a failed
  or interrupted child keeps its checkout with the path in the error, because
  half a change applied to your tree is the outcome isolation exists to prevent.
  Stale checkouts are pruned after seven days.

  Adoption is serialised per repository, and that lock was measured rather than
  assumed: without it, two concurrent adoptions of one file reported success
  twice and silently kept one version — the precise failure the feature exists
  to remove.
- **Every frontend runs a turn through one contract (`agent.Turn`).** Each one
  used to declare its own `RunFunc` — the TUI's took nine positional parameters,
  stream-json's five — and the CLI wrote a closure per mode that filled in
  `Options` by hand. Two consequences, both live bugs rather than hypotheticals:
  a frontend that *omitted* a capability was indistinguishable from one that did
  not want it (which is how `AskUserQuestion` and `ExitPlanMode` came to be dead
  over stream-json), and adding a parameter meant touching every frontend, so
  nobody did and the gap between the TUI and the embedding channels only
  widened. A capability is now a field a frontend sets, the copying happens once
  in `Turn.Apply`, and a new field left zero behaves exactly as it did before it
  existed. `HostChange.Fields` is shared for the same reason: stream-json's
  `host_change` payload and ACP's `_meta` have to agree about which fields are
  omitted, and the omissions are the informative part.
- **MCP elicitation, so a server can ask the user instead of failing.** The
  client now speaks protocol **2026-07-28** (go-sdk v1.6.1 → v1.8.0) and
  advertises the elicitation capability, which is how a server obtains the one
  thing it cannot derive — a token, a branch name, a confirmation before
  something destructive. Without it the server's only options were a degraded
  path or an error that said nothing about what it actually wanted.

  The question goes to the same prompt `AskUserQuestion` uses and is labelled
  with the server that asked; "an MCP server wants your GitHub token" is not
  answerable without knowing which one. Each schema field is one question —
  booleans and enums become choices, anything else is typed — and `required`
  fields are asked first in the order the schema lists them. That part of the
  order is real, because `required` is a JSON array; the optional fields are
  sorted, because object key order does not survive decoding and the schema
  reaches us already decoded.

  Answers are free text, so a declared type is a coercion that can fail. A
  failure **cancels** rather than guessing: `cancel` is recoverable — the server
  may ask again — where an invented number is not. An answer outside a declared
  enum cancels for the same reason. `3.0` is accepted for an integer; `3.5` is
  not.

  Two things are deliberately not supported:

  - **URL-mode elicitation is declined, and not advertised.** The spec's other
    mode hands the client a link to open out of band and expects an immediate
    "accept". A terminal cannot open a browser without reaching outside the
    project, and answering yes for a link that was only printed into a
    scrollback nobody is watching is worse for the server than being told no —
    it waits on a flow that never started. Declaring form-only lets it choose
    its own fallback. Nothing on the multi round-trip path enforces the declared
    mode, so the check is made again on arrival.
  - **Headless runs advertise no elicitation at all.** There is nobody to ask, so
    a capability would only earn a decline on every request. A server that sees
    nothing takes its non-interactive path instead.

  Two findings from the SDK worth recording, because neither is guessable from
  the spec text. On 2026-07-28 a server **cannot** send `elicitation/create`
  while serving a request: it returns an `InputRequests` map in the tool result
  and the client fulfils it and retries (multi round-trip requests, SEP-2322).
  The SDK's client middleware already runs that loop, so `mcpTool.Execute` needed
  no changes — but it fulfils the requests of one round **concurrently**, and the
  frontend has a single question slot, so two prompts in flight would overwrite
  each other's options and the user would answer the wrong question. Elicitations
  are serialised.

  Capabilities are now set explicitly, which also drops the SDK's default
  `roots: {listChanged: true}`. Klaudia registers no root, so that advertised a
  feature whose only possible answer was an empty list; roots is deprecated as of
  this protocol version anyway.
- **`/doctor` warns about the deprecated HTTP+SSE transport.** `type:"sse"` still
  works and is still supported, but the spec has deprecated it and servers drop
  it on their own schedule. The symptom when one does is a connect error with no
  hint that the fix is one word in `.mcp.json`, so the check names the servers
  still on it.
- **Lifecycle hooks: four events, and a confirmation for the ones that arrive
  with a clone.** The commonest request a harness gets is "run *my* thing at
  *that* moment" — format after a write, refuse edits to generated files, paste
  the ticket into every prompt — and every one of them is a feature Klaudia
  would otherwise have to grow an opinion about. A hook is a line of shell and
  the agent stays out of it.

  Four events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`.
  Claude Code has grown past thirty, and the long tail of them exists to serve
  one workflow each; every event is a promise about when the loop calls out,
  which is a promise about the loop's shape. `Stop`/`SubagentStop` — "the model
  thinks it is done, make it keep going" — were left out deliberately: a hook
  that can deny completion can hang a session, and nothing in a shell command
  knows better than the loop whether the work is finished.

  **A hook is not a security boundary, and the ordering is what keeps it from
  pretending to be one.** `PreToolUse` runs *after* the host gate and the
  permission check have both allowed a call, so a hook can narrow what Klaudia
  will do and can never widen it. The alternative — a hook consulted as part of
  the decision — is a config file that can grant permission, which is a way to
  switch the gate off by writing a file.

  Which matters because a project's `.klaudia/config.toml` arrives with a clone.
  Cloning a repo and starting Klaudia in it would otherwise be enough to run
  whatever that file says, before the user has read a line of it: the same shape
  that made editors stop sourcing project-local config, and the reason `.vscode`
  tasks and `direnv` both grew an approval step. So hooks from `~/.klaudia` are
  the user's own and run unprompted — asking someone to confirm their own
  settings file is how people learn to stop reading prompts — and hooks from the
  repository are confirmed once, through the existing host-change card, with
  every command shown. The approval is recorded in `~/.klaudia/hooks.json` keyed
  by project directory.

  The unit of approval is the whole set, fingerprinted over each hook's event,
  matcher, command and timeout, in order. A set rather than a file, because a
  prompt has to show what is being agreed to and "these four commands" is
  something a person can read where "hooks: yes" is not. And a fingerprint
  rather than a flag, because the dangerous repo is not the one that asks on day
  one — it is the one that asks for something harmless and changes it in a later
  pull. Reverting to a formerly-approved set re-asks too: that is precisely the
  move available to an attacker who got a yes once. `config.merge` deliberately
  does *not* merge the two files' hook lists, because a merged slice is one in
  which a repository's command is indistinguishable from the user's.

  Details that were decided rather than inherited:

  - **Exit 2 is a verdict; any other non-zero status is a malfunction.** A hook
    that exits 127 because the formatter is not installed has made no judgement
    about the tool call, and reporting that to the model as a refusal would have
    it reason about, and route around, a broken environment. Malfunctions and
    timeouts go to the user as notices and never to the model — a broken script
    is the operator's to fix. The convention itself is Claude Code's, and so are
    the stdin field names (`tool_name`, `tool_input`, `tool_response`), because
    a hook script is the most portable artefact in the ecosystem and a
    gratuitously different payload means editing every existing one for nothing.
  - **Hook stdout is prompt, so it is bounded.** 8 KB, with the truncation
    reported. `command = "git log"` in a long-lived repo is megabytes pasted in
    front of every user message.
  - **30 seconds by default, not Claude Code's minute.** `PreToolUse` is in the
    critical path of every single tool call, so a hook that hangs is a session
    that looks frozen; the formatter and linter cases finish in under a second.
  - **Injected context is its own content block, placed before the prompt.**
    Separate, so a frontend replaying the transcript can tell what the user
    typed from what a hook added. Before, because context that follows an
    instruction reads as part of it — a hook that pastes a file listing after
    "delete the stale ones" has changed what the sentence means.
  - **`PostToolUse` output is appended to the result and labelled.** The model
    has to be able to tell the tool's own words from a hook's commentary: a
    formatter saying "reformatted 1 file" is not something `Write` printed. A
    refusal here cannot undo the call, but it does make the result arrive as an
    error with the reason attached, which is the difference between the model
    believing its write succeeded and knowing the linter rejected it.
  - **`SessionStart` fires once per session and cannot block.** Once per
    session, not per turn, and the claim lives on the runner because that is the
    only object with the session's lifetime — a `Loop` is per-process, but a
    sub-agent builds its own, which would have fired it again on the first
    `Agent` call. It cannot block because there is nothing left to block, and
    treating its exit 2 as a veto would make a config typo look like a broken
    install. Sub-agents run the two tool events and skip `UserPromptSubmit`:
    their prompt is the parent's instruction, not the user's.
  - **A bad entry is dropped and reported, not fatal and not silent.** A
    mistyped event name is the likeliest error by a distance, and
    `"SessionStarted"` quietly never firing is the worst outcome available. All
    the problems in a file are reported, not the first.
  - **Hooks are not sandboxed, whatever `[sandbox]` says.** That setting
    confines the model's commands. A hook is the user's own automation, and the
    things it exists to do — run a formatter, touch a git index, post a
    notification — are exactly what confinement forbids; one failing under
    `sandbox-exec` would fail invisibly from the config file that declared it.
  - **In a headless run, project hooks do not run.** The notice distinguishes
    that from a refusal, because nobody objecting is not the same as agreement.

  `/doctor` now lists the configured hooks in the order they will run and warns
  when a project set is still waiting on approval — the one state whose only
  symptom is silence. See `docs/hooks.md`.

- **The model is told which permission mode it is working under.** It was never
  told, and it is the one doing the work under the constraint. In plan mode it
  reasoned its way to an edit, called `Edit`, and was refused — a wasted turn,
  and worse, a plan built without knowing that running the build to check it had
  never been an option. Leaving plan mode was the mirror image: the user got a
  banner, the transcript recorded it, and the model carried on hedging about
  work it was now free to do.

  The active mode is now a system-prompt clause, rebuilt per turn, and a change
  is emitted as a `permission_mode` event for headless and stream-json
  frontends (the TUI already prints its own banner and shows the mode in the
  status bar). Per turn rather than once per run because approving a plan flips
  the mode mid-run. Doing it in the loop — the only place that holds the live
  mode function — covers the picker, `/mode`, `/plan`, plan approval and any
  future control request without any of them knowing about it.

  Two details. Autonomous adds no clause: it is what the base prompt already
  assumes, and restating the default every turn is tokens spent to change
  nothing. And the clause is a separate system block appended after the base
  prompt rather than spliced into it, so the long identical prefix stays
  byte-for-byte stable and a mode change invalidates nothing before the cache
  breakpoint.

- **AGENTS.md is read.** It is the cross-agent instruction standard — now a
  Linux Foundation project, present in several hundred thousand repositories and
  read by around twenty agents — and Klaudia read none of them. A user who had
  already written down their conventions for exactly this purpose got a model
  that had never seen them.

  Both files are now read, at three levels, generic first at each one:
  `~/.claude/CLAUDE.md`, `~/.klaudia/AGENTS.md`, then `AGENTS.md` and
  `CLAUDE.md` at the git root and again in the working directory. Both rather
  than one, because a repo may carry either or both with different content — the
  generic file for every agent and a Claude-specific refinement beside it — and
  reading one of them silently drops the other.

  Supporting both with `ln -s AGENTS.md CLAUDE.md` is the commonest arrangement
  in the wild, and a copy is the second commonest. Either would have sent the
  whole instruction block twice in every request, so identity is established
  both by resolved path (`EvalSymlinks` — `filepath.Abs` gives a symlink its own
  path) and by content hash, which is the only thing that catches the copy. The
  prompt's section header now names the files the instructions actually came
  from, so a model asked to write a convention down knows which file to put it
  in.

- **Read-only tool calls in the same batch run at the same time.** A turn's
  tool calls were dispatched strictly one after another, so the commonest shape
  in a turn — three or four `Read`s, or a `Glob` and two `Grep`s, issued
  together to orient — cost the *sum* of its round trips instead of the slowest
  one. Adjacent calls to tools that have opted in now execute concurrently, up
  to five at once.

  Opting in means implementing `tools.ConcurrencySafe`, and the marker asserts
  more than "does not write": that the tool never prompts the user, holds no
  process-wide resource assuming one caller, and does not depend on when it ran
  relative to its neighbours. `Read`, `Glob` and `Grep` are marked; everything
  else keeps the old behaviour, because adding a tool should not require
  thinking about parallelism and the cost of forgetting should be a slow batch
  rather than a race.

  Three properties are preserved rather than traded away:

  - **Order.** Only *adjacent* safe calls are grouped. A `Read`, an `Edit` and
    another `Read` stay in that order — the second `Read`'s result depends on
    the `Edit` having happened — so groups are maximal runs and nothing is ever
    lifted past a call it followed. Results are written by index, so the model
    sees them paired with the calls it made however the goroutines finished.
  - **One prompt at a time.** A call whose host-gate or permission check would
    ask is never grouped, whatever the tool reports: there is one `Approver` and
    one place to put a question. That is decided before anything starts, so
    `dispatch` does not need to know it is in a group.
  - **A readable transcript.** A group's events are delivered in call order.
    The call at the head streams live so the user sees movement immediately;
    later calls buffer until the ones before them finish. A group of one
    behaves exactly like the old serial path.

  The loop-breaker counters moved behind a mutex (`failureState`). The lock
  makes each operation atomic, not the breaker *decision* atomic with the
  update that follows it — serialising dispatch around a heuristic would undo
  the parallelism it protects. The consequence is bounded: a group can run one
  extra copy of a call that was about to be refused, and since only
  concurrency-safe tools are grouped, the extra copy is a read that reads
  again.

- **A turn's tool results are capped in aggregate, not just one by one.** The
  per-result cap cannot see this case: five results that each stop just under
  the 30 KB budget are 150 KB in a single user message, and a turn is not
  limited to five calls. Twenty well-behaved `Grep`s is most of a 200K window
  in one message, with nothing in the per-result path finding anything wrong —
  and parallel dispatch above makes that shape common rather than theoretical.

  One turn's combined tool-result text is now held to 200 KB. Shares are
  allotted by water-filling rather than an equal split: results already under
  their share keep every byte and what they do not use is redistributed to the
  large ones, because trimming a 200-byte result to an equal share of a budget
  it was never going to exhaust loses real content to no purpose. Image parts
  survive untouched — the model cannot recover vision content from a log file —
  and a result the per-result cap already spilled keeps pointing at that file
  rather than gaining a second notice naming a spill of the already-clamped
  copy. The cap runs after each call's events are emitted, so a local frontend
  still shows what the tool actually produced.

- **An elided tool result now leaves a handle, not a blank.** Microcompact
  replaced every old result with the same fixed string, so after a pass the
  conversation held several identical `[Old tool result elided to save
  context]` markers: the model could not tell which call each had been, and the
  content was gone for good. The only recoveries were re-running the tool —
  paying the latency and the tokens again, and hoping it was idempotent — or
  reasoning without it.

  The placeholder now carries the original size, its first non-empty line
  truncated to 120 bytes, and the path of a spill file holding the full text, so
  it reads `[Old tool result elided to save context; 70 bytes; began:
  ./internal/agent/loop.go:349: for _, tu := range toolUses {; full text:
  /…/elided-123.log]`. That is enough to recognise the call and recover it with
  a `Read`, for a few dozen tokens against the thousands a pass reclaims. The
  spill is injected as a `Spiller` function rather than imported, keeping the
  package dependency-free, and it is the same on-disk spill the per-result cap
  writes; with no spiller the placeholder simply promises no file.

  `Microcompact` runs in two passes for a reason: `compact()` is called at the
  top of every turn, and pricing the placeholders *before* writing anything lets
  the `MinTokensToSave` floor reject the pass without side effects. A single
  pass would have written a fresh set of spill files on every turn a
  conversation spent sitting just under that floor. The second pass spills and
  recomputes the exact saving with the real paths in place.

- **Every tool's output is capped, not just Bash's.** The clamp-and-spill
  machinery in `internal/tools/output.go` had exactly one caller — Bash — so a
  single Grep across a large repo, a verbose MCP server, or any tool added by
  someone who had not thought about the context window could put its entire
  output into the conversation. Measured on a synthetic Grep-shaped result: 980
  KB arrived at the model intact, roughly a quarter of a million tokens from one
  call.

  The agent loop now applies a backstop after collapsing a tool's results: the
  text is clamped head-and-tail to the same 30 KB budget Bash uses, the
  untruncated copy is written to `~/.klaudia/outputs/<tool>-*.log`, and a notice
  names the file so the model can read what was removed. The UI is unaffected —
  it already receives the full text out of band via `Result.Full`.

  This is a backstop, not a replacement. A tool that understands its own output
  clamps it better than a byte-level cut can: Bash keeps a tail because that is
  where the verdict is, and appends the exit annotation afterwards so it
  survives. Output already within budget passes through untouched, so
  self-clamping tools are never cut twice. Spill files are now named after the
  tool that produced them (previously all `bash-*.log`), which also meant
  sanitising MCP names — `CreateTemp` rejects a pattern containing a path
  separator outright.

- **MCP servers can be configured globally, in `~/.klaudia/.mcp.json`.** Only
  `./.mcp.json` and `./.klaudia/.mcp.json` were read, both relative to the
  project, so a server you want in *every* project had to be copied into every
  checkout. Worse, the message when none were configured — "Add them in
  `.mcp.json` or `.klaudia/.mcp.json`" — reads like one of those is a home-dir
  path, and installing to `~/.klaudia/.mcp.json` on that basis produces a file
  that is never read, no error, and no servers. Config and sessions live under
  `~/.klaudia`, and skills already load from `~/.klaudia/skills`, so the absence
  of a global MCP scope was the odd one out.

  Precedence is global → project `.mcp.json` → project `.klaudia/.mcp.json`,
  per server name, matching how `config.toml` overlays a global file with a
  project one. Parse errors now name the full path: three files share the base
  name `.mcp.json`, and "which one is broken" is the entire question when the
  answer is a file in another directory.

- **MCP config changes apply to the running session.** Any `.mcp.json` that
  applies to the project is watched, and an edit adds, drops or restarts servers
  in place — a server whose config didn't change keeps its session rather than
  being restarted along with the rest. Both the main registry and the one
  sub-agents draw from are rebuilt, along with the deferred-tool set, so a
  sub-agent spawned after a reload sees the same tools as its parent.

  The watch covers only the config files themselves, not `~/.klaudia` as a
  whole: that directory also holds `sessions/`, `jobs/` and `browser/`, which a
  live session writes to continuously, and watching it recursively would rebuild
  the MCP servers on every message typed. A config that no longer parses is left
  unapplied rather than applied empty — a half-typed file should not take
  working servers away — and both that and any server which fails to launch are
  now reported in the transcript.

- **A failed MCP reload says so.** Reload outcomes were silent, on the grounds
  that the watcher could not write to a live TUI without corrupting the render.
  That was not true: background jobs already report their exits across
  goroutines through the model's event channel, and reload notices go the same
  way. Silence was the expensive part — a typo in `.mcp.json` looked exactly
  like a clean reload, and the first sign of trouble was an unrelated-looking
  tool failure much later.

  A config that fails to parse reports the parse error *and* that the previously
  loaded servers are still running, because the natural reading of a config
  error is that MCP is now down, and the useful fact is the opposite. Servers
  that fail to launch are named, capped at three with a count of the rest, and
  point at `/mcp`. A reload that works stays silent: announcing every one would
  print a line each time an unrelated key in the file was saved.

### Changed
- **`baseURL` now applies to whichever provider is selected, not just
  `provider = "openai"`.** It was read on the OpenAI-compatible path and
  nowhere else, so an Anthropic-dialect proxy or gateway could only be reached
  through the undocumented `KLAUDIA_CUSTOM_ENDPOINT`, and a `baseURL` sitting
  in the file under the default provider was read, ignored, and silently
  overridden by production. `KLAUDIA_CUSTOM_ENDPOINT` still wins over the
  config — env over config, as everywhere else — and now applies to both
  providers rather than only to Anthropic.

  This is a behaviour change for one configuration: `baseURL` set with
  `provider` unset or `"anthropic"`. That combination previously went to the
  Anthropic production API and now goes to `baseURL`. If the URL was left over
  from an OpenAI-compatible setup, set `provider = "openai"` to restore the
  pairing, or delete the key.

  `baseURL` and `provider` stay two keys — endpoint and dialect — rather than
  collapsing into one. Separate `baseURLAnthropic`/`baseURLOpenAI` keys were
  built first and removed before release: `provider` already states the
  dialect, so a key per backend duplicates it and grows the config by one key
  for every protocol added later. Inferring the dialect from the URL instead
  was rejected in the other direction — nothing in a hostname distinguishes an
  Anthropic-compatible gateway from an OpenAI-compatible one, and a wrong
  inference surfaces as a protocol-mismatch 400 from the server rather than as
  anything that names the cause.

### Removed
- **The per-command permission model is gone, not deprecated.** No allow/deny
  rules, no `--allowedTools`/`--disallowedTools`, no `/allow` or `/deny`, no
  `default`/`acceptEdits`/`dontAsk` modes, no "always" answer on a prompt, and
  no `[permissions] allow`/`deny` or `[trust]` config. `HostPolicy` goes with
  them: the gate has no observe or off posture, because `bypassPermissions`
  already means "check nothing" and says so.

  These were kept as a migration affordance when zones landed. Running both
  models at once turned out to be worse than either, in a way only visible in
  use: a rule in `.klaudia/config.toml` put the next session into observe,
  observe dropped its permission mode to `default`, and `default` asked before
  every edit and every command. So answering "always" — the thing offered to
  stop a prompt — was what guaranteed more prompts next session, in a project
  that would otherwise have started autonomous. Persistence was also gated on
  `.klaudia/` already existing, so a project acquired the behaviour the first
  time anything created that directory. Measured on a project where `/goal`
  wrote `.klaudia/GOAL.md`: the next "always" reached disk, and simulating the
  following startup gave `trust=observe permissionMode=default`.

  Two further dead ends went with it. `/trust upgrade` returned "Already
  enforcing" without looking at the permission mode, so a session that reached
  gate-enforcing with the mode left behind — `/plan off` left `default`, an
  approved `ExitPlanMode` left `acceptEdits`, neither touching the gate — had
  no signposted way out: the command the UI and docs pointed at reported
  success and changed nothing. Both now land on `autonomous`, and the gate has
  no posture to be out of step with.

  Approving an operation is the replacement, and it was already the documented
  one — `docs/trust.md` and `/trust` had both been claiming Klaudia no longer
  created rules while `tui.go` still did. Existing configs are not migrated:
  delete the `[permissions]` allow/deny entries and any `[trust]` section.
  `[permissions] mode` is still read and now takes
  `autonomous`/`plan`/`bypassPermissions`.

### Changed
- **Backend-synced skills are no longer loaded.** `~/.claude/skills/synced/` is
  written by Claude's sync service: not in anyone's version control, not
  reviewed, and able to change between runs. A skill is instructions injected
  into the model's context, so this is a trust boundary rather than a parsing
  question — reading `~/.claude/skills` is a compatibility courtesy, and it does
  not extend to whatever a backend put there overnight. Copy a skill into
  `.klaudia/skills` to use it, where it lives under git and changes when you
  change it. `/doctor` reports how many were ignored so they stay discoverable.

### Fixed
- **The dropped-search notice reappeared above every reply.** Three notices,
  every turn, for the rest of the session. The repair was only ever applied to
  the copy of the conversation sent to the API — `Messages:
  sanitizeMessages(messages)` — so the orphaned `server_tool_use` blocks stayed
  in the conversation itself and were rediscovered on the next turn, and the
  next. The "already reported" set is per `Run`, and a TUI turn is a `Run`, so
  it never suppressed anything.

  The sanitised conversation is now kept, so a search is announced once and the
  history is clean from then on. Sanitising is idempotent, so this costs
  nothing when there is nothing to repair.

- **A repaired turn could 400 forever: "`thinking` ... blocks in the latest
  assistant message cannot be modified".** Reported from a session that could
  not be continued at all — five interrupted web fetches, then this 400 on
  every send, including after typing `continue`.

  The two repairs collided. An interrupted server-tool call leaves an unpaired
  `server_tool_use`, which the API rejects, so Klaudia edits that message to
  drop it. But the API also forbids *any* modification of the latest assistant
  message when it carries thinking blocks — and with extended thinking on,
  nearly every assistant message does (9021 of them in the transcript
  concerned). Fixing the first 400 therefore caused the second, with no way
  forward: the edit cannot be undone, because the original blocks are gone.

  Both ends are now handled. Sanitising drops the whole trailing turn rather
  than editing it when that message carries thinking — an incomplete turn the
  model simply takes again, with the existing notice explaining the lost
  search. And if the API rejects a request for this reason anyway, the loop
  abandons that turn and retries once instead of resending it forever. Client
  `tool_result` blocks answering a dropped turn go with it, so one dead end is
  not traded for another.

- **The dropped-search notice blamed the transcript for every drop.** A web
  search still in flight when a turn is interrupted is orphaned in the ordinary
  course of events — the `pause_turn` assistant message is recorded with the
  search unanswered — but the notice said its result "could not be restored
  from the transcript", which is the wording for genuine corruption. Reading
  that for three interrupted searches in one session sent someone hunting a bug
  that had already been fixed. The notice now distinguishes the two: orphans
  present when the Run started blame the saved transcript, and ones that appear
  mid-session say they were interrupted.

- **Claude's own skills directory produced warnings and hid 19 skills.**
  Startup printed `skill ~/.claude/skills/.trash: directory has no SKILL.md`
  and the same for `synced`. Both were wrong in opposite directions.

  `.trash` holds skills Claude has *deleted*, still complete on disk —
  resurrecting them would be worse than the warning. Hidden directories are now
  skipped outright. `synced/<workspace-id>/<name>/SKILL.md` is where Claude's
  synced skills actually live, so the container was not a broken skill but 19
  working ones we never looked inside; the loader now walks containers to a
  bounded depth of three, which covers that layout without turning startup into
  a filesystem crawl.

  A directory is only reported as broken if it directly contains Markdown but
  no `SKILL.md` — a directory holding other skill directories is a container,
  not a mistake. Loose `.md` files count as skills only at the top level, so a
  skill's own `notes.md` cannot become a second, bogus skill.

- **Five things the stream-json embedding channel could not do.** It is the
  channel whose entire purpose is a client with a user sitting in front of it,
  and most of what follows was a capability the transport never carried.

  - **`AskUserQuestion` and `ExitPlanMode` were dead.** The CLI passed no
    `Asker` and no `Planner` on this path, so the model called them and was told
    there was nobody to ask. Two new `control_request` subtypes, `ask_user` and
    `exit_plan`, answered on the same `request_id` channel as `can_use_tool`. A
    peer that has not implemented them should answer with an error, which reads
    as "cancelled" and leaves the model where it was — rather than the first
    option being picked and an answer the user never gave being attributed to
    them.
  - **A permission ask arrived stripped of everything but the tool name and its
    input.** `tool_use_id`, the specifier, the suggestion and the whole host
    change were dropped on the floor, so every ask looked the same. The host
    change is the damaging one: "allow Bash?" is the wrong question to put to
    someone about `systemctl restart nginx`, and a peer that cannot tell the two
    apart cannot render the right card.
  - **A refusal was reported as a successful result with no text.** `subtype:
    "success"` and an empty `result` let a pipeline treat a refusal, or a hit
    limit, as a finished task — the same bug the headless path already fixes and
    this one had not inherited. It now reports the stop reason, `is_error`, and
    the note explaining what stopped the turn.
  - **`--resume`/`--continue` was resolved and then discarded.** The driver
    started a fresh conversation while the CLI reported it had resumed one.
  - **A control response without a `subtype` was read as a failure.** Only
    `"success"` counted, so a peer that echoed a `request_id` and a payload —
    the obvious minimal implementation — had every answer treated as a denial.
- **Notices reach the operator in a headless run.** `-p` suppresses
  intermediate events on purpose — stdout is the result and nothing else,
  because it gets piped — but notices were going through the same renderer and
  being dropped with them. A notice is not a step in the work; it is the one
  class of message only the operator can act on: a hook that failed to start,
  project hooks waiting on an approval nobody is there to give, an MCP server
  that could not be reloaded. Reported but invisible is the same as unreported.

  They now go to stderr, in every headless format, so stdout's contract is
  untouched and a notice cannot land in the middle of a stream-json line.

- **The list of slash commands a skill cannot shadow is derived from the
  commands, not kept by hand.** `internal/cli` held its own copy, and it had
  drifted in both directions. It still reserved `allow` and `deny` after the
  per-command rule model was removed, so a skill could not be named either. And
  it had never learned about `/trust`, `/undo`, `/jobs`, `/pin` and fifteen
  others, so a skill named after one of those was shadowed by the built-in —
  reachable only through the `Skill` tool — with no warning at all, which is the
  exact failure the list exists to report.

  It now comes from `commandList`, the table that already drives `/help` and
  type-ahead, with the two switch-only aliases (`/?`, `/exit`) named explicitly.
  Both lists live in `internal/tui` beside the switch they describe, and a test
  parses `handleSlash` and fails if a case there is not claimed by one of them —
  measured by adding a `/bogus` case and watching it fail, rather than assumed.

- **A dead MCP server restarts itself on the next call.** A reload already
  probes liveness, but only the config changing triggers a reload — so a server
  that died mid-session with nobody editing `.mcp.json` stayed dead for the rest
  of it. A stdio server whose child process exits keeps a non-nil
  `ClientSession`, nothing nils it out, so the first sign was a tool call
  failing and the only cure was `/mcp` or a restart.

  A call that fails on the wire now probes the server and relaunches it if it
  has stopped answering. The probe decides, not the error: a bad argument or an
  unknown tool also arrives as a call error, and restarting a healthy server
  over the model's mistake would throw away whatever state it holds — for an
  editor bridge, the user's open session.

  Whether to repeat the call is a correctness question, not a performance one.
  The failure may have come on the way back from a call that already ran, and
  nothing on the wire distinguishes that from one that never arrived. So a tool
  declared read-only (`readOnlyHint`, or the per-server override) is retried,
  and anything else is reported with the ambiguity stated — the server is back,
  the call may or may not have taken effect, check before retrying. Reading an
  MCP *resource* is always retried, having no side effects to be ambiguous
  about.

  Relaunches are serialised per server and rate-limited to one per 15 seconds.
  Two tool calls, or a tool call and `/mcp`, can arrive together and would
  otherwise each spawn a child process and close the other's; and a server that
  simply cannot start would pay a full launch timeout on every call the model
  makes, turning one bad entry into a stalled session.

- **`ToolSearch` no longer loads tools on the strength of "the".** Ranking
  matches instead of demanding every term fixed descriptive queries and brought
  a cost with it: because OR-matching qualifies a tool on any single term, and a
  match *reveals* that tool, a word that appears all over the catalog spends the
  budget deferred loading exists to protect. Asked to "stop the background job
  that is running", Klaudia loaded a Godot project runner — its description
  contains "the" — and a scene manager, because "is" is a substring of "disk".

  Two rules, each aimed at one of those mechanisms. A term matching more than
  half the catalog is dropped before matches are qualified, decided against the
  loaded catalog rather than a list of English stopwords, because which words
  are uninformative depends on what is loaded — with 65 Godot tools present,
  "godot" narrows nothing either. And a term shorter than three characters
  matches whole words only, with no fuzzy tier, since document frequency cannot
  catch "is" inside "disk" when "disk" is itself a rare word.

  If every term is noise the whole query is kept: the bare "godot" case, where
  returning all 65 is the honest answer to a question that broad and returning
  nothing was the original bug. Measured on a mixed catalog, the two job queries
  above went from 7 matches to 6 and from 5 to 3, losing only tools that had
  matched on a function word.

- **A dropped web search now says so.** The repair that keeps a broken
  server-tool exchange from poisoning a session was correct and completely
  silent, and the silence turned out to be the worse half of the bug. The model
  reads a placeholder on the *next* request, so it can recover; the user saw a
  search in the transcript with no results, no error and no way to know the
  answer that followed had been reasoned without it. In the session that
  prompted this, several searches came back empty and nothing anywhere
  mentioned it.

  Each dropped exchange is now announced once, as a new `notice` event, naming
  whether a search or a fetch was lost and that it can be re-run. Once, not per
  turn: the broken exchange stays in history, so a transcript inherited from a
  resume would otherwise carry a permanent banner. Two shapes are deliberately
  *not* announced, because a false alarm is indistinguishable from a real one —
  a server tool the API withheld because the same batch held client tools (it
  runs on the next request, by design) and a paused search whose two halves are
  still in separate messages.

- **The previous frame's chrome no longer shows through tab-indented output.**
  Reading a tab-indented source file produced lines with the status bar and the
  input placeholder embedded in the indentation — `266Klaudia…` and
  `268-opus-5 · auto-e}` in a real session. The bleed runs were 8 and 16 columns
  wide, which is the signature: a literal HT advances the terminal's cursor to
  the next tab stop *without painting the cells it skips*, and Bubble Tea writes
  a queued scrollback line straight over the rows the last frame occupied,
  appending `EraseLineRight` only at the end of the line. Every cell a tab
  jumped over kept the pixels the prompt box, placeholder or status line had
  left there.

  A second defect compounded it. `fitScrollback` measures lines with
  `ansi.StringWidth`, which scores a tab as **zero** columns — HT is an
  `ExecuteAction`, not a `PrintAction` — so a tab-heavy line was judged short,
  skipped the wrap, and was handed to the terminal to wrap onto rows Klaudia
  never erases. That is the same class of bug `fitScrollback` already exists to
  prevent, arriving by a different route.

  Tabs are now expanded to real tab stops (multiples of 8, counted in columns
  with escape sequences excluded so a colour code cannot shift a later stop) in
  `fitScrollback` — the single choke point every printed block passes through,
  and before the width measurement, so one change fixes both. It is done there
  rather than in `baseStyle` deliberately: `TabWidth(NoTabConversion)` is still
  set, `m.transcript` still holds the literal tabs, and `/copy` and `/export`
  still paste `go test` and TSV output with its columns intact. lipgloss's own
  expansion was not an option — it writes four spaces per tab regardless of
  column, which is what destroys that alignment and why it was disabled in the
  first place.

- **A tool call carrying one extra property is no longer thrown away.** Tool
  input schemas are generated with `additionalProperties: false`, and that was
  enforced at dispatch, so a model that supplied every field correctly plus one
  the tool has no field for got `Input validation error` instead of a tool run.
  From a live session: four consecutive `AskUserQuestion` calls with a valid
  question and valid options, rejected for a stray top-level `description`.
  Rejecting it bought nothing — `json.Unmarshal` drops unknown fields anyway, so
  the call would have worked.

  The advertised schema still says the shape is closed, because telling the
  model the exact shape is useful; the validator compiled from it no longer
  does. Field names the tool cares about are still protected: a missing
  `required` property or a wrong type fails as before, so the case the
  field-list hint was written for (`line_start` in place of `offset`, which
  leaves `file_path` missing) still self-corrects.

- **A rejected input is no longer diagnosed as a wedged environment.** The
  second loop-breaker splits on whether the model varied its inputs across a
  run of same-shape failures: varied inputs read as the environment being
  broken, identical ones as a guessing loop. But a call refused *before* the
  tool runs — unrecognised name, input that doesn't validate, permission denied
  — produces character-identical text however much the input varies, so varying
  inputs steered straight into the environment directive. The `AskUserQuestion`
  loop above was answered with "This looks like an environment issue (shell
  wedged, leaked background process, network unreachable, filesystem broken)"
  and advised to run `KillShell`, for a schema error.

  Failures are now classified by where they came from, and only ones where the
  tool actually ran can be blamed on the environment. Pre-execution failures
  get the same-shape directive, which quotes the recurring error — the thing
  the model needed to read.

- **The repeated-failure breaker latched, and bricked Bash for the rest of the
  run.** From a live session: after two failures of the same shape, every
  subsequent Bash call — including `true`, `pwd` and `echo hello` — was refused
  in 0ms with the "environment issue" directive, long after whatever broke had
  passed. `KillShell` had nothing to kill and `Jobs` showed nothing running,
  because nothing was actually wedged.

  The streak that fires the directive was only ever cleared by a *successful*
  execution, and the directive is what prevents one — a latch, not a breaker.
  The streak is now cleared as the directive is sent, so the next call runs. The
  anti-loop property is unchanged: a tool that keeps failing rebuilds the streak
  and gets told again. Two host-gate paths already worked around the same
  deadlock locally; this fixes the general case.

- **The Bash tool now says that exit status is reported for it.** A model was
  seen appending `; echo "EXIT_STATUS: $?"` to capture a status Klaudia already
  surfaces as `[exit code N]` — which makes the shell exit 0, so the call is
  recorded as successful and the failure never reaches the loop's error
  tracking at all. The description says so, and says not to.

- **The trust posture is no longer read and written unsynchronised.**
  `HostGate.Policy` was a plain field, written by `/trust upgrade` from the
  Bubble Tea update loop and read by the gate on every tool call from the
  agent's goroutine — a data race on every session that changed posture
  mid-turn. `-race` never caught it because no test changed the policy while a
  turn was in flight. Adding the MCP trust probe gave it a third concurrent
  reader, which is what prompted looking.

  It is now an `atomic.Value` behind `Policy()`/`SetPolicy()`, so the field
  cannot be touched directly. The regression test drives all three access
  patterns at once and reports `WARNING: DATA RACE` against a plain field.

- **A paste made while Klaudia is working reaches the model.** Pasting mid-turn
  queues a steering message, and that path sent the paste *chip* rather than its
  payload: the model received the literal `[#1 pasted · 8 lines]` and nothing
  else. It then did the only thing it could and said the paste had not come
  through, which reads as a transport failure rather than a Klaudia bug — the
  reporting user and Klaudia each concluded the other end had dropped it.

  A regression, and a narrow one. Paste chips landed on 2026-08-21 with
  `promptValue()` at every submit site; mid-turn steering landed on 2026-08-24
  as a new submit path and was never taught about chips. `/goal` made it easy to
  hit rather than causing it — pasting a spec into a session that is already
  running is exactly the steering path.

  The steer box now carries both forms, as the idle prompt already did: the chip
  for the queued hint and for `↑` recall, the expansion for the agent.
  Expansion happens when the message is queued rather than when it is drained,
  because the queued branch resets the input without pushing history — the chip
  is then referenced nowhere, and the next reconcile would evict the payload out
  from under a later expansion.

  The same hole in the `!` path is fixed with it: pastes are accepted while
  idle, and a bang line submits from idle, so a pasted multi-line command was
  handed to the shell as the literal chip text.

- **Pasting into "answer in your own words" works.** The paste gate listed the
  states with an editable box, and so did `inputHeight`, and the two copies
  drifted: `stateAnsweringOther` was in one and not the other. A paste there was
  dropped entirely — no chip, no text, no message — in the one state whose
  answer is most likely to be a pasted log or diff, since it exists precisely
  because the offered options were wrong.

  Both now ask `editableInput`, and the four submit paths take their text from
  one `readInput` that returns the chip form and the expanded form together.
  Three bugs in this area were all the same mistake — a submit path reading the
  raw input and sending it — so the accessor that can give the wrong answer now
  has a single caller.

- **An enforcing session no longer prompts for every MCP call.** MCP tools were
  the one door the zone model did not reach: their intrinsic decision was `Ask`
  in every interactive mode regardless of trust, on the grounds that external
  code should never be auto-allowed by mode alone. In a session that actually
  uses MCP that produced a stream of prompts nobody could answer on the merits
  — *may `mcp__gsol__godot_game_time` run?* is not a question the user has the
  information to decide — and approving one bought a single qualified name.
  Nothing accumulated: renaming a server in `.mcp.json`, or reaching for the
  twenty-second tool on a server with sixty, started again from nothing. Rule
  matching is exact on the tool name, so `mcp__server__*` could not help.

  MCP calls now follow the trust posture, like every other tool. Enforcing acts;
  observing and off still ask. Plan mode still refuses, because that is about
  what the session is for rather than who vouches for the call, and `dontAsk`
  now proceeds when trust is enforcing instead of refusing for want of anyone to
  prompt — which is what makes MCP usable in unattended runs.

  What this gives up is documented rather than glossed: the classifier reads
  tool inputs and has no model of what an MCP server does, so an MCP call raises
  no concerns and is permitted. Following the posture means trusting the servers
  in `.mcp.json` roughly as much as the shell — the same bet configuring them
  already made. See `docs/trust.md`.

- **`ToolSearch` ranks matches instead of demanding every term.** Matching was
  a strict AND of substrings: a tool was returned only if *every* word of the
  query appeared in that one tool's name or description. Describing what you
  wanted therefore returned nothing, and the more precisely you described it the
  worse it got. With 65 Godot MCP tools loaded, `godot game time freeze runtime
  state digest` matched none of them, while the bare word `godot` matched all
  65 — including `godot_game_time`, whose own description contains *freeze*,
  *step* and *state*. The failure mode is quietly expensive: an empty result
  reads as "no such tool exists", so the obvious next move is to give up on the
  capability rather than re-word the query.

  Matching is now OR, scored and ranked, in tiers — an exact name beats a name
  substring, which beats a description substring, which beats a fuzzy
  subsequence on the name. Tools matching more of the query rank above tools
  matching it more strongly, so a specific query still lands on one tool.
  Fuzzy matching is deliberately confined to names: descriptions are long enough
  that almost any short pattern appears in them as some scattered subsequence.

  Results cap at 25, and the reply says when it truncated. Without a cap, OR
  matching would hand a broad query an entire MCP server's surface — undoing
  the context saving that deferred tools exist for.

  The ordered-subsequence scorer behind `@`-file completion moved from
  `internal/tui` to a new `internal/fuzzy` so both callers share one
  implementation, rather than the tool catalog growing a second copy. The
  domain-specific ranking stays with each caller; only the primitive is shared.

- **A reload restarts an MCP server whose process has died.** Hot reload left
  one gap: a server that crashed, or was killed from outside, was skipped by
  every subsequent reload and stayed unreachable until Klaudia restarted.
  `Reload` asked `Connected()`, which reports whether we still hold a
  `ClientSession` — and a stdio server whose child process has died keeps a
  non-nil one, because nothing nils it out. So the corpse read as "unchanged and
  running" and was left in place. Editing `.mcp.json` did nothing; the only way
  back was renaming the server's key, which made it look new rather than
  unchanged.

  Reload now probes with a 2s-bounded protocol `Ping` instead. The config
  comparison runs first, so the ping is paid only for servers that would
  otherwise have been kept, and a server that fails it is relaunched like any
  other changed one. `Connected()` keeps its cheap non-blocking meaning for
  `/mcp` and the tool wrappers, which want "is there a session" rather than "is
  the far end alive".

  Found while running two Godot MCP servers side by side, where killing a server
  by hand is routine; the regression test kills the peer and asserts the
  relaunch, and fails against the previous condition.

- **A refused host change no longer disables the tool for the rest of the turn.**
  The host gate refuses with the same text whatever the command was, so two
  refused commands looked to loop-breaker B like one error shape recurring
  across different inputs — its signature for "the environment is wedged". B
  then answers *every* later call to that tool without running it, including
  read-only ones, while reporting a shell that is perfectly healthy. Since the
  only thing that clears a streak is a successful execution, and B is what
  prevents one, the tool stayed dead until the next user message; approving the
  host change didn't help either, because approval cleared nothing.

  Gate refusals and declined asks are decisions, not malfunctions, and no longer
  feed the shape streak. Loop-breaker A now refuses with the same short-circuit
  B uses, so its own refusals can't feed B either, and a granted host change
  clears both counters for the tool.

- **A background job that exits immediately no longer races the status read.**
  `Start` called `j.status()` outside `s.mu` while the process-reaping goroutine
  wrote the same fields under it. Every other `status()` call site already held
  the lock; this one was the outlier.
- **The startup banner lists loaded skills.** Three sessions in a row could not
  establish whether skills were working, because there was no way to tell from
  outside the model: with none loaded there is no `Skill` tool to ask about, and
  with some loaded the only evidence was the model's own account of its tools.
  The banner now shows `skills: frontend-design` under the model line (first
  four names, then `+N more`), and omits the line entirely when none load —
  an empty `skills:` would read as breakage. Names come from the same list that
  backs the `/<name>` commands, so the banner cannot disagree with what will
  actually dispatch.

  Worth knowing about what is *not* automatic: a skill's name and description
  are in every request (they are the `Skill` tool's description), but its
  instructions are only loaded when the skill is invoked. That is the intended
  progressive disclosure — skill bodies run to thousands of tokens — so a model
  reporting "registered but not loaded" is describing correct behaviour.

- **Skills can be a directory: `.klaudia/skills/<name>/SKILL.md`.** The loader
  only read top-level `*.md` and skipped subdirectories before parsing
  anything, so a correctly written skill in the increasingly common
  directory-per-skill layout produced no skill, no warning, and — because the
  `Skill` tool is only registered when at least one skill loads — no tool
  either. Two sessions concluded from that that Klaudia had no skill machinery
  at all, which was the wrong lesson from true evidence.

  Both layouts now load, the directory names the skill when the frontmatter
  omits a name (`SKILL` would be useless), and a skill directory with no
  `SKILL.md` warns instead of vanishing. `skill.md` is accepted alongside
  `SKILL.md`, because a case-insensitive filesystem hides that difference until
  the skill reaches Linux.

- **Skills are read from `.claude/skills` too.** Both `~/.claude/skills` and
  `<project>/.claude/skills` are now searched, below their `.klaudia`
  equivalents, because that is where the ecosystem's installers put skills —
  the same reasoning that already has Klaudia reading `~/.claude/CLAUDE.md`.
  Precedence runs user-`.claude`, user-`.klaudia`, project-`.claude`,
  project-`.klaudia`, so a project can override an installed skill by name.

- **`/doctor` reports skills.** It lists what loaded and from which scope
  (project or user), and when nothing loaded it names the two directories to
  put skills in. This is the only place that can distinguish "no skills
  defined" from "skills are broken": with zero skills there is no `Skill` tool
  to ask about.

### Fixed
- **A long session hit `prompt is too long: 1000464 tokens > 1000000 maximum`
  and could not be resumed.** Three separate faults lined up, found from a
  four-day, 1174-record transcript in one project.

  Autocompaction never fired. Its trigger compares `EstimateTokens(messages)`
  against the window — but that only counts the message list, at ~4 chars per
  token, while the real request also carries the system prompt, the project
  instructions and every tool's JSON schema, and code tokenises closer to 3
  chars per token. For a 1M window the threshold sits at 967k, so an estimate
  running ~10% low never reaches it while the actual request sails past 1M. The
  estimate is now calibrated against the input size the API reports for each
  response — ground truth that includes the system prompt and tools — with the
  ratio only ever increasing, so a cache-heavy turn cannot undo a correction.

  The 400 was then unrecoverable: every resend is the same oversized request, so
  the session was finished. An overflow now forces a summarise-and-retry once
  per run, with the reason surfaced; a second overflow is reported rather than
  looped on.

  And the error text was unhelpful, because `isContextOverflow` matched several
  OpenAI-compatible phrasings but not Anthropic's own "prompt is too long". The
  report was a bare "Bad request (400)" with no mention of `/compact` or
  `contextWindow`.

- **A slow stream was killed as a stalled one.** Reported as a stall mid-turn,
  after text had already streamed — a healthy connection, and the second
  distinct cause behind the same message.

  The watchdog reset only when the SDK's `stream.Next()` returned a decoded
  event, and the SDK discards the API's keepalives outright (`case "ping":
  continue` in `packages/ssestream`). Anthropic sends those pings during
  exactly the pauses that matter — a large `tool_use` payload being assembled,
  extended thinking, the service under load — so a stream that was alive on the
  wire and merely slow was indistinguishable from a dead one, and got cancelled
  at 120s.

  Liveness is now measured in bytes off the socket: the transport wraps the
  response body and touches an activity tracker on every read, and the timer
  re-arms instead of firing while data is still arriving. A genuinely silent
  connection still trips it. Both are regression-tested — the ping test fails
  against the previous watchdog, and a hanging server must still stall.

  The message was misleading too: it said "auto-retried without success" even
  when no retry was attempted, which is the case whenever output has already
  been delivered (a re-issue would repeat what you just read). That path now
  says so, and points at the partial answer above it.

- **A session left idle came back with a six-minute stall.** Reconstructed from
  the transcript: 6h11m between the previous reply and the send that failed,
  then "the model connection stalled … auto-retried without success". Nothing
  was flaky, and the watchdog was doing its job — the layer below it was lying.

  The Anthropic API is HTTP/2, and Go's automatic HTTP/2 does no health
  checking by default (`http2.Transport.ReadIdleTimeout` is zero: never send a
  keepalive PING). A multiplexed connection killed by a sleep cycle, NAT or VPN
  therefore sits in the pool looking perfectly usable; the request is written
  into a black hole and the read blocks until the 120s stall watchdog fires.
  `IdleConnTimeout` does not save it, because that timer does not fire reliably
  across a sleep and the request can claim the corpse before cleanup runs. Each
  of the two retries then reused the pool and burned another 120s.

  Model calls now run on a transport with h2 keepalive pings (20s read-idle,
  10s ping timeout — both well inside the stall window, so a dead connection
  surfaces as a fast retryable error), and a stalled attempt drops idle
  connections before retrying so the next one dials.

- **An interrupted command was reported as a bare `[exit code -1]`, and got
  described back to the user as a timeout.** Interrupting a turn cancels the
  running command's context, but only `DeadlineExceeded` was recognised — a
  user cancellation fell through to a generic non-zero exit with no explanation.
  The two are indistinguishable at the shell (a killed process, no useful
  status), so the model filled the gap by guessing, and told the user a rebuild
  they had interrupted four minutes in had "hit my 15-minute tool timeout".
  Cancellation is now carried through as its own state and reported as such:
  interrupted by the user, explicitly not a timeout and not a failure of the
  command, with a note that whatever it had already done still took effect and
  the state is worth checking before re-running. Exit code 130, the convention
  for SIGINT, replaces the meaningless -1.
- **The server-tool repair deleted a web search the model was still waiting on
  (regression).** When the model calls a server tool and a client tool in the
  same batch, the API deliberately does *not* run the server tool: it returns
  `stop_reason: "tool_use"`, you return the client results, and it runs the
  search on the next request. Its `server_tool_use` therefore sits unanswered on
  purpose — and the unpaired-block repair read that as an orphan and deleted it,
  losing the search silently and breaking the request that followed. The repair
  now only treats an unanswered call as abandoned once the conversation has moved
  past that turn. Both shapes end assistant-then-user; what separates them is
  what the user message holds — only `tool_result`s means the turn is still being
  answered, ordinary text means nobody is coming back for it. Verified against
  both transcripts involved: the pending search survives untouched, and the three
  genuinely abandoned calls in the older session are still repaired.

### Added
- **A multiple-choice question always offers a way out of the multiple choice.**
  `AskUserQuestion` accepted digits `1..N` and silently swallowed every other
  key, so the only ways past a question were to pick one of its answers or kill
  the turn. That is the wrong shape for the situation the tool exists to handle:
  the *model* writes the options, so when its framing is off, a numbered list
  asks you to choose between four answers to the wrong question. The list now
  always carries one more entry the model did not write — "Something else —
  answer in your own words" — and picking it, or simply starting to type, opens
  a normal input box whose text is returned verbatim. `Esc` goes back to the
  options; the question stays live. The tool's description tells the model the
  answer may be none of its labels and to follow it as an instruction, including
  when it rejects the premise, rather than snapping it to the nearest option.
  Its result now reads "The user answered: …" instead of "chose", which is the
  truthful wording for both cases.

  Also closed while in there: answering a question whose turn had already been
  interrupted sent on a nil channel, which blocks forever and would have frozen
  the UI.
- **`input.enter` config: `Return` can insert a newline instead of sending.**
  `enter = "newline"` swaps the prompt's two chords — `Return` adds a line,
  `Alt+Return` or `Ctrl+J` sends. The default is unchanged.

  There is deliberately no `ctrl+enter` setting, because a terminal cannot
  produce one: `Return` and `Ctrl+Return` are the same byte (CR), and `Ctrl+J`
  is simply LF, which is why it has always been the newline chord. The Kitty
  keyboard protocol can disambiguate them, but Bubble Tea v1 does not parse
  those sequences — an enabled protocol would deliver `\x1b[13;5u` to the
  prompt as the literal text `13;5u`, and would also break every other `Ctrl`
  binding. So the supported route is to map the chord to `ESC` `CR` in the
  terminal, which arrives as `Alt+Return`; the README has the incantation for
  Ghostty, kitty, WezTerm, iTerm2, Windows Terminal and Apple Terminal, with
  the caveats (Windows Terminal binds `Alt+Return` to fullscreen; macOS
  Option-as-Meta costs you `é`).

  **Behaviour change:** `Alt+Return` previously sent, because the handler
  ignored the Alt modifier. It now inserts a newline in the default mode.
  `Ctrl+J` is honoured in both modes with no terminal support at all, so a
  mistyped `input.enter` (or a terminal that cannot send `Alt+Return`) can
  never leave the prompt unable to send.

  Caught while implementing: the queue-while-running path read `tea.KeyEnter`
  directly rather than going through the new decision, so in `newline` mode a
  `Return` meant to add a line queued a follow-up instead. Both paths now share
  one function, and a test asserts they agree.

- **The read-only sub-agents can reach read-only MCP tools.** `Explore` and
  `Plan` were limited to `Read`, `Glob` and `Grep`, so the two agents whose whole
  purpose is read-only fan-out research could not touch a wiki, an issue tracker
  or a chat archive. Naming MCP tools in their whitelist was never an option:
  they are discovered at connect time and differ per project.

  This is also where the context argument points. Searching four sources to
  answer one question is exactly the work that should not run in the main
  thread — a sub-agent spends its own window and hands back a summary.

  Eligibility is read from what a tool declares, not from what its agent is
  asked to do. A tool qualifies by setting the protocol's `readOnlyHint`, and a
  tool that says nothing is treated as a write, because the annotation is
  optional and the safe reading of silence is that nobody considered it. That
  matters: `mcp__gitea__delete_branch` is one connected server away from an
  agent whose only previous guarantee was a system prompt asking it not to
  write, and there is a test that fails if it ever arrives.

  For a server that annotates nothing — including one launched in its own
  read-only mode, where the operator knows something the protocol was not told —
  `"readOnly": true` on the server in `.mcp.json` says so. `"readOnly": false`
  says the opposite: `readOnlyHint` is a claim a server makes about itself and
  nothing verifies it, so an operator who does not believe a third-party server
  can decline to take its word without giving up the server, which the main
  agent keeps and still asks about before every call. Unset trusts the
  annotations. Measured against the case that prompted this: gitea-mcp annotates
  all 54 of its tools, 33 of them read-only, matching exactly what its own `-r`
  flag exposes.
- **Klaudia tells your working-tree changes apart from its own.** `/changes`
  splits them three ways: yours, Klaudia's, and *both* — a file it wrote that
  was already dirty or that you edited afterwards. Klaudia cannot merge those
  two changes, but it can refuse to pretend they are not there, which is what
  keeps undo and `/commit` honest. Startup says once how many files were already
  modified, for the case where you had forgotten.
- **`/undo`, and it cannot destroy work you did.** Before a turn writes a file,
  its contents are stored as a git blob with `git hash-object -w` — a plain
  object write that leaves your index, HEAD and `git status` untouched. A stash
  would have been simpler and is wrong: it moves the whole working tree
  including your unrelated edits. Undo shows the plan first, including the
  equivalent `git cat-file -p <sha> > path` for each file, because "undo 2
  files?" is a promise rather than an inspection. Files you also touched are
  skipped and named.
- **The completion block says what was *not* verified.** "auth tests 83/83
  passing" reads as proof until you notice the full suite was never run. A
  targeted run now names its subset and reports the gap, and files changed with
  nothing run at all says so outright.
- **`/context`, `/pin`, `/unpin`, `/forget`.** What Klaudia has read, changed
  and been working in, instead of a token percentage — with a closing line
  saying that list is what it looked at, not what the task needed. A pinned file
  is re-stated every turn, which is how it survives compaction; a file mentioned
  once forty turns ago is not really in context at all.
- **Resume reconciles the work, not just the chat.** The working tree is
  re-read, ownership is recovered from the transcript's own Write/Edit calls,
  and jobs are reported as stopped — they were children of a process that has
  exited, and the conversation you are resuming implies they are still up.
  Approvals are deliberately not restored: session-scoped means session-scoped,
  and resurrecting them would be the flaky remembered permissions the trust
  model replaced.
- **Meaningful exit codes** for headless runs: 0 done, 1 failed, 2 invoked
  wrongly, 3 hit `--max-turns`, 4 needed a host change with no way to ask, 130
  interrupted. 4 is the one worth wiring up — it distinguishes "the task needed
  a package installed" from "the model got it wrong".

### Fixed
- **A `.mcp.json` with comments loaded no servers, and said nothing.** The file
  is read with `encoding/json`, which rejects `//` and `/* */` — and this
  README documents `.mcp.json` *with* comments in two places, so the
  documented example was the broken case. Worse, `LoadConfig`'s error was
  discarded at the call site (`mcpCfg, _ := mcp.LoadConfig(cwd)`), while
  *connection* errors two lines below were reported. A stray comment therefore
  produced a session with no MCP tools, no warning, and a model that
  confidently reported the server was down — which is exactly how it was
  found, wiring a stub ledger server into a new project.

  Comments are now stripped before parsing (outside strings, so a
  `"https://…"` URL keeps its slashes), and a config that still does not parse
  is reported as `warning: mcp config: …`. Measured against a stub MCP server
  that logs every JSON-RPC request: 0 requests before the fix with a commented
  config, 3 after. Comment bytes are replaced with spaces rather than removed
  so that a parse error's offset still points at the right line.

- **A sub-agent showed nothing at all while it worked.** A twenty-minute
  research run and a hang were indistinguishable: one `⚙ Agent` line, then a
  spinner, then silence. The cause was structural rather than cosmetic — the
  child loop was started with a nil emitter (`spawner.go`), so it could not
  report even in principle, and no seam existed to carry its events out. Tools
  can now report progress through a `Progress` callback on `tools.Context`; the
  Agent tool passes it down, and the child's tool calls come back as
  `tool_progress` events rendered indented under the `Agent` line ("`↳ Read
  pricing.go`"). Only tool calls are relayed, not the child's prose — what you
  need while waiting is the shape of the work, not a second voice in the
  transcript. Progress also counts as activity, so the "quiet for…" hint no
  longer fires while a sub-agent is visibly busy.

- **A sub-agent could run unbounded, and compacted far too early.** It took
  `--max-turns` (default `0`, unlimited) — defensible for the main loop, where
  you watch each step and can interrupt, but not for a child you cannot see. It
  now defaults to a 50-turn bound and says so when it stops there, instead of
  returning a truncated answer as though it were finished. Its context window
  was also passed as `0`, falling back to the 200k compaction default, so a
  sub-agent on a 1M-token model summarised its history at a fifth of the room it
  had; it now inherits the model's real window.

- **The unpaired-server-tool repair broke valid paused searches (regression).**
  The fix below repaired a `server_tool_use` with no result, but checked each
  message on its own. A paused server tool (`stop_reason: pause_turn`) records
  its call in one assistant message and its result in the next, and the two are
  only a visible pair *after* same-role messages are merged — which happened
  later in the pipeline. So a perfectly good paused search was read as an orphan,
  its call dropped, and its result left stranded, trading one 400 for its mirror:
  ``unexpected `tool_use_id` found in `web_search_tool_result` blocks … must have
  a corresponding `server_tool_use` block before it``. The merge now runs first,
  and the pairing check is order-aware and symmetric — it drops a call with no
  result *and* a result with no preceding call, either of which the API rejects.
  Verified against the transcript that hit this: 5 unpaired blocks before, none
  after.

- **Interrupting a turn mid-web-search could brick the session.** Every
  subsequent message failed with ``messages.N: `web_search` tool use with id
  `srvtoolu_…` was found without a corresponding `web_search_tool_result`
  block``, and because the bad message stayed in history the session could be
  neither continued nor resumed. Unlike a client `tool_use` — answered by a
  `tool_result` in the *next* message, a case the sanitizer already repaired — a
  server tool's result belongs to the *same* assistant message, so an unpaired
  `server_tool_use` slipped through untouched. It arises whenever a turn is cut
  off with a search in flight, including from the `pause_turn` continuation the
  API uses for long searches, which records the paused turn before its result
  exists. The sanitizer now drops an unanswered `web_search`/`web_fetch`
  `server_tool_use` on the way out, leaving a note so the model knows the search
  was lost rather than having it vanish silently. Completed searches, and server
  tools this package doesn't model, are left untouched. Repairs already-poisoned
  transcripts on the next send, so an affected session recovers by itself.

### Changed
- **chromedp 0.15.1 → 0.16.0, cdproto 2026-04-27 → 2026-08-04.** Routine
  currency, prompted by the protocol-noise fix above: the four unhandled DOM
  events are present in every version including master, so the upgrade neither
  causes nor fixes them. Verified live on Chrome 152 — launch, navigate,
  DOM-settle wait and HTML→Markdown snapshot unchanged on two real pages, and
  chromedp logged nothing at all. The DuckDuckGo search parser returns no
  results for a headless profile with `headedFallback` off, on 0.16.0 and
  0.15.1 alike, so that is pre-existing and not from the bump.

- **Server web tools bumped to the current GA versions.** `web_search` and
  `web_fetch` now use the `20260318` tool types (were `20250305` / `20250910`),
  pinned to `allowed_callers: ["direct"]`. Direct mode is deliberate: from
  `20260209` on, the default runs search/fetch inside code execution ("dynamic
  filtering"), which nests results under a `code_execution_tool_result` — a
  different shape than the direct `web_search_result` blocks the recorder and
  citation repair handle — and 400s on pre-4.6 models. `direct` keeps the exact
  result/citation shape already round-tripped and works on every model. The
  result/citation handling is version-agnostic, so it needed no change. Dynamic
  filtering (its token-saving payoff) is a scoped follow-up: it needs
  code-execution result handling, a model-capability gate for `allowed_callers`,
  and a live smoke test. The original web-tool beta headers are kept (they are
  ignored now that the tools are GA, and dropping them is an untestable change to
  a working path).

### Fixed
- **A large tool call (e.g. writing a long file) failed with a cryptic schema
  error and then hung.** Writing a big document blew the 8192-token output cap
  mid-tool-call, so `stop_reason` was `max_tokens` and the tool's argument JSON
  arrived truncated. The stream layer patches truncated JSON to `{}` to keep the
  SDK from crashing, but the loop then dispatched that `{}` as a real call —
  `Write` with no `file_path`/`content` — surfacing "missing properties
  'file_path', 'content'" and looping as the model retried the same oversized
  write. Fixes: (1) a tool_use truncated by the output limit — `max_tokens` OR
  `model_context_window_exceeded` (Claude 4.5+ accepts input+max_tokens>context
  and stops this way instead of 400ing) — is no longer dispatched; it returns an
  actionable result telling the model its output was cut off, nothing ran, and to
  write less or continue. (2) The output cap is now the model's real maximum
  (`api.MaxOutputTokens`, doc-verified): 128000 for the 1M-context models
  (Opus 5/4.8/4.7/4.6, Sonnet 5/4.6, Fable 5), 64000 for the 200k-context models
  (Haiku 4.5, Opus 4.5, Sonnet 4.5), 8192 only for unknown/OpenAI-compatible
  models. Requesting the full cap is safe — on Claude 4.5+ an over-long request
  is accepted and stops gracefully rather than erroring. (3) A `maxTokens` config
  key overrides it (mirrors `contextWindow`), for a provider whose limit the
  table doesn't know. `turnnote` no longer points at a `--max-tokens` flag that
  never existed.

- **Chrome's protocol noise tore the frame during a browser tool.** Fetching a
  news page mid-turn printed `ERROR: unhandled node event
  *dom.EventAdRelatedStateUpdated` over the spinner row and the input box
  border, leaving the box's top edge spliced into the status line and the tool
  result. chromedp defaults its browser logger to `log.Printf`, so every message
  went to stderr — the same terminal the inline renderer is painting, with
  nothing coordinating the two.

  The first fix pointed chromedp's `WithLogf`/`WithErrorf` at a sink that
  discarded everything, which also threw away chromedp's real errors, so the
  stream is now split by cause. The noisy class is structural, not a symptom:
  chromedp routes DOM/Page events through a hand-written type switch
  (`target.go`) while cdproto's event types are generated from the protocol, so
  the switch permanently trails Chrome and everything newer than it is logged as
  an error. Against the pinned cdproto that is four DOM events —
  `AdRelatedStateUpdated`, `AdoptedStyleSheetsModified`,
  `AffectedByStartingStylesFlagUpdated`, `TopLayerElementsUpdated` — all
  carrying node state chromedp doesn't model, none actionable, and the first
  emitted once per update by any ad-carrying page. Those are dropped, matched as
  a class so the next protocol release doesn't reintroduce the tear. Everything
  else chromedp says (JSON/unmarshal failures, malformed messages, missing
  document root, executor bookkeeping) is kept in a small per-browser ring and
  attached to the error of the next browser operation that fails — `(chrome:
  …)` on the tool result, where it is worth reading. `KLAUDIA_BROWSER_LOG=<path>`
  still captures the unfiltered stream for debugging chromedp itself.

  The real fix belongs upstream, and its shape is now known: `cdp.Node` has
  `AdProvenance`, `AdoptedStyleSheets` and `AffectedByStartingStyles` fields, so
  three of the four events can be stored on the node the way the maintainer's
  own `ScrollableFlagUpdated` commit handled that one, and only
  `topLayerElementsUpdated` (no parameters, so no node) needs ignoring. A fork
  pinned by a `replace` directive was rejected because
  `go install pkg@version` ignores `replace`, so users installing from `@main`
  would silently get unpatched chromedp. Measured, not assumed: the gaps are in
  chromedp master as well as v0.15.1 and v0.16.0, against both the old and the
  current cdproto (checked by diffing cdproto's generated event types against
  the switch), so this is not us sitting on a stale dependency; no upstream
  issue or PR mentions any of the four (#1530, open since 2024, is the same
  class for an event since handled); and the unit tests build the dropped
  messages from the real cdproto types through chromedp's own format string, so
  they fail if either side changes. Not reproduced live: on Chrome
  152 none of the four fired across four ad-heavy news sites, and a killed
  Chrome logs nothing at all (chromedp only logs a read error when the JSON is
  syntactically broken) — the classification is verified from chromedp's source
  and unit tests rather than from a live capture.

  As a backstop for the next library to reach for the standard logger, the TUI
  points `log`'s output at `io.Discard` (or `KLAUDIA_LOG`) while the program
  runs, and restores the previous writer, flags and prefix on exit.

- **A web search poisoned the next turn with a 400.** After a successful search
  (the Anthropic-backend `web_search` server tool), the very next message failed
  with `citations.0.web_search_result_location.url: Value should have at least 1
  item`, and every follow-up re-sent the same bad history. Root cause was an SDK
  bug: before v1.68.0, converting a web-search citation to its request form
  copied the title and cited text but dropped the `url` and `encrypted_index`,
  both of which the API then requires back — so `ToParam()` produced an empty
  `url`. Fixed by upgrading the Anthropic SDK to v1.68.0, where the conversion
  copies both fields. A sanitize step still drops any empty-`url` web-search
  citation found in a transcript recorded by an older build (the value can't be
  recovered at send time; the annotated text is kept), so resuming an old
  session no longer 400s.

- **Typing a line that wrapped grew the input box but hid the text above it.**
  A soft-wrapping line made the box taller, yet instead of revealing the new
  wrapped row the earlier row scrolled out of view and a blank row appeared at
  the bottom. bubbles' textarea repositions its viewport during `Update` at the
  height it had *before* the box grew — and only ever scrolls toward the cursor,
  never back up — so growing the box afterwards left the view stranded past the
  top. Edits now size the textarea to its full height before the keystroke is
  applied, so a line that still fits never scrolls its top out; the box is then
  shrunk for display with the view already at the top.

- **Pasted attachments accumulated with no way to delete them.** Pasting parks
  the payload behind a short `[#N pasted · …]` chip; deleting the chip from the
  input never dropped the payload, and the counter only ever climbed, so
  repeated paste-then-delete looked like attachments piling up. The store now
  reconciles against what is actually referenced — the live input plus any
  recall-able history entry — so deleting a chip drops its attachment and the
  counter falls back to `#1` once nothing is outstanding. A chip still held by
  history is kept, so `↑` recall re-expands it exactly as before.

- **Queuing a message during a turn left a permanent hint smeared into the
  transcript.** "Klaudia will read this before its next step" was written to
  scrollback with appendLine — the immutable, terminal-owned layer — so a fresh
  copy stuck in the history every time a message was queued, on top of the live
  hint under the input that already showed the same state. Transient UI belongs
  only in the composited live region, which clears itself; it no longer touches
  scrollback. The live hint now also carries the reassurance the removed line
  had — the message is read at the next step, so it is not lost while Klaudia
  works.

- **Interrupt-to-send during a command could strand the message.** Queue a
  message, press Enter again to interrupt and send it now, and — if a tool was
  running — the message sometimes vanished: the turn cancelled but no new turn
  started, just an idle prompt. Two things drain the queue, on different
  goroutines: the agent's mid-turn steering poll and the UI's interrupt path.
  Cancelling an in-flight command completes its tool batch, which drives the
  agent straight into its post-batch poll — draining the message into the turn
  being killed, where it sat in history unanswered. Confirmed from the real
  transcript, which ended with the queued message as a trailing user turn with
  no reply. The interrupt now takes the message out of the queue before
  cancelling, so a late poll finds nothing, and the message always becomes the
  next turn.
- **Interrupting no longer loses track of running jobs.** Cancelling a turn
  kills the foreground command but leaves managed background jobs running (a dev
  server keeps its port). The resend turn now tells the model what it left up so
  it can decide whether each still matters for the new instruction — inspect,
  stop, or leave running. It does not kill them automatically; that is the
  model's call.

### Changed
- **Web tools are disambiguated and steer toward the best source.** The local
  Chrome-backed `WebSearch`/`WebFetch` are renamed `BrowserSearch`/`BrowserFetch`
  (joining `BrowserNavigate`/`BrowserSnapshot`) so they no longer shadow the
  Anthropic backend `web_search`/`web_fetch` server tools. Their descriptions now
  tell the model to prefer the built-in `web_search`/`web_fetch` when available
  (Claude models — cited, higher quality) and use the Chrome tools when it isn't
  (non-Claude providers) or when explicitly asked to drive the browser. Tool
  behaviour is unchanged; only the model-facing names and guidance moved.

- **Dependencies:** Anthropic SDK `v1.45.0 → v1.68.0` (fixes the web-search
  citation bug above; `go build`/`vet`/`test` clean, no source changes needed)
  and `invopop/jsonschema v0.13.0 → v0.14.0`. `govulncheck ./...` reports no
  vulnerabilities. `charmbracelet/bubbles`, `bubbletea`, and `lipgloss` are
  already at (or ahead of) the newest v1 releases, so there is nothing to bump on
  the v1 line. A v2 upgrade is deliberately **not** taken: v2's `textarea.
  SetHeight` calls `repositionView` (which would reintroduce the just-fixed
  wrapped-input scroll bug) and bubbletea v2 replaces the standard renderer with
  a rewrite (which would invalidate the inline reflow/last-column cursor math).
  Recorded so a future bump starts from the known incompatibilities.

- **Auto-resume skips a session that ended in a model refusal.** A refusal is
  tripped by the accumulated context, not the last message, so reviving that
  conversation makes the very next prompt — even an unrelated one — refuse too.
  Klaudia now starts fresh and says so ("Last session ended in a model refusal —
  starting fresh"), leaving the transcript on disk for an explicit --resume. Only
  the implicit pick-the-most-recent case; naming a session by --resume/--continue
  is honoured. Detected by the refusal's signature — an assistant turn with no
  content, or an older transcript's stop_reason — so it needs no new stored
  field.

### Fixed
- **A web search in the history could 400 the whole turn** with
  `web_search_tool_result.content ...: Input should be a valid array`. The
  transcript recorder marshalled the raw response message, and the SDK's
  response union types have no marshaller — so a server-tool result's content
  serialised as an object full of leaked field names
  (`OfBetaWebSearchResultBlockArray`) instead of an array. On resume that parsed
  back into an empty error block and the API rejected it. Two fixes: the
  recorder now stores the param form, which round-trips cleanly (RawJSON is not
  an option — after streaming the SDK sets it to the same leaky marshal); and
  sanitize drops an already-corrupted result and its server_tool_use before
  send, with a visible placeholder, so a transcript written before this fix
  resumes instead of failing. Verified against the real on-disk transcript that
  hit it.

- **A refused or truncated turn showed nothing but "✓ done".** Reported from a
  real session: three messages in a row got a silent completion, no answer above
  them, indistinguishable from a bug. Three stop reasons cause it — the model
  refused (`refusal`), ran out of output budget (`max_tokens`), or overflowed the
  context window — and all three rendered as dead air. Klaudia now names what
  happened and how to get out of it.

  Refusal is the one that compounds: the model's safety system can be tripped by
  earlier context rather than the latest message, so once a conversation has
  drifted into refused territory even "hi" keeps refusing — and an auto-resumed
  session brings the tainted history back with it. The note points at `/clear`
  (or `--new-session`), which is the actual way out. Headless runs no longer
  report a refusal as `success` with empty stdout: the payload carries the
  explanation and the exit code is non-zero, so a pipeline can branch on it.


### Changed
- **`/commit` stages only Klaudia-owned files.** A file it wrote that you also
  edited is left out and listed: the two changes cannot be separated without
  hunk-level surgery, and sweeping your edit into a commit describing Klaudia's
  work is the bug `/commit` already stopped doing once.

### Fixed
- **Resizing the terminal left a stack of orphaned prompt boxes.** Reported from
  a live drag-resize. Bubble Tea's inline renderer returns to the top of the live
  region with `CursorUp(linesRendered-1)`, where that count is the *logical* line
  count of the previous frame. On a resize it updates its width and calls repaint
  but does not reset the count — and the terminal has meanwhile reflowed the rows
  already on screen. A four-line region drawn at 120 columns occupies seven rows
  at 90, so the renderer moves up three when it needed six; the cursor lands
  inside the old frame, `EraseScreenBelow` clears from there, and the rows above
  survive. Every intermediate size in a drag deposits another band.

  The deficit is arithmetic, not a mystery: we rendered the previous frame, so we
  know each line's visual width, and the terminal wraps a line of width w into
  ceil(w/newWidth) rows. The next frame is now prefixed with exactly that many
  `CursorUp` plus an `EraseScreenBelow`, which puts the renderer back on the true
  top of the old frame. The escapes ride inside the frame rather than going
  straight to stdout, because the renderer owns that stream — `ansi.Truncate`
  keeps CSI sequences at zero width, which was verified rather than assumed.

  Separately, three of the four live-region lines were *exactly* the terminal
  width — measured. The renderer only appends `EraseLineRight` to lines narrower
  than the terminal, so a full-width line never erased what was to its right, and
  writing the last column parks the cursor in the pending-wrap state. The live
  region now stops one column short at every width, with a test asserting it.
- **A question could be shown with its escapes intact.** Seen in a real
  session: `On \"ask whether they want to change…\" \u2014 which did you mean?`.
  The transcript shows the model escaped its JSON twice, so one round of
  decoding — which is all that is correct — leaves backslashes as literal
  characters. Nothing was decoding it wrongly; it genuinely contained them. Short
  human-facing strings (a question, its option labels, a host-change summary) are
  now repaired on the way out, and only when the result parses cleanly as a
  quoted literal. Plans, diffs and anything multi-line or long are deliberately
  untouched: they legitimately contain backslashes, and rewriting real content
  would be worse than the bug.
- **A stale prompt box could be stranded in the middle of the conversation.**
  Seen mid-turn: box borders, "› Ask Klaudia…" and the status line sitting in
  scrollback with later tool output written across them. When `tea.Println`
  flushes new output, the renderer returns to the top of the live region with
  `CursorUp(linesRendered-1)` — a *logical* line count. Any live-region line
  that fills the terminal width is two physical rows, so the cursor lands inside
  the region and everything above it is stranded for good.

  The earlier fix stopped the input box and status line reaching the last
  column, but its test built an *idle* model — so it never rendered the two
  components that were still doing it: the streaming preview (truncated to
  exactly the width, off by one) and the approval prompts (not truncated at all;
  326 columns for a long path). The clamp now lives at one choke point covering
  the whole live region, and the test drives every state that renders something.
- **Scrollback lines longer than the terminal left residue on their last row.**
  Spotted in a screenshot of a real session: a 160-character prompt echoed at 149
  columns wrapped, and the short second row still showed "0k tokens" from the
  status line that had been there before. Bubble Tea appends EraseLineRight to a
  queued line only when it is *narrower* than the terminal, so an over-wide line
  gets none and the terminal's own wrap leaves a partial final row. Klaudia now
  wraps its own output to one column short of the terminal, making every physical
  row a line the renderer will clean up. The transcript keeps the unwrapped text,
  so /copy and /export are unchanged.
- **The status bar showed "ask" while the session was autonomous.** shortMode had
  no case for the mode added in the six-to-three collapse, so it fell through to
  the default. Of everything on that line, the mode is the one field that must
  not be wrong — it was telling the user Klaudia would stop and check while it
  was working straight through. There is now a test that every mode has its own
  label and no two share one.
- **The host gate told the model off and never told the user.** Reported from a
  live session: a blocked call printed a paragraph of policy as a red `✗`
  failure, the model quietly took another route, and the user — who might well
  have said yes — was never asked. The message instructed the model to call
  `RequestHostChange`, and the model, being a volunteer, generally didn't.

  The first instinct was to make the gate ask every time. That was wrong, and
  the sessions that prompted it prove why: both blocks were an incidental
  `2>/dev/null` and a scratch file in `/tmp`, where routing around is not a
  workaround but the correct answer. Asking would have been pure interruption,
  which is the prompt fatigue the design already avoids on purpose.

  So the split is by whether Klaudia can proceed. A block it can route around is
  a non-event: the refusal to the model is now three short sentences that prefer
  another route and mention the declaration tool second, and it draws as a muted
  `⊘ changes this machine: writes /dev/null — trying another way` rather than as
  a failure. A block it cannot route around still reaches the user, and the
  prompt grew a third answer — **(s)omething else** — because declining a host
  change usually means "not like that" rather than "give up"; it keeps the turn
  alive and the redirect lands before Klaudia's next action.

  The residual risk is the quiet one: giving up without saying so. A host change
  stopped and never approved is now named in the completion block under `Not
  done — needs your agreement`, judged at end of turn so the good path — gate
  stops it, model declares it properly, user agrees — is not misreported as
  undone.
- **`2>/dev/null` was gated as a host change.** Reported from a live session.
  Writing to a pseudo-device changes nothing about the machine, and discarding
  output is one of the commonest things a command does — a false prompt on it
  costs more than the guardrail is worth. `/dev/null`, `/dev/stdout`,
  `/dev/stderr`, `/dev/tty`, `/dev/fd/*`, `/dev/pts/*` and the random/zero
  sources are all ordinary now. Block devices deliberately are not: `dd
  of=/dev/disk2` still asks, which is the reason `/dev` is watched at all.
- **`/tmp` was a host change on macOS and not on Linux.** Found while fixing the
  above. macOS resolves `/tmp` to `/private/tmp`, and the resolved form was being
  added to the host prefixes — so writing a scratch file asked for permission on
  one machine and not the other. `/tmp` is scratch space everywhere now.
- **A dev server backgrounded with `&` bypassed the job system entirely.** Found
  by running the spec's agent-loop torture test: the model shell-backgrounded
  the server eleven times and then managed the processes by hand with `pkill`
  and `kill -9 $(lsof -ti:PORT)` — no name, no managed log, no crash detection,
  no restart. A model that already knows `&` will use `&`, and the timeout nudge
  cannot help because a self-backgrounded command returns immediately. Bash now
  refuses to detach something long-running and names `run_in_background`; the
  refusal fires only when the command both backgrounds *and* looks long-running,
  so `sleep 1 &` is nobody's business.
- **Ownership was recorded before the write, not after.** The stamp used to be
  taken from the `tool_use` event, which fires before the tool runs — so every
  file Klaudia edited looked like it had changed underneath, i.e. like *you* had
  edited it, and undo would have refused to restore its own work. A failed Write
  no longer claims a file it never wrote either.
- **Session teardown abandoned processes that were slow to stop.** KillAll sent
  SIGTERM and returned; the binary then exited, taking with it the goroutine
  that would have escalated to SIGKILL two seconds later. A server that ignores
  SIGTERM therefore kept its port forever — the exact symptom process groups
  were introduced to fix. Teardown now waits for each job to actually go. Found
  by the smoke test, which was itself checking too early.
- **A declared host change that was refused left no trace.** The guardrail only
  logged changes it *caught*, so a model that did the right thing — declared its
  intent up front and was told no — was invisible to /trust and to the exit
  code. It is recorded now, which is what makes exit 4 reliable.
- **The undo snapshot could race the write it was meant to precede.** A frontend
  learns about a tool from an event on a channel; a snapshot taken when that
  event arrives can happen after the write and capture the new contents as the
  "before". It now runs through a synchronous hook immediately before execution.

### Added
- **Long-running commands become managed jobs.** `npm run dev` used to either
  hold the agent until the timeout or vanish into an untracked process owning
  port 3000 for the afternoon. A job now has an id and a name you can say out
  loud (`npm run dev` → `dev`, derived rather than mapped from a table guessing
  at your vocabulary), a log file under `~/.klaudia/jobs/`, a port when it
  announces one, and a location — `local`, or the host it started on over ssh.
  `/jobs`, `/logs`, `/restart`, `/stopjob`, and the same reach for the model
  through `Jobs`, `BashOutput`, `RestartJob` and `KillShell`.

  **A crash is reported when it happens.** Nothing used to notice a job dying
  until something read it, so "why is the site down" got the answer "it's
  running fine".

  **Restart replaces the process in place** — same id, same name, same log, with
  a marker where the restart happened — and starting an already-running command
  hands back the existing job. Two dev servers fighting over one port is a
  confusing failure, and the loser looks like broken code.
- **`/logs` uses your pager, and follow mode cannot fight you.** `/logs <job>`
  hands `less` the real file (so `F` follows from there); `/logs -f` prints into
  the terminal's own scrollback, where scrolling up cannot be undone by new
  output arriving — there is no viewport to snap. `/logs --errors` pulls just
  the failure lines, stack traces intact, into the conversation, so a crash can
  reach the model without four thousand lines of request logging.
- **You can steer Klaudia while it works.** Typing "don't modify the API"
  mid-turn used to hold the text until the turn ended and send it afterwards, by
  which point the API had been modified. The correction now lands in the request
  that decides the next action. `/stop` asks Klaudia to finish the current step
  and report what it did — "stop after this test run" should not throw away the
  test run.
- **`!command` runs the shell without leaving the conversation**, and its output
  becomes context, so "revert that" has a referent. The input marker switches
  from `›` to `$` as soon as the line starts with `!`, so what Enter will do is
  visible before you press it.
- **A turn ends with what changed and what was verified**, kept apart on
  purpose: conflating them is how "I fixed it" comes to mean "it compiles". Only
  test runners, typecheckers, linters and vet count as verification — a passing
  `go build` is not evidence the behaviour is right. Failures stay visible with
  their count. A turn that changed nothing prints nothing.

### Changed
- **Commands run in their own process group**, and stopping one signals the
  whole group. See Fixed.
- **Children are told there is no terminal**: `GIT_TERMINAL_PROMPT=0`,
  `GIT_PAGER=cat`, `PAGER=cat`, and the real `COLUMNS`/`LINES`. Credential
  helpers are untouched. Your own `$PAGER` still drives Klaudia's long-output
  view, which has a real terminal.
- **Programs needing a terminal are refused before launch**, naming the flag
  that would have worked: `git rebase --onto`, `git add --`, `-m`,
  `ssh <host> '<command>'`. Klaudia does not allocate a PTY — one would make
  `vim` "work" in a surface the model cannot drive and you cannot see, so the
  turn would look successful while sitting in an editor forever. **Known
  limitation:** terminal resize does not reach child processes.
- **A timed-out service says so.** A bare `exit 124` reads as "the command is
  broken" and invites a retry with a longer timeout; when the command looks like
  a service, the result names `run_in_background` instead.
- **Klaudia handles SIGINT.** It had no signal handling at all, which was
  survivable only because children shared its process group. Now that they do
  not, Ctrl+C cancels the run and tears jobs down properly.

### Fixed
- **Stopping a background command left its children running.** Nothing set
  `Setpgid`, so `exec.CommandContext` signalled only the shell, not what the
  shell started — verified with a probe: a `sleep 60` grandchild survived cancel
  and Wait. That single gap is why stopping a job did not stop it, why Esc
  during a command did not end it, and why the next `npm run dev` became a
  second copy. Kill now signals the group, SIGTERM then SIGKILL after two
  seconds so a server releases its port and flushes its log rather than being
  truncated mid-sentence.
- **Background output was never written down.** It lived in a slice that only
  grew, so an afternoon's dev server held every line it ever printed in
  Klaudia's heap, none of it pageable, searchable or readable after the session.

### Added
- **Klaudia works autonomously inside the project and asks before changing your
  machine.** The old model asked per command, which produced prompt fatigue for
  ordinary work and approvals that neither covered the operation you meant nor
  stopped at its edges. Every tool call is now classified into a zone: project
  work, network fetches and work on a remote host the task calls for all proceed
  without asking — including the destructive parts, because `rm -rf ./dist` and
  `git reset --hard` are ordinary. Changes to this machine, and local credential
  material, stop.

  A few consequences are deliberate and worth knowing. Build caches under `$HOME`
  (`~/.cache`, `~/go/pkg/mod`, `~/.npm`, `~/.cargo`, `~/.m2`) are project zone,
  or every build would prompt. The rest of `$HOME` is your data and is not
  protected — this protects the machine, not the home directory — but `~/.zshrc`,
  `~/.gitconfig` and `~/Library/LaunchAgents` are host, because they configure
  your login session and persist. A project at `/opt/app` or `/usr/local/src` is
  still the project. `sudo` is not itself the trigger: `sudo -u deploy
  ./scripts/deploy.sh` in the project is project work.

  `ssh staging sudo systemctl restart nginx` is the job you asked for; the same
  line without the `ssh` is a change to the machine you are typing on. Using a
  credential (`ssh -i ~/.ssh/key`, `curl --cert`) is ordinary; printing one
  (`cat ~/.ssh/id_rsa`) is not.

  **This is a guardrail against well-intentioned mistakes, not a security
  boundary.** It reads command lines and tool inputs; it does not observe what
  programs do, so a command that computes its own target or a package's
  postinstall hook goes past it. For enforcement the kernel applies, set
  `[sandbox] mode = "os"`. See [docs/trust.md](docs/trust.md).
- **`RequestHostChange`: one approval covers a whole operation.** Klaudia
  declares what it intends to change and why, in your terms — "install nginx and
  configure it as a development proxy" rather than `sudo apt-get install -y
  nginx`. Approving it covers the package install, the config directory, the
  write, the validate and the restart. Anything outside the scope stops and says
  "this wasn't part of what you approved"; declining fails that one tool call, so
  work already done stands.

  Approving one file inside a directory covers that directory, so the second step
  of an approved change does not ask again — but approving `/etc/hosts` does not
  hand over `/etc`, a request for a whole system directory is refused before it
  reaches you, and an install grant does not authorise a removal. Approvals are
  session-scoped and never written to disk. There is no always-allow for a host
  change: a standing permission to reconfigure your machine is one you cannot see
  and did not schedule the end of.
- **`/trust`** shows the guardrail's state, the approvals live this session and
  what they reach, what the classifier has found, and any allow/deny rules
  carried over from the per-command model. `/trust revoke <id>`, `/trust revoke
  all`, `/trust upgrade`, `/trust observe`, `/trust off`.
- **`--allow-host-changes`** for unattended runs on a machine you are willing to
  have reconfigured. Without it, headless runs still do project and remote work
  and refuse host changes with a message naming the flag — rather than the old
  behaviour of denying everything that would prompt, in silence.

### Changed
- **The status line counts running jobs.** A dev server holding a port was
  invisible until the next start collided with it. Shown only when something is
  up, so it costs nothing the rest of the time — and it fills the slot the mode
  segment vacated.
- **`bypass` is styled as a warning.** In the same dim grey as the token count,
  the one mode where nothing is checking anything read as ordinary chrome.
- **The status line no longer announces the mode when nothing is unusual.** Once
  autonomous became the default it was on the line in almost every session, and
  a segment that never changes stops being read — while still outranking the
  context percentage, which is the one number there anyone acts on. `plan`,
  `bypass` and the legacy modes still appear, and still survive first when a
  narrow terminal starts dropping segments; "working normally" does not.
- **Permission modes collapse from six to three:** `autonomous` (the new
  default), `plan`, `bypassPermissions`. The old set asked you to pick a stance
  on file edits versus commands versus network, which is a question about tool
  categories and never had a good answer; zones answer it now. `default`,
  `acceptEdits` and `dontAsk` stay valid so existing configs keep working, and
  are no longer offered as a choice. `autonomous` is refused unless the host
  guardrail is enforcing — without it, it would be `bypassPermissions` under
  another name.
- **A config that already has `[permissions]` rules starts in observe mode**,
  with a one-time notice: the classifier runs and `/trust` reports what it found,
  but nothing is refused and your per-action prompts continue until you run
  `/trust upgrade`. Existing allow/deny rules keep working; Klaudia no longer
  creates new ones.
- **`--loop` no longer requires `--dangerously-skip-permissions`.** It needed it
  only because no mode would edit a file in the project without asking, so
  running unattended meant turning off every check to get past prompts about
  ordinary work. Use `--permission-mode autonomous`.
- **`/commit` stages what Klaudia changed, not everything.** It ran `git add -A`,
  which swept up the half-finished change you left open in another editor and the
  scratch file you meant to delete, into a commit whose message described
  something else. It now stages only the files this session edited, lists what it
  is leaving out, and — if you staged something yourself — commits exactly that
  and adds nothing on top.
- **Sub-agents share the parent's guardrail and its approvals.** A sub-agent must
  not be a way around the boundary, and an approval you gave the parent should
  cover the child doing the work.

### Fixed
- **The Bash parser did not see output redirections**, so `echo x > /etc/hosts`
  looked like a harmless `echo`. It also silently fabricated paths from partial
  expansions — `> "$HOME/notes.txt"` was recorded as a write to `/notes.txt`, an
  absolute path that looks real and is not — and dropped whole commands whose
  program name came from a variable (`$SUDO apt-get install`). Words now carry
  whether their text is the whole story, and anything deciding what a command
  touches has to check.
- **Tools ran in Klaudia's process directory, not the project.** `WorkingDir` was
  declared and never set, so `cmd.Dir` was never assigned and Grep/Glob rooted
  themselves wherever Klaudia happened to start.
- **Sandbox roots were compared unresolved**, so on macOS a write to `/etc/foo`
  never matched the policy prefix `/private/etc` and looked harmless.

### Changed
- **The input is drawn in a box, and the status line is its caption.** A dim
  full-width "model · mode · turns · tokens" line reads as a status bar, and
  status bars belong pinned to the bottom of a window — so inline rendering,
  which leaves it wherever the cursor is, made it look misplaced. The position
  was never the problem: the line had nothing visible to belong to. Framing the
  input gives it one. The caption also drops whole segments rather than
  characters when the terminal is narrow, so it degrades to "opus-5 · ask"
  instead of "opus-5 · ask · 0 tur", and the box gives way to a bare input below
  30 columns or 10 rows, where the border costs more than it buys.
- **The default model is now `claude-opus-5`** (was `claude-sonnet-4-6`), and
  the `opus`/`sonnet` aliases track the current lineup (`claude-opus-5` /
  `claude-sonnet-5`); `fable` is added for `claude-fable-5`.
- **The TUI renders inline instead of taking over the screen.** Klaudia used the
  alternate screen with a custom viewport, which hid the shell scrollback you
  launched from, denied the terminal's own search and selection over the
  conversation, threw the session away on exit, and — measured — performed worse
  than the pager it replaced: re-wrapping the whole transcript on every streamed
  token cost 102µs at 10 turns, 476µs at 50 and 1880µs (plus 2MB of garbage) at
  200. Finished output now goes into real scrollback via `tea.Println` and only
  the input and status bar are redrawn in place, so scrolling, drag-to-select,
  terminal search, tmux copy mode and output-survives-exit all work again.
  Per-token cost is now flat in session length and allocation-free (~130ns).
  `PgUp`/`PgDn` and mouse capture are gone rather than reimplemented; `/theme`
  applies to new output only, since printed text cannot be restyled.
- **`Ctrl+C` no longer quits on the first press.** It now does the smallest
  useful thing available — interrupt a running turn, cancel an open prompt,
  clear a non-empty draft — and quits only when pressed twice in a row.
- **`/last` takes an argument** (`/last <n>`, `/last list`) and opens long output
  in `$PAGER` rather than printing it, so paging, search and copy are the
  pager's job.

### Added
- **Model discovery in `/model`.** With no argument it now asks the provider
  which models it actually serves and offers them as a picker, instead of
  requiring you to type an exact ID from memory. Both backends answer at
  `GET /v1/models` — Anthropic via the SDK (with the OAuth beta header the rest
  of the client sends), OpenAI-compatible endpoints by convention — exposed as
  an optional `api.ModelLister` so a future backend that can't enumerate still
  satisfies `api.Provider` and simply falls back to type-the-ID. Selecting a
  model also records the context window the provider reports for it.
- **`/copy`** — put the last answer, a code block, a tool result or the whole
  conversation on the system clipboard using OSC 52, so it works over SSH and
  inside tmux. It copies from raw sources, never from rendered output.
- **`/search`, `/outline`, `/errors`, `/show`** — index the session and report
  matches. Inline rendering means the app can't move the terminal's scroll
  position, so these print results rather than jumping.
- **`/open <path:line>`** — open a reference copied from a stack trace or
  compiler error at the right line in `$EDITOR`.
- **A `light` theme**, and `NO_COLOR` support. Every previous theme assumed a
  dark background, and glamour was hardcoded to a TrueColor profile that ignored
  `NO_COLOR` entirely.

### Fixed
- **Pasting is byte-exact.** bubbles' textarea sanitises all input, replacing
  tabs with four spaces and mapping `\r` and `\n` to newlines *independently* —
  so CRLF text arrived with every line doubled and pasted Go, Python or Makefile
  source silently lost its tabs. Pastes are now intercepted before the widget
  sees them; large or tab-bearing ones are stored verbatim and shown as a chip
  (`[#1 pasted · 42 lines]`) that expands on submit, which also stops a
  thousand-line paste from swamping the six-row input box.
- **Rendered code copies as source.** Glamour padded every line to the wrap
  width and indented code blocks, so a copied snippet carried a four-space
  prefix and dozens of trailing spaces. Margins and block backgrounds are
  dropped and the residual padding is stripped from the rendered string.
- **Tabs survive.** lipgloss expanded them to four spaces everywhere, destroying
  the column structure of anything tabular a tool printed.
- **Earlier tool output is recoverable.** Only the single most recent result was
  kept, so a long build log became unreachable as soon as any later tool ran. A
  bounded ring now holds the last 200 results, and the inline preview names the
  number to ask for.
- **Tool-result previews are rune-safe.** They were truncated with a byte slice
  that cut multi-byte characters in half and printed replacement characters.
- **`@` completion finds nested files.** It was prefix-only, so `@session.ts`
  could never match `src/auth/session.ts`. It is now fuzzy, caches the repo
  listing instead of re-walking it on every Tab keystroke, and ranks files
  Klaudia recently read or wrote first — the previous code discarded
  `search.Glob`'s mtime ordering by re-sorting alphabetically.
- **The model and the UI no longer share one truncated string.** `tools.Result`
  gained a `Full` field (surfaced as `agent.Event.FullContent`) carrying the
  untruncated output for local display only. Bash clamps what the model sees to
  protect the context window; `/last` now shows everything the command actually
  printed — 245 KB where the model saw 30 KB, in the end-to-end test — and says
  so. This also removes the TUI's coupling to a magic string in the tool's own
  output: it was parsing the spill-file path back out of the notice. That notice
  remains, because it is what lets the *model* read the elided middle.
- **Bash output keeps its tail.** Output over 30 KB was truncated head-only, so
  the part people actually want — the `FAIL` summary from `go test ./...`, the
  error that stopped a build, the end of a stack trace — was thrown away, for
  the model as well as the user. The same budget is now split head+tail, cut on
  line boundaries, and the untruncated text is written to `~/.klaudia/outputs/`
  and named in the notice, so `/last` shows the complete log and the model can
  grep it. Spill files are pruned after 24 hours. The old truncation also
  sliced bytes, which could cut a multi-byte character in half.
- **Context-window reporting was understating the window ~5×.** The static
  per-model table claimed 200K for the current lineup, on the theory that 1M
  needed the `context-1m-2025-08-07` beta that `DefaultBetas` doesn't send.
  `GET /v1/models` reports 1M for those models, so the status bar's `ctx N%`
  had been overstating context pressure accordingly. The table is corrected
  against the live endpoint, and `/model` now prefers the provider's own figure
  over any table at all. `humanTokens` gained an M tier — a 1M window rendered
  as the unreadable "1000.0k".
- **`Ctrl+U` and `Ctrl+D` reach the input.** They were bound to viewport paging
  and matched before the textarea saw them, so readline's kill-line and
  delete-forward never worked.

### Added
- **Prompt caching (Anthropic).** Requests now set `cache_control` breakpoints on
  the stable prefix (tools + system prompt) and a rolling conversation
  breakpoint, so each turn no longer re-pays full price for the whole history.
  Because the system prompt and tools are byte-stable across launches, this also
  caches across `--continue`/resume (verified live: the resumed turn reads the
  whole prior prefix from cache rather than re-creating it). Cache usage is
  reported in the result envelope (`cache_read_input_tokens` /
  `cache_creation_input_tokens`). Disable with `KLAUDIA_DISABLE_PROMPT_CACHE`.

### Fixed
- **Tool order is now stable.** `Registry.Names()` iterated a map, so the tool
  list (and thus the request's cached prefix) was shuffled every turn — which by
  itself defeated prompt caching. Names are now sorted; verified live to take
  cache reads from 0 to most of the prefix.
- **Streaming no longer hangs forever.** A stalled model stream (half-open SSE
  connection) used to leave the TUI stuck on "thinking…". An idle watchdog now
  breaks a stalled turn — transparently retrying when nothing has been emitted
  yet, otherwise failing with a clear timeout. Configurable via
  `KLAUDIA_STREAM_IDLE_TIMEOUT` (seconds, default 120; `0` disables). Covers both
  the Anthropic and OpenAI-compatible providers.
- **Long-context-credits 429** now reports honestly: the *"Usage credits are
  required for long context requests"* error is a billing gate, so the message no
  longer suggests retrying — it points at adding credits or reducing context.
- **Session resume "lost memory".** Auto-resume could land on an empty transcript
  left by a launch that recorded nothing (quit before typing, or every turn
  errored on an expired token), reporting "resumed" with no context. `MostRecent`
  now skips contentless transcripts, and transcripts are created lazily on first
  write so aborted launches leave no file behind.
- **Markdown files rendered as soup.** Reading a `.md` file in the TUI ran its
  `cat -n` output through the Markdown renderer, collapsing every line into one
  reflowed block with inlined line numbers. Line-numbered tool output is now
  shown verbatim.

### Changed
- Docs: corrected the prompt-caching status in `docs/parity.md` (planned, not
  implemented) and documented the streaming/reliability knobs in the README.
