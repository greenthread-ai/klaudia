# Changelog

Notable changes to Klaudia. The version tracks the Claude Code reference the Go
port mirrors (see `internal/version`).

## Unreleased

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
