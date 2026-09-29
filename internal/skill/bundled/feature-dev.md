---
name: feature-dev
description: Guided workflow for building a new feature - explore the codebase with sub-agents, settle open questions with the user, compare designs, implement, then verify and review. Use when the user asks for a non-trivial feature rather than a small fix.
---
Build a feature in stages, checking in with the user at the points where a wrong
guess is expensive. Track the stages with TodoWrite so progress is visible.

Feature requested: $ARGUMENTS

If no feature was described, ask the user what they want built and stop.

## 1. Understand the request

Restate the feature in two or three sentences: what the user will be able to do
afterwards, and how they will know it works. Note what is unclear.

## 2. Explore the codebase

Before designing anything, learn how this codebase already does similar things.
Launch two or three `Explore` sub-agents through the Agent tool, each with a
different question, for example:

- where does comparable existing functionality live, and how is it structured
  from entry point to storage or output?
- which modules, types and interfaces will this feature have to touch or
  extend, and who else depends on them?
- how is this area tested, and what conventions (naming, error handling,
  configuration, documentation) does the project follow?

Give each one the feature description and ask it to return a short summary plus
the list of files most worth reading. Then read those files yourself — the
summaries are a map, not a substitute. Also read the project's `AGENTS.md` /
`CLAUDE.md` and README sections for the area.

## 3. Resolve open questions

Now that you know the code, list what is still undecided: behaviour at the
edges, error handling, compatibility with existing data or config, scope
boundaries, performance expectations. Put these questions to the user (use
AskUserQuestion if it is available; otherwise ask in your reply and end the
turn). Do not design around guesses about things the user can simply answer.

## 4. Design

Produce two or three candidate approaches — for example the smallest change that
fits the existing structure, and a cleaner design that refactors more. A `Plan`
sub-agent can draft each one; give it the feature, the answers from step 3, and
the files that matter. For each approach give the files touched, the new pieces,
the trade-offs and the risks. Recommend one and say why, then wait for the user
to choose before writing code.

## 5. Implement

Implement the chosen design. Follow the conventions found in step 2; reuse
existing helpers instead of adding near-duplicates. Keep the change to what the
feature needs. Add or update tests alongside the code, and update documentation
or the changelog where the project keeps them.

## 6. Verify and review

Run the project's formatter, linters, build and tests, and fix what fails.
Then review your own change: run the `code-review` skill if it is available, or
send an `Explore` sub-agent the diff with a request to find defects. Fix the
findings that are real; tell the user about any you chose not to fix and why.

## 7. Report

Summarise what was built, the files changed, the design decisions made along the
way, what was tested and how, and anything left for later.
