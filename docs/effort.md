# Effort

Effort is how many tokens the model is allowed to spend on a response —
thinking, tool calls and text together. On the current Claude models thinking
is always on and effort is the control for how deep it goes; it replaced the
older `budget_tokens` knob.

Klaudia sends nothing unless you ask for a level, so out of the box you get the
provider's own default (high on most Claude models, medium on Opus 5.5 and
Haiku 5.5).

## Setting it

| Where | How | Scope |
| --- | --- | --- |
| Flag | `klaudia --effort xhigh` | one run |
| Environment | `KLAUDIA_EFFORT=medium` | your shell |
| Config | `effort = "high"` in `.klaudia/config.toml` | the project |
| Slash command | `/effort xhigh`, `/effort off` | the running session |

Precedence is flag → environment → config, so a shell default can override a
checked-in file and one run can override the shell. `/effort` beats all three
for the session it is typed in. `/effort` with no argument lists the levels and
marks the live one.

## The levels

| Level | What it is for |
| --- | --- |
| `low` | Classification, lookups, high-volume mechanical work |
| `medium` | Balanced agentic work where cost matters |
| `high` | Spends what the task needs; the default on most models |
| `xhigh` | Long-horizon coding and agentic work; the recommended start for coding |
| `max` | Maximum capability, no constraint on token spend |

## Both providers, one field

The level is set once on the canonical request and translated per provider:

- **Anthropic** — `output_config.effort`. No beta header: top-level effort is
  GA.
- **OpenAI-compatible** — `reasoning_effort`. The OpenAI vocabulary is a
  superset of ours (it adds `none` and `minimal` below `low`), so all five
  values pass through unchanged rather than being clamped.

That is deliberate. Configuring effort per provider, or mapping our levels onto
a smaller set for one of them, would make the same setting mean different
things depending on the backend — which is the problem the single field exists
to avoid.

When no level is set, neither field appears in the request at all. That matters
for OpenAI-compatible endpoints: older vLLM and llama.cpp builds reject an
unknown key outright, so `"reasoning_effort": ""` would break them.

## Model support

Not every model takes every level, and the API **rejects an unsupported
combination with a 400 rather than dropping to the nearest level it does
support** — so a wrong value is a dead session, not a slightly cheaper one.
Klaudia checks before sending: at startup `--effort` is validated against the
model, `/effort` refuses a level the current model cannot do, and `/model`
lowers the live level to the new model's ceiling and says so.

The table lives in `pkg/api/effort.go` (`modelMaxEffort`) and is keyed by
resolved model id, so aliases work. Keep it in step with `modelContextWindows`
and `modelMaxOutputTokens`: `TestModelTablesAgreeWithEachOther` asserts all
three agree offline, and `TestLiveModelTablesMatchProvider`
(`KLAUDIA_LIVE_MODELS=1`) checks the two sizing tables against `GET /v1/models`
and reports what to add. That test exists because the three tables missed the
Claude 5.5 lineup simultaneously and nothing said so.

A model that is **not** in the table is treated as supported, not unsupported.
That is the normal case for an OpenAI-compatible endpoint and for a Claude
snapshot newer than the table: the server is the authority on its own
capabilities, and refusing locally would be Klaudia inventing a limit the
provider never stated.

## max_tokens is a hard limit on thinking *and* text

At `xhigh` and `max` the model needs room to think before it answers, and
`max_tokens` bounds both halves together. Klaudia requests each Claude model's
true cap (128k on the 1M-context models), so this is handled for them.

The case that bites is a model nothing can size: it gets
`DefaultMaxOutputTokens` (8192), and a high level against that cap does not
error — it truncates mid-thought, which reads as the model losing the plot
rather than as a setting. `/doctor` warns when the resolved cap is under 64k and
the level is `xhigh` or `max`; set `maxTokens` in the config to fix it.

This is not hypothetical: when the 5.5 lineup shipped it was absent from
`modelMaxOutputTokens`, so Opus 5.5 ran at 8192 against a real cap of 128000 —
15.6× under — and the first casualty was a large `Write` truncated mid-tool-call.
`api.Resolve` now prefers the provider's figures over the table (from a
once-a-day cache) and asks live for any model the table cannot size, so an
unlisted model only reaches the floor when the endpoint will not say either.

## Changing it mid-session costs a cache read

Changing the top-level effort between requests invalidates the prompt cache for
the conversation's messages, so the next turn re-reads the whole prefix at full
input rate. On a long session that is the single most expensive thing a slash
command can do, which is why `/effort` says so every time.

Anthropic offers a cache-preserving alternative — per-message effort, a
`role: "system"` message carrying the new level, behind the
`mid-conversation-output-config-2026-07-01` beta header. Klaudia does **not**
use it. It is beta, and it is Anthropic-only: adopting it would make a mid-
session change cheap on one backend and expensive on the other, which is the
capability skew this feature was built to avoid. Revisit when there is an
equivalent on the OpenAI-compatible side.

## Sub-agents

Sub-agents inherit the session's level unless `[subagents] effort` says
otherwise:

```toml
[subagents]
effort = "low"
```

Inherit is the default on purpose. Explore fan-outs are the canonical
low-effort workload — many short, mechanical searches — and running them below
the main loop is a real saving, but a silent downgrade is the kind of change
that degrades results months later with nothing to attribute it to. The knob
exists so the trade can be measured on your own work before it is taken.

Like the sub-agent model, the level is a snapshot taken at startup: a later
`/effort` changes the main loop, not children already wired.

## Compaction

A compaction summary runs at `medium` regardless of the session's level, unless
the session is already at `low` or unset. Summarising is extraction from a
transcript the model already has in front of it, so `xhigh` or `max` buys
nothing there and would be paid on every compaction — the most frequent call
after the turn itself. A session at `low` keeps `low`: someone who asked for
low everywhere did not ask compaction to be the exception.
