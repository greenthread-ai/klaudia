# Context Compaction

How Klaudia keeps a conversation within the model's context window.
Implemented in `pkg/compaction` and driven from `pkg/agent/loop.go`.

## Two stages, every turn

At the top of each agent turn:

1. **Microcompact** — fast, local, no model call. Elides old tool results when
   they dominate the context.
2. **Autocompact** — model-based summarization, only when nearing the window
   limit. Replaces the history with a summary.

Either can be turned off with `agent.Options.NoMicrocompact` /
`NoAutocompact`. These were the `DISABLE_*COMPACT` environment variables, read
on every turn by every loop in the process; they are fields now so an embedder
can set them per conversation. The CLI does not expose them — they were only
ever for debugging.

Token counts are estimates (~4 chars/token for text, a flat per-item estimate
for images/documents) — close enough to drive the thresholds without a real
tokenizer. See `EstimateTokens` in `pkg/compaction`.

## Microcompact

Drops the content of tool results older than the most recent few when they
dominate the context, replacing each with a short placeholder. It only acts when
the saving is worthwhile, so it's cheap and rarely disruptive.

| Constant | Value | Purpose |
| --- | --- | --- |
| `KeepLastNResults` | 3 | Recent tool results left untouched |
| `ToolResultTokenThreshold` | 40000 | Act only when tool-result tokens exceed this |
| `MinTokensToSave` | 20000 | Minimum saving before eliding |
| `EstimatedTokensPerImage` | 2000 | Flat estimate per image/document |
| `elidedPreviewBytes` | 120 | How much of the original survives in the placeholder |

### The placeholder is a handle

An elided result is not simply deleted. The placeholder names the original's
size, quotes its first non-empty line, and points at a spill file holding the
full text:

```
[Old tool result elided to save context; 70 bytes; began: ./pkg/agent/loop.go:349: for _, tu := range toolUses {; full text: /…/outputs/elided-123.log]
```

Without that, a pass left several byte-identical markers and the model had no
way to tell which call each had been, nor to get any of them back short of
re-running the tool. The preview and path cost a few dozen tokens against the
thousands a pass reclaims, and recovery is an ordinary `Read`.

The spill is supplied by the caller as a `Spiller` func, not imported, so this
package keeps no dependency on `internal/tools`; the agent loop passes the same
on-disk spill the per-result cap uses. A nil `Spiller` is valid and the
placeholder then promises no file.

`Microcompact` therefore runs two passes. `compact()` is called at the top of
every turn, so the first pass prices the placeholders *without* paths and
checks `MinTokensToSave`, letting the floor reject the pass before anything is
written — a single pass would write a fresh set of spill files on every turn a
conversation spent sitting just under that floor. The second pass spills and
recomputes the exact saving with the real paths in place.


## Autocompact

When the estimated token count exceeds the compaction threshold, Klaudia asks
the model to summarize the conversation and replaces the history with that
summary. Thresholds (`ComputeThresholds`):

```
reserve          = min(20000, contextWindow)         // halved for tiny windows
effectiveWindow  = contextWindow - reserve
compactThreshold = effectiveWindow - 13000           // autocompact triggers above this
blockingLimit    = effectiveWindow - 3000            // hard ceiling
```

`DefaultContextWindow` (200000) is assumed when the model's window is unknown.
That assumption is the expensive one on a large model: a 1M-context model whose
id was missing from `modelContextWindows` summarised its history at a fifth of
the room it had. `api.Resolve` narrows the gap by preferring the provider's figures
(`GET /v1/models`, cached for a day) over the table, and asking live for any model
the table cannot size, and deliberately leaves
the window at 0/"unknown" rather than guessing high when neither can — guessing
high would skip compaction until the request 400s, which is unrecoverable
whereas compacting early is merely wasteful.

The summary call (`agent.summarize`, behind both autocompact and `/compact`)
runs at the session's resolved output cap, and a summary that stops with
`max_tokens` is **refused**: the history is kept and a notice says why. It
used to request a fixed 4096 tokens and ignore the stop reason, so a long
session's summary ended mid-word and silently replaced everything after the
cut — the model then re-asked questions the user had already answered. An
unsummarised history can still be compacted later; a truncated one cannot be
recovered.

### Idle compaction (TUI)

A conversation at 85% or more of the compact threshold will be compacted on
its next turn anyway, mid-turn, with the user waiting. So after four idle
minutes — under the provider's five-minute prompt cache, so the summary
request reads the conversation from cache — the TUI compacts it then, with a
line saying so. Never with a draft in the input box, never once another turn
has started, and not at all with `idleCompact = false` in config. Headless
and stream-json runs have no idle time to use and keep compacting in-turn.

## Before either stage: the per-result cap

Compaction deals with history that has already accumulated. The cheaper defence
is not letting a single tool result become enormous in the first place, and that
happens at dispatch time in `pkg/tool/output.go`.

Two levels, deliberately:

1. **Tool-level, smart.** A tool that understands its own output clamps it
   itself. Bash is the only one today: it keeps a head *and* a tail, because the
   verdict (`FAIL`, the error that stopped the build, the last stack frame) is at
   the end, and it appends the exit-code annotation after clamping so that
   survives too.
2. **Loop-level, dumb, universal.** `tools.SpillStore.Cap` is the backstop the agent loop
   applies to whatever a tool returned. It is byte-level and content-blind — the
   weaker of the two — but it is the only one that covers tools nobody tuned,
   including every MCP tool.

Both clamp to `maxToolOutput` (30000 bytes, head-heavy at 20000/10000) and write
the untruncated text to `<config root>/outputs/<tool>-*.log` (`~/.klaudia`
unless `KLAUDIA_CONFIG_DIR` says otherwise — the host passes the directory in
`tools.SpillStore`; an empty one clamps without writing), appending a
`[full output: <path>]` notice so the model can read the elided middle. Files
older than 24h are pruned on each spill. Output already within budget is
returned unchanged, so the backstop never re-cuts a self-clamped result.

The UI never sees a truncated result: `tools.Result.Full` carries the complete
text in memory for local display, separately from what the model receives.

### And the per-message cap, after it

A per-result cap cannot see a batch. Five results that each stop just under
30 KB are 150 KB in one user message, and a turn is not limited to five calls:
twenty well-behaved `Grep`s is most of a 200K window, with nothing in the
per-result path finding anything wrong. Parallel dispatch makes that shape
common rather than theoretical.

`capMessage` (`pkg/agent/batchcap.go`) holds one turn's combined
tool-result text to `maxMessageOutput` (200000 bytes, ~50K tokens). Shares are
allotted by water-filling, not an equal split: a result already under its share
keeps every byte and the remainder goes to the ones that need it, because
trimming a 200-byte result to an equal share of a budget it was never going to
exhaust loses content to no purpose and frees nothing that matters.

Two things it leaves alone. Image parts, which the model cannot recover from a
log file. And the spill notice of a result the per-result cap already wrote: the
tail keeps that path, and a second spill would only preserve the already-clamped
copy. It runs after each call's events are emitted, so a local frontend still
shows what the tool produced.

### Divergence: persisted summaries

Beyond the JS scheme, each autocompact summary is offered to the CLI via
`agent.Options.OnSummary` and written alongside the transcript in
`~/.klaudia/sessions/<encoded-cwd>/<id>.summary.md`, and the turn's own
transcript gets a boundary line, `{"type":"system","subtype":"compact_boundary",
"content":<summary>}` (Claude Code's name for the same line; `/compact` writes
one too). On resume, Klaudia rebuilds the conversation as it stood live — the
last boundary's summary, then every entry recorded after it — instead of
replaying the whole transcript; `--full` forces a full replay.

Resume used to seed from the summary file **alone**, dropping everything after
the compaction: one restart lost 572 entries, 80 minutes of work, that the
resumed session then could not remember doing. Transcripts written before
boundary lines existed are cut at the summary file's `_Compacted <ts>_` stamp
instead. The boundary is written through the turn's recorder
(`agent.CompactionRecorder`), not `OnSummary`, because `OnSummary` is one per
process and an ACP process records each session to its own transcript. The
seeded summary also ends with an explicit closing line: the next prompt merges
into the same user message, and a summary ending `**Goal m` once reached the
model as `**Goal malright…`. See `internal/session/summary.go` and
`session.ReadForResume`.

`/rewind`'s "summarise up to here" is a partial compaction: the summary
replaces the conversation before a chosen prompt and the prompt and everything
after it stay verbatim. It is recorded as its own line,
`{"type":"system","subtype":"partial_compact","content":<summary>,"kept":N}`,
and resume keeps the last `N` entries before it. Not a `compact_boundary` with
an extra field: a build that predates it would read that as "everything above
is gone" and drop the kept messages, where an unknown subtype is skipped and
the session resumes in full. It writes no `.summary.md`, which is the fallback
for a summary of the whole conversation.
