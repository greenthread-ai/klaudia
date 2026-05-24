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

### Divergence: persisted summaries

Beyond the JS scheme, each autocompact summary is offered to the CLI via
`agent.Options.OnSummary` and written to `.klaudia/sessions/<id>.summary.md`. On
`--resume`, Klaudia seeds the conversation from that summary instead of replaying
the whole transcript (token-saving); `--full` forces a full replay. See
`internal/session/summary.go`.
