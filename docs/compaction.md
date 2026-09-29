# Context Compaction

How Klaudia keeps a conversation within the model's context window.
Implemented in `internal/compaction` and driven from `internal/agent/loop.go`.

## Two stages, every turn

At the top of each agent turn:

1. **Microcompact** — fast, local, no model call. Elides old tool results when
   they dominate the context.
2. **Autocompact** — model-based summarization, only when nearing the window
   limit. Replaces the history with a summary.

Both can be disabled by env var (matching the JS reference):

```bash
DISABLE_COMPACT=1        # disable both stages
DISABLE_MICROCOMPACT=1   # disable only microcompact
DISABLE_AUTO_COMPACT=1   # disable only autocompact
```

Token counts are estimates (~4 chars/token for text, a flat per-item estimate
for images/documents) — close enough to drive the thresholds without a real
tokenizer. See `EstimateTokens` in `internal/compaction`.

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

Microcompact is skipped on models with preserved thinking (Claude Fable 5.1,
Claude Opus 5.5; `api.PreservesThinking`) — see below.

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

The summary request is sent without the conversation's system prompt and tools,
so it also goes out without the conversation's thinking blocks
(`BuildSummaryRequest`). The live history keeps them. After the summary replaces
the history, no earlier turn is sent again.

## Preserved thinking

On Claude Fable 5.1 and Claude Opus 5.5, a thinking block's signature records
the system prompt, the tool set and every message before it. If any of those
changes after the block is produced, the API rejects the next request that
replays the block. The rejection is a 400 for accounts created on or after
2026-08-31. Other accounts can choose to drop the stale reasoning instead. So on
these models, history that has already been sent must only grow, never change.

| Edit | On these models |
| --- | --- |
| Microcompact elides old tool results | Skipped. Pruning has no append-only form |
| Autocompact summary request | Sent without thinking blocks. Removing every block is always accepted |
| Autocompact replaces the history with the summary | Allowed. No earlier turn or thinking is replayed |
| `sanitizeMessages` repairs | Deterministic in the stored history. Not yet measured against the API |

Klaudia does not yet use server-side context editing (`clear_tool_uses`) or
on-demand compaction (`compact-2026-09-04`). The API accepts those edits.

### Divergence: persisted summaries

Beyond the JS scheme, each autocompact summary is offered to the CLI via
`agent.Options.OnSummary` and written alongside the transcript in
`~/.klaudia/sessions/<encoded-cwd>/<id>.summary.md`. On resume, Klaudia seeds the
conversation from that summary instead of replaying the whole transcript
(token-saving); `--full` forces a full replay. See `internal/session/summary.go`.
