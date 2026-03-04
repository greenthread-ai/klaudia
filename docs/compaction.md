# Context Compaction

How Klaudia manages conversation length to stay within the model's context window.

---

## Overview

Two-stage compaction runs at the top of every `agentLoop` turn:

```
agentLoop turn N:
├── 1. microcompact(messages)   — fast, local, no model call
├── 2. autocompact(messages)    — model-based summarization (if token threshold hit)
└── 3. callModel(messages)      — proceed with (possibly compacted) messages
```

Both are injected via dependency injection (`$3q()` in `06-app-ui.js:37696`),
making them mockable for testing.

---

## Stage 1: Microcompact (`microcompact`)

**File:** `05-app-core.js:113773`

Fast, client-side content removal. **Does not call the model.**

### What it does

1. Finds tool results older than the last 3 (`f3Y = 3`)
2. If total tool result tokens exceed 40K (`Z3Y`), removes old results
3. Replaces images/documents with `[image]` / `[document]` placeholders
4. Only acts if savings >= 20K tokens (`G3Y`)

### Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `f3Y` | 3 | Keep last N tool results untouched |
| `Z3Y` | 40000 | Tool result token threshold |
| `G3Y` | 20000 | Minimum tokens to save before acting |
| `dv8` | 2000 | Estimated tokens per image/document |

### Output

Returns `{ messages, compactionInfo }`. If compaction occurred, `compactionInfo`
contains a `microcompact_boundary` system message that gets yielded into the
conversation.

### Disable

```bash
DISABLE_MICROCOMPACT=1 node dist/cli.js ...
```

---

## Stage 2: Autocompact (`autocompactFn`)

**File:** `05-app-core.js:117432`

Model-based conversation summarization. **Calls the model** to generate a
summary of the full conversation, then replaces messages with the summary.

### When it triggers

Checked every turn via `shouldAutocompact()` (`05-app-core.js:117422`):

```
Token count > compactThreshold?
  compactThreshold = contextWindow - reserveTokens - 13000
  reserveTokens = min(modelReserve, 20000)
```

Roughly triggers at **~80% of context window utilization**.

### Token budget calculation (`calculateTokenThresholds`)

**File:** `05-app-core.js:117395`

```
effectiveWindow  = contextWindow - reserve (max 20K)
compactThreshold = effectiveWindow - 13000      ← autocompact triggers here
warningThreshold = effectiveWindow - 20000      ← UI warning shown
errorThreshold   = effectiveWindow - 20000      ← UI error shown
blockingLimit    = effectiveWindow - 3000       ← hard stop
```

### Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `S5Y` | 20000 | Max reserved tokens |
| `ov8` | 13000 | Buffer before compact threshold |
| `h5Y` | 20000 | Warning threshold offset |
| `I5Y` | 20000 | Error threshold offset |
| `av8` | 3000 | Blocking limit offset |

### How it compresses

Core implementation in `compactConversation()` (`05-app-core.js:115995`):

1. Run pre-compact hooks (`CP1()`)
2. Build a summarization request with compaction instructions
3. Call the model (`TD4()`) to generate a summary
4. Replace entire conversation history with:
   - **Boundary marker** — system message with `compact_boundary` subtype
   - **Summary** — assistant message(s) from the model (flagged `isCompactSummary: true`)
   - **Preserved attachments** — file attachments carried forward
   - **Hook results** — from pre-compact hooks
5. Clear all caches (`le()`, `se()`)

### Compaction result structure

```javascript
{
  boundaryMarker,           // System message with metadata
  summaryMessages,          // Assistant message(s) containing summary
  attachments,              // Preserved file attachments
  hookResults,              // Pre-compact hook results
  messagesToKeep,           // For partial compaction
  preCompactTokenCount,     // Tokens before
  postCompactTokenCount,    // Tokens after
  compactionUsage: {        // API usage for the summary call
    input_tokens,
    output_tokens,
    cache_read_input_tokens,
    cache_creation_input_tokens,
  },
}
```

### Cache interaction

- Autocompact **invalidates all prompt caches** — the message content changes
  completely, breaking cache chains
- The compaction call itself can use prompt caching (`tengu_compact_cache_prefix`
  feature flag) for the summarization request
- After compaction: `le()` clears microcompact tracking, `se()` clears
  comprehensive caches including model context state

### Disable

```bash
DISABLE_COMPACT=1 node dist/cli.js ...        # Disable all compaction
DISABLE_AUTO_COMPACT=1 node dist/cli.js ...   # Disable only autocompact
```

### Override threshold

```bash
CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=90 node dist/cli.js ...  # Trigger at 90% instead of ~80%
```

---

## Integration in `agentLoop`

**File:** `06-app-ui.js:37808-37862`

```javascript
// Step 1: Microcompact
let h = await H.microcompact(messages, toolUseContext, querySource);
messages = h.messages;
if (h.compactionInfo?.boundaryMessage) yield h.compactionInfo.boundaryMessage;

// Step 2: Autocompact
let { compactionResult } = await H.autocompact(
  messages, toolUseContext,
  { systemPrompt, userContext, systemContext, toolUseContext, forkContextMessages },
  querySource,
);

if (compactionResult) {
  // Log metrics (pre/post token counts, cache stats)
  let expanded = re(compactionResult);  // Expand to message array
  for (let msg of expanded) yield msg;  // Yield each message
  messages = expanded;                  // Replace messages
}

// Step 3: Proceed with (possibly compacted) messages
for await (let event of callModel({ messages, ... })) { ... }
```

---

## Key functions

| Name | Purpose | File | Line |
|------|---------|------|------|
| `microcompact` | Microcompact (was `Lg`) | 05-app-core.js | 113773 |
| `autocompactFn` | Autocompact (was `JX4`) | 05-app-core.js | 117432 |
| `shouldAutocompact` | Should autocompact trigger? (was `x5Y`) | 05-app-core.js | 117422 |
| `calculateTokenThresholds` | Token threshold calculator (was `tc`) | 05-app-core.js | 117395 |
| `I96` | Effective context window | 05-app-core.js | 117378 |
| `PQ6` | Compact threshold | 05-app-core.js | 117382 |
| `compactConversation` | Core autocompact, calls model (was `SG6`) | 05-app-core.js | 115995 |
| `ZD4` | Partial/selective compaction | 05-app-core.js | 116130 |
| `uP1` | Session memory compaction | 05-app-core.js | 116980 |
| `re` | Expand compaction result | 05-app-core.js | 115986 |
| `cv8` | Process microcompact boundaries | 05-app-core.js | 113727 |
| `countMessageTokens` | Count tokens in messages (was `ak`) | 05-app-core.js | 115045 |
| `le` | Clear microcompact caches | 05-app-core.js | 113717 |
| `se` | Clear all compaction caches | 05-app-core.js | 117365 |

---

## Remaining rename candidates

| Current | Suggested | Occurrences |
|---------|-----------|-------------|
| `I96` | `effectiveContextWindow` | ~3 |
| `PQ6` | `compactThreshold` | ~3 |
| `ZD4` | `partialCompact` | ~2 |
| `re` | `expandCompactionResult` | ~2 |
| `le` | `clearMicrocompactCaches` | ~3 |
| `se` | `clearCompactionCaches` | ~3 |
