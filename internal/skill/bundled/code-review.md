---
name: code-review
description: Review a change for bugs that would break real use - the uncommitted diff by default, or a named branch, commit range or PR number. Use when the user asks for a review of their changes.
---
Review a code change and report the defects in it. Do not edit files; the output
of this skill is a report.

Scope requested: $ARGUMENTS

## 1. Pin down the change

Decide exactly which change is under review before reading any of it:

- Nothing requested: the uncommitted work — `git diff HEAD` (staged and
  unstaged together), plus any untracked files `git status --short` lists. If
  the tree is clean, review the current branch against the branch it will merge
  into: `git diff $(git merge-base HEAD main)...HEAD` (try `master` or the
  remote's default branch if there is no `main`).
- A PR number: `gh pr view <n>` for the stated intent, `gh pr diff <n>` for the
  change.
- A branch, commit or range: `git diff <base>...<branch>`, or `git show <sha>`.

Say in one line what you are reviewing (for example "uncommitted changes to 4
files, +120/-30"). If the change is empty, say so and stop.

## 2. Learn the rules the change must follow

Read the project's instruction files that apply to the changed paths
(`AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`, and any in the changed
directories). A rule stated there is a requirement; a violation of one is a
finding, and you should quote the rule.

## 3. Read the change in context

A diff hunk rarely shows enough to judge it. For each changed function, read the
whole function and whatever it calls that changed. For each changed signature,
exported name, return value, error, config key or file format, search for the
callers and readers (Grep) and check they still agree with it. Look at the
tests that cover the changed code, and at whether new behaviour has any.

Look for, in rough order of cost to the user:

- wrong results: inverted conditions, off-by-one, wrong variable, stale value,
  a branch that can no longer be reached, a case the old code handled that the
  new code drops
- failures handled badly: errors ignored or swallowed, a failure reported as
  success, cleanup skipped on an early return, partial writes
- inputs at the edges: empty, nil, zero, very large, duplicate, concurrent,
  non-ASCII, a path with spaces, a file that does not exist
- concurrency: shared state without synchronisation, a lock held across a slow
  call, a goroutine or task that can leak or outlive what it uses
- security: untrusted input reaching a shell, a query, a path or a template;
  secrets logged or committed; a permission check bypassed
- contract breaks: a caller, a stored file or a documented behaviour the change
  silently invalidates
- missing tests for the behaviour that changed

## 4. Keep only what you can stand behind

For every candidate finding, confirm it by reading the code, not by pattern:
name the input or sequence of events that triggers it and what goes wrong.
Drop it if you cannot. Drop matters of taste, formatting a tool would fix,
and problems that were already there before this change unless the change makes
them worse. Fewer, certain findings are worth more than a long list the reader
must re-check.

## 5. Report

List findings most serious first. For each:

- **file:line** — one-sentence statement of the defect
- how it goes wrong: the triggering input or sequence and the observable result
- the fix, briefly (a sentence or a few lines of code)

Mark each as **bug**, **risk** (plausible but depends on something you could
not verify — say what) or **gap** (missing test or documentation). End with a
line on what you checked and did not check. If you found nothing that meets the
bar, say so plainly rather than inventing minor points.
