---
name: review-pr
description: Thorough multi-aspect review of a branch or PR, handing each aspect (correctness, tests, error handling, comments, design, simplicity) to its own read-only sub-agent. Use for a pre-merge review; arguments may name a PR and/or the aspects wanted.
---
Run a review of a change in which each concern gets its own reviewer. You
coordinate; read-only sub-agents do the reviewing; you merge what they find.
Do not edit files.

Request: $ARGUMENTS

## 1. Collect the change

Work out the change being reviewed from the request:

- a PR number: `gh pr view <n>` (title, description, base branch) and
  `gh pr diff <n>`
- a branch or range: `git diff <base>...<branch>`
- nothing: the current branch against its merge base with `main` (or the
  repository's default branch), plus any uncommitted work

Record: the stated intent (PR description or commit messages), the list of
changed files (`--stat`), and the diff. If the diff is empty, say so and stop.

## 2. Choose the aspects

The aspects, and what each reviewer is told to look for:

- **correctness** — logic errors, broken edge cases, contract breaks for
  callers, concurrency and resource bugs, security problems in the new code
- **tests** — whether the changed behaviour is tested, whether the tests would
  fail if the change were reverted, missing edge and failure cases, tests that
  assert on implementation detail instead of behaviour
- **errors** — failures that are ignored, swallowed, logged and forgotten,
  reported as success, or turned into a misleading message; fallbacks that hide
  a real problem; cleanup missed on error paths
- **comments** — comments, docstrings, README and changelog text that the
  change made wrong, that describe what the code no longer does, or that promise
  more than it does
- **design** — types and interfaces that allow invalid states, leaky or
  premature abstractions, names that mislead, duplicated logic that already
  exists elsewhere in the codebase
- **simplicity** — code that could be shorter or plainer with the same
  behaviour: dead code, needless indirection, hand-rolled versions of standard
  library or project helpers

If the request names aspects, use those. Otherwise use correctness, tests and
errors always, plus comments when docs or comments changed, and design and
simplicity when the change adds new types or more than a couple of hundred
lines.

## 3. Hand each aspect to a sub-agent

For each chosen aspect call the Agent tool with `subagent_type` set to
`Explore` (it can read and search the repository but cannot change it). Each
sub-agent starts with no knowledge of this conversation, so its prompt must be
complete. Include:

- the repository path and the stated intent of the change
- the changed files, and the diff itself (if the diff is very large, give the
  changed files with their changed line ranges and tell the sub-agent to read
  them)
- its one aspect, with the description above, and an instruction to ignore
  other aspects
- the project rules that apply (quote the relevant lines of `AGENTS.md` /
  `CLAUDE.md` if they exist)
- what to return: findings only, each with file:line, the problem, how it shows
  up in practice, a suggested fix, and a confidence of high/medium/low; and to
  return "no findings" rather than padding

## 4. Merge and check

Collect the reports. Remove duplicates that several reviewers raised (keep the
clearest wording and note the overlap). Before reporting any high-severity
finding, open the code yourself and confirm it; drop what does not hold up and
downgrade what you cannot confirm to low confidence.

## 5. Report

Group the findings:

- **Must fix** — defects that would break real use or lose data
- **Should fix** — real problems of lower cost, and missing tests for changed
  behaviour
- **Consider** — design and simplification suggestions

Each finding: aspect, file:line, the problem, the fix. Finish with the aspects
that were reviewed, any that were skipped and why, and one line on what was
done well if something clearly was.
