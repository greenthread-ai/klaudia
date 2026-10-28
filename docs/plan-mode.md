# Plan mode

Plan mode is read-only exploration that ends in a plan the user approves. The
intent is the loop most agent harnesses have converged on: research the request,
break it into a visible list of actionable steps, get agreement, then work
through the list marking progress and revising it when reality disagrees.

This document is the design record for that loop. The first half is what was
measured to be wrong with the implementation in October 2026; the second half is
the decisions that followed — five, all landed (see Order). Everything marked
**measured** was observed by probe or by reading the code path; everything marked
**assumed** is a judgement that has not been tested.

## What was wrong

### Plan mode gated execution, not mutation

**Fixed by D2.**

`tools.execClassDecision` refused *every* Bash call in plan mode with "plan mode
is read-only; command execution is not allowed". Read-only was implemented as
"no shell".

But executing is how you research. `Read`, `Glob` and `Grep` cover files;
`git log`, `git diff`, `go doc`, `go list -m` and `go build` have no tool
equivalent at all. **Measured:** a planning session in this repo was refused
`git status`, `git log`, `git diff`, `sed -n` and `grep`, and the plan it
produced shipped with the caveat "`main` is behind 13, I have not looked at what
landed" — the plan was wrong about the tree it was planning against, and the
mode had made checking impossible.

The axis is wrong, not the strictness. `agent.dispatch` runs the host gate
*before* the permission check and the gate does not consult the mode, so changes
to the machine Klaudia runs on are already stopped in every mode. What plan mode
uniquely protects is the user's working tree. So it should gate **mutation**,
and a command that only inspects should run.

The same cause restricted the `Plan` sub-agent to `Read`/`Glob`/`Grep`
(`docs/parity.md`), so delegating research did not work around it. (Fixed by
D2: Explore and Plan now hold `Bash` and run in plan mode.)

### A permission denial was counted as a tool failure

**Fixed by D1.**

`loop.go`: `permission.Deny` → `errBadCall` → `errResultKind(msg,
failurePreExec)` → `fs.bumpStreak`. A policy refusal was recorded as evidence
the tool was failing.

This is load-bearing for plan mode specifically, because the plan-mode message
is byte-identical whatever the command was. Two refused commands have the same
signature and different inputs, so `errStreak.varied` becomes true — and
`blocks()` then returns true for *any* input, not just a repeat. With
`repeatFailureLimit = 2`, the **second** refused command trips loop-breaker B,
which short-circuits with `repeatedShapeFailureMsg` and never consults the
permission check at all.

**Measured:** this produced two symptoms that looked like separate bugs.

- *"The gate does not reset on mode exit."* After `ExitPlanMode` flipped the
  session to autonomous, the next Bash call was still refused with the
  plan-mode text. It had not reached the gate: breaker B replayed the stale
  message. The branch then calls `fs.clearStreak`, which is why a bare `pwd`
  worked immediately afterwards and the identical command succeeded on retry.
  The live-mode plumbing (`Turn.Mode` → `tui.liveMode` →
  `permission.CurrentMode`) was correct throughout; there was never any stale
  mode state.
- *"The repeat-error guard blamed the model."* The refusal arrived wrapped in
  "Bash has failed with the SAME error 2 times in a row. Stop calling it until
  you've understood the error below." Both failures were the harness refusing
  on its own policy. The guard counted them as model mistakes and instructed
  the model to stop using the only tool that could do the job.

The exclusion this needs already exists for the host gate, and its comment
applies verbatim: a refusal "is a decision, not a malfunction. Its text is
identical whatever the command was". Permission denials were simply never added
to it. They are arguably the stronger case, because a permission denial can be
**revoked mid-run** — by `ExitPlanMode`, by `/mode` — at which point every count
derived from it is stale.

### The plan was discarded on approval

**Fixed by D3.**

`ExitPlanMode.Execute` passes the markdown to the frontend for display and, on
approval, returns the string `"Plan approved. Plan mode is now off — proceed
with the implementation."` Nothing structures it, stores it or tracks it. The
plan survives only as prose in conversation history, where compaction is free to
summarise it away. There is no step list, so nothing can mark progress against
it.

### Nothing rendered the task list, except ACP

**Fixed by D4.**

**Measured:** `todos := &TodoStore{}` is constructed inside
`internal/tools/registry_default.go` and never handed to a frontend.
`TodoStore.Items()` has exactly one caller in the repository and it is a test.
The store is write-only in production.

ACP is the exception: `acp/update.go` renders TodoWrite's *input* as first-class
ACP plan entries and deliberately does not report it as a tool call. So an
editor user gets a live checklist and the TUI — the default frontend — renders
TodoWrite as a generic tool call and shows no tracked list at all. `parity.md`
describes this accurately; it reads as a feature rather than as the gap it is
for every other frontend.

## Three plan representations, one codebase

This is the finding that shapes the rest. Klaudia has three answers to "where
does the plan live", none of which knows about the others.

| | Where it lives | Progress | Survives compaction | User can edit |
| --- | --- | --- | --- | --- |
| Plan mode (`ExitPlanMode`) | conversation prose | none | no | no |
| Goal mode (`internal/goal`) | `PRD.md` or `.klaudia/GOAL.md` | `- [ ]` checkboxes, counted | yes | yes |
| `TodoWrite` / `tasks.Store` | memory, two separate stores | status field | no | no |

Goal mode is the one that works. The spec is a file; `goal.CountUnchecked`
mechanically counts remaining boxes and rejects a completion claim that leaves
any unticked; `goal.RequiresStubFix` catches a tracker missing rows for phases
the body describes; each iteration's *prompt* is fixed and re-reads the spec,
which is the Ralph principle the goal loop is explicitly built on (the TUI
still passes the conversation along — see D5). The TUI shows iteration
progress in the status bar, and since D5 the spec's checklist in the live
panel. `.klaudia/` is gitignored, so the
default location does not litter the repo.

Plan mode is the one that does not persist anything at all — and it is the
entry point users actually reach, via `/plan`.

So the consolidation is not "merge two features". It is: **plan mode should
produce the artefact goal mode already knows how to execute and verify**, and
the in-memory todo list should become a view over that artefact rather than a
third source of truth.

`TodoWrite` and the four `Task*` tools are genuinely redundant: same shape, two
stores, and only `TodoWrite` is wired to anything (ACP). `TodoWrite` is also the
parity tool and the one ACP maps, so it is the interface that stays.

## The decisions

### D1 — A refused call is not a failed call

**Landed.** Permission denials stop feeding both loop breakers, by the same
route and for the same reason host-gate refusals already do.

The narrow alternative — clear the streak when the mode changes — was rejected.
It needs every mode-change route to remember to call it, and the host gate's
`fs.clear(key, tu.Name)` on approval exists precisely because that kind of
bookkeeping was already missed once. Not counting a decision as a malfunction is
the invariant; clearing afterwards is a patch over having counted it.

This lands first and alone. It is small, it is independent of the rest, and
until it is in place every other plan-mode fault is amplified into a latched
tool and a misdirected instruction to the model.

Implemented as a third `failureKind`, `failurePermission`, kept out of *both*
counters where `failureHostGate` is kept out of only the shape streak. Both
refusal routes are covered — plan mode's rule and the frontend's `Approver`
answering no — because both are revocable, and a counter fed by either outlives
the decision that produced it.

### D2 — Plan mode gates mutation, not execution

**Landed.** `Write`, `Edit` and `NotebookEdit` stay denied; that is already
right. Bash is classified, and the verdict is one of three:

- **only inspects** → runs, silently.
- **known to mutate** → denied, with a message naming what it would have
  changed.
- **cannot be determined** → *asks the user*, rather than being refused.

The ask is the important part. **Measured:** `trust.ClassifyCommandIn` cannot
answer this question — probed over 22 commands it reports zero effects for
`rm -rf build`, `git commit -m x`, `go build ./...` and `curl`, because its job
is the host boundary and ordinary project work is deliberately silent to it.
Empty `Effects` means "nothing worth gating", **not** "reads only". Gating plan
mode on it would let `rm -rf` through. `trust.readOnlyPrograms` is closer in
spirit but has no entry for `git`, `go` or `sed`, so it rejects every command the
measured session was blocked on.

So this needs a new classifier, and a new classifier is an allowlist, and
`CLAUDE.md` already records what allowlists do: they go stale the day something
ships, and the whole Claude 5.5 lineup was missing from three of them at once.
An allowlist that fails to a *prompt* degrades into a question; one that fails to
a *denial* degrades into the bug this document opens with. Hence the third
verdict.

Redirections must disqualify a command whatever the program (`echo` is
read-only; `echo x > f` is not — **measured**, this is the one case `trust`
already detects as a project write), and every component of a pipeline or `&&`
chain must clear the bar independently.

What the implementation settled that the design left open:

- **`trust.InspectCommand`** is the classifier, a sibling of
  `ClassifyCommandIn` rather than a new package. It reuses that file's
  unexported `unwrap`, `shellPayload` and `base`, so `sudo env timeout git log`
  and `bash -c '…'` are read the same way by both, and `CLAUDE.md` already puts
  command classification in `trust`'s remit. Three catalogs: `readOnlyPrograms`
  as the base, a subcommand table for `git` and `go`, and `writesWithFlag` for
  the programs that only write when asked (`sed -i`, `find -delete`, `sort -o`,
  `yq -i`) — the third exists because the bare forms, `sed -n '1,40p'` and
  `find . -name`, are a Read and a Glob and are what research runs.
- **`go build` and `go test` are denied**, not asked about, on the user's call:
  plan mode is for anything that does not modify files, and `go build
  ./cmd/klaudia` drops a binary in the working directory while `go test` runs
  arbitrary code. Telling `go build ./...` (cache only) from the single-main
  case needs the package graph, so the honest verdict is the strict one.
- **Unclassified multiplexers** — npm, cargo, docker, kubectl — are absent on
  purpose. They ask, which is the right answer for a tool whose grammar nobody
  has sat down and read, and is how the table is meant to grow.
- **`permission.PermissionRequest` gained `Raw`.** `Specifier` is the prefix,
  and the prefix of `git status && rm -rf build` is `git status` — a gate given
  only that would allow the line.
- **The research sub-agents are held read-only by mode, not by a short tool
  list.** `Explore` and `Plan` now carry Bash plus `Type.Mode = ModePlan`.
  Withholding the shell was the old enforcement and it withheld exactly the
  commands research is made of. The mode is both the stronger guarantee and the
  less crippling one; it is deliberately not applied over
  `bypassPermissions`, which means the user has taken responsibility for the
  session. They also stop taking a worktree: a child that cannot write has
  nothing to isolate.
- **The system-prompt clause names the available commands.** It used to say
  "commands with effects" are refused, which was both wrong (every command was)
  and useless as guidance — a model told that executing is risky stops
  executing, and so never finds out that `git log` was always going to be
  allowed.

### D3 — One plan store, with a durable artefact

**Landed.** A single store is the in-process source of truth. `TodoWrite`
writes to it. The four `Task*` tools either back onto it or are retired; they
have no consumer today, so retiring them costs nothing but a parity note.

An **approved** plan is written through to a file under `~/.klaudia/plans/`, in goal
mode's checkbox format. Approval is the line because an approved plan is by
definition multi-step work the user has blessed; a three-item ad-hoc todo list
should not touch the disk. **Assumed:** that this is the right line. The
alternative — persist everything — is simpler to reason about and costs a file
write per `TodoWrite` call.

Writing through buys three things the current prose plan does not have: it
survives compaction, it survives `--resume`, and the user can edit it when the
plan is wrong. Writing it in goal mode's checkbox
format means `CountUnchecked` and `RequiresStubFix` *could* verify a plan that
came from plan mode. Nothing calls them on one today — `goal.SpecPath` only
looks at `./PRD.md` and `.klaudia/GOAL.md` — so this is a latent capability,
not the consolidation an earlier draft of this doc claimed it was.

What the implementation settled:

- **`pkg/plan`** (was `internal/plan`) is the store, a leaf package depending on nothing in the
  tree. It is owned by the *caller* and passed in with
  `tools.WithPlanStore`, because the old store was constructed inside
  `DefaultRegistry` and handed to nobody — which is the whole reason the todo
  list was rendered by exactly one frontend, and from the tool *input* rather
  than any store.
- **The four `Task*` tools and `internal/tasks` are retired**, not re-backed.
  Nothing read that store; the tools were registered and mapped into the ACP
  and TUI label tables, and that was the extent of their existence. Two task
  systems with different update shapes and no shared state is how a plan ends
  up disagreeing with itself.
- **The artefact is `~/.klaudia/plans/<session-id>.md`**, mirroring Claude
  Code's `~/.claude/plans`. The first cut wrote one `.klaudia/PLAN.md` per
  project, so two sessions in the same repo silently overwrote each other's
  plan. Keying by session also means `--resume`, which reuses the id, keeps
  the same file. It is outside the project because a plan is per-request
  working state, not a spec: next to `.klaudia/GOAL.md` it invited confusing
  the two. A `plansDirectory`-style override is not implemented.
- **In-progress is `- [ ] step _(in progress)_`, not `- [~] step`.** `[~]` is
  not a checkbox to any Markdown renderer, and `goal.CountUnchecked` counts
  only `- [ ]` and `- [x]`, so a third box state would make an active step
  invisible to the verifier.
- **An approved plan with no steps yet writes `_No steps recorded yet._`**
  rather than an empty section, because `CountUnchecked` reads "no unchecked
  boxes" as done and a plan approved a second ago is the opposite of done.
- **A failed write never costs the model its plan.** `Replace` and `Approve`
  return the error and keep the in-memory list; `TodoWrite` and `ExitPlanMode`
  pass the message to the model as a note on an otherwise successful call. An
  unwritable `~/.klaudia/plans` degrades to a session with no artefact.
- **`ExitPlanMode`'s approval result now drives execution.** It was the bare
  sentence "Plan approved. Plan mode is now off — proceed with the
  implementation"; it now also tells the model to record the steps with
  `TodoWrite` before starting the first one and to revise them as it goes.
  Approving a plan should begin working through a visible list, not merely
  unlock the tools.
- **`Store.Observe`** delivers the current snapshot on subscribe, so a frontend
  needs no separate first read and cannot miss an update landing between the
  two. D4 did not end up using it (see below); it remains unused.

**Not done:** the headless (`-p`) path still has no approver, so a plan can be
recorded but never approved, and therefore never persisted. That is the
pre-existing shape of headless plan mode rather than something D3 changed.

### D4 — Frontends learn the plan from an event ✅ landed

The loop emits a plan event when the store changes, the way it already emits
`permission_mode`. ACP switches from sniffing `TodoWrite`'s input to consuming
the event; the TUI and stream-json gain rendering they have never had.

This corrects an earlier call in this design: a new capability is a field on
`agent.Turn` when the *frontend provides* something to the loop, which is what
kept `AskUserQuestion` dead over stream-json. A plan update goes the other way —
the loop telling the frontend — and the established mechanism for that is
`Event`. A `Turn` field here would be a capability the loop asks the frontend
for, which is not what this is.

**As built:**

- **The event.** `agent/planevent.go` emits `Event{Type: "plan", Plan:
  &snapshot}` after a tool batch whenever the store's `Rev` has moved. It
  compares revisions rather than subscribing with `Store.Observe`, for the
  three reasons recorded on `planTracker`: `Observe` has no unsubscribe and a
  TUI turn is a `Run`, so subscriptions would pile up for the session; an
  observer fires on whichever goroutine ran `TodoWrite`, which in a concurrent
  group is not the emitting one; and an in-stream event lands right after the
  `tool_result` it belongs to, in every frontend. The tracker is per `Run`, so
  each turn re-sends the current plan once — which is how ACP catches up after
  `session/load`.
- **TUI.** `tui/planview.go` draws a live panel above the prompt (an 8-row
  cap, completed steps folded, Ctrl+T collapses it, hidden once every step is
  done); in scrollback a `TodoWrite` result is one line, `plan updated · n/m
  done`, since the panel already shows the list.
- **stream-json.** The driver writes events verbatim, so `plan.Snapshot` and
  `plan.Item` carry snake_case JSON tags (`items`, `content`, `status`,
  `active_form`, `approved`, `path`, `rev`). Measured before the tags: the
  payload went out PascalCase, and a cleared plan as `"Items":null`. A cleared
  plan is now `"items":[]`.
- **ACP.** The `plan` session update is posted from the event. `TodoWrite`'s
  `tool_use` is still suppressed as a tool call but posts nothing: it arrives
  before the tool validates anything, so a rejected call used to show the
  client a plan that never existed. `session/load` replay still renders from
  the input — a reloaded history has no store to read.
- **One store per conversation**, which the per-frontend rendering exposed.
  The CLI built one store per process and wired it into the shared registry,
  so ACP's threads shared one list (and one plan file keyed by the process's
  session id), and a general-purpose sub-agent — which gets every tool —
  replaced the user's plan with its own `TodoWrite` list (measured: the test
  saw the user's steps become `[child step]`). Now `agent.Turn.Plan` sets
  `Options.Plan`, the loop hands that store to the tools as
  `tools.Context.Plans` — the same store it announces, so what a frontend
  shows is by construction what the tools changed — ACP opens one per session
  (`~/.klaudia/plans/<acp-session-id>.md`), and each sub-agent gets an
  in-memory one whose events reach nobody.

### D5 — Plan runs: the goal loop over the approved plan (opt-in) ✅ landed

Approval is a picker (the same one AskUserQuestion uses): *Yes — run it*
(`y`, the old behaviour: one turn), *Yes — and keep going until every step is
done*, and *No — keep planning* (`n`), which opens a box whose text reaches the
model as a steering message — so `tools.Planner` keeps its plain
approved/refused contract across all three frontends.

The second choice, and `/plan run [N]` later, run the goal loop with the
approved plan as its spec. What is shared is the driver's control flow: the
turn budget, the completion gate, the verification turn, the wrap-up turn, one
completion block per run. What is not: the prompts
(`goal.PlanIterationPrompt` and siblings tell the model to record progress with
`TodoWrite`, because the store owns the file and rewrites it), the branch
switch (a plan is approved on the user's branch), and the gate's source (the
store's snapshot, not a file parse). A plan run is also complete when every
step is, token or not — the steps are what the user approved.

`/plan run` over a step list nobody approved — the model kept it with
`TodoWrite`, outside plan mode — approves it first (`plan.Store.Approve` with
no prose, through `tui.Session.ApprovePlan`), writing the artefact, then runs
it. The panel shows such a list under the same "Plan" title, so refusing it
("No approved plan this session") read as a bug with the steps in view; asking
to run the list is the approval.

The live panel draws whichever list is active (`workList`): the plan, or during
`/goal run` the spec's checklist via `goal.Checklist` — the parser
`CountUnchecked` now shares, so the panel and the gate cannot disagree.

## Order

1. **D1** — permission denials stop feeding the loop breakers. Independent;
   lands alone. ✅ landed
2. **D2** — the mutation classifier and the three-verdict plan-mode gate. Makes
   plan mode usable for its stated purpose. ✅ landed
3. **D3** — one store, written to `~/.klaudia/plans/` on approval. ✅ landed
4. **D4** — the plan event, and rendering in the TUI, stream-json and ACP. ✅ landed
5. **D5** — plan runs: the approval picker and the goal loop over the approved
   plan; one live panel for plan and goal. ✅ landed

D1 is a bug fix. D2 is the one that makes plan mode work. D3 and D4 are the
consolidation, and are the point at which `internal/goal` and plan mode stop
being two features that solve the same problem differently.

## What this does not do

- ~~It does not merge the `/goal` *loop* into plan mode.~~ Revised 2026-10-27,
  see D5. The premise — "goal mode runs unattended with no history per
  iteration" — describes the goal loop's prompt, not its driver: the TUI passes
  the full conversation to every iteration. Battletesting showed the cost of
  the split: a plan the model abandoned at 1/3 simply stopped, with nothing to
  send it back.
- It does not make the plan file a committed artefact. It lives under
  `~/.klaudia/plans/`, outside the repository; `PRD.md` stays the opt-in
  committed form, as it is for goal mode.
- ~~It does not change what `ExitPlanMode` shows the user.~~ Revised by D5: the
  y/n prompt became a three-row picker (run it / keep going / keep planning),
  and the model-facing result is folded to one line on screen.
