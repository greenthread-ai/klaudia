# Terminal UX spec — what shipped, and where we went a different way

Two specs drove this work: **Phase 1, "Don't break my terminal"** (terminal
fundamentals, 12 sections, 5 acceptance criteria) and **Phase 2, "Agent Loop,
Shell Integration & Trust"** (20 sections, 15 acceptance criteria).

Both are now implemented. This is the record of *how*, written because reading
the spec alone would leave a false impression in both directions: several
checkboxes were satisfied by deleting code rather than adding it, and a handful
were deliberately not satisfied at all.

Everything below is the state of the branch, not the plan.

## The deviations, up front

These are the places where the shipped behaviour differs from a literal reading
of the spec. Each was a decision, not an oversight.

| Spec asks | What shipped | Why |
|---|---|---|
| §12: logs with independent scrollback, paging, search, and follow mode that doesn't "snap the viewport back down" | No log viewer. `/logs` hands the file to `$PAGER`; `/logs -f` prints into real scrollback | The snapping problem *only exists because* an app owns a viewport. Phase 1 deleted ours; rebuilding one to satisfy the checklist would reintroduce the bug the checklist is about. `less` does paging, search and selection-copy better anyway. |
| §11: `[j] jobs`, `[l] logs`, `[r] restart` key bindings | `/jobs`, `/logs`, `/restart` slash commands | A global single-letter key collides with typing the letter. |
| §13: "resize propagates to child processes" | **Not implemented.** `COLUMNS`/`LINES` are set; anything querying the terminal directly sees nothing | Needs a PTY, and a PTY would make `vim` and `top` "work" in a surface the model cannot drive and the user cannot see — the turn would look successful while sitting in an editor forever. Interactive programs are refused fast instead, naming the flag that would have worked. |
| §9: "routine reads don't flood the transcript" | **Partly met.** Tool lines match Claude Code's density; a 20-file investigation still produces 20 lines | Explicit user choice for familiarity over the spec's aggressive collapsing. The other half of §9 — the results-first completion block — did land. Worth revisiting after the agent-loop torture test. |
| §2: "the machine is a protected boundary" | A **command classifier**, not the OS sandbox | The sandbox (`internal/sandbox`, `mode = "os"`) is real kernel enforcement but breaks `go build`'s module cache and cannot tell remote work from local, which §6 requires. The classifier is a guardrail against well-intentioned mistakes; that phrasing is enforced by a test that fails on "protected", "secure", "cannot", "prevents". |
| §17: undo | Git blob checkpoints, never the index or a stash | A stash moves the *whole* working tree including the user's unrelated edits. |
| §19: "Jobs: local #1 postgres running" on resume | Jobs are always reported as **stopped** | They were children of a process that exited. Reporting them as running would be a lie the moment it was written. |
| §20: `klaudia review --json` subcommand | `-p "…" --output-format json` | The existing flag already does it; a subcommand would be a second way to say the same thing. |

## Phase 1 — Don't break my terminal

The five acceptance criteria all pass.

The largest change was subtractive. Klaudia used the alternate screen with a
custom viewport; that hid shell scrollback, denied the terminal's own search and
selection, threw the session away on exit, and — measured — performed *worse*
than what it replaced: re-wrapping the transcript on every streamed token cost
102µs at 10 turns, 476µs at 50, and 1880µs plus 2MB of garbage at 200. Inline
rendering made the per-token cost flat and allocation-free (~130ns), and gave
back scroll, drag-select, terminal search, tmux copy mode and
output-survives-exit for free.

The spec's own principle called it: *"Do not replace terminal-native behaviour
merely because Klaudia can implement its own version."* The viewport, the follow
flag, the paging keys and the mouse handler were all deleted rather than fixed.

Other notable pieces: byte-exact paste with placeholder chips (bubbles' textarea
sanitises input, mapping `\r` and `\n` independently), copy fidelity through
OSC 52 with glamour's document padding stripped at commit time, long output
recoverable through `$PAGER`, and a two-press Ctrl+C so a reflexive "stop the
running thing" cannot destroy the session.

## Phase 2 — Agent loop, shell integration and trust

### §1–8 Trust — [docs/trust.md](trust.md)

Zones replace per-command permission. Project, network and remote work is
autonomous — including the destructive parts, because `rm -rf ./dist` is
ordinary. Changes to *this machine* and local credential material stop.

`RequestHostChange` is what makes one approval cover an operation: the model
declares what it intends to change and why, and the package install, config
write, validate and restart that follow all proceed. Approvals are
session-scoped, never written to disk, and there is no always-allow.

Modes collapsed from six to three (autonomous, plan, bypass). Autonomous is
refused unless the guardrail is enforcing — without it, it would be
`bypassPermissions` under a friendlier name.

### §9–14 Loop, jobs, shell — [docs/jobs.md](jobs.md)

Long-running commands are managed jobs with names, disk-backed logs, crash
detection and restart-in-place. Corrections typed mid-turn reach the model
*before its next action*, not after the turn. `!command` runs the shell without
leaving the conversation, and its output becomes context.

### §15–19 Verification, ownership, undo, context, resume — [docs/working-tree.md](working-tree.md)

Changed and verified are kept apart, and what was *not* verified is stated.
Klaudia knows which working-tree changes are yours; `/undo` cannot touch a file
you also edited. `/context` shows what it has read rather than a token
percentage. Resume reconciles the world rather than replaying the chat.

### §20 Automation

Exit codes an automation can branch on — notably **4** for "needed a host change
and had no way to ask". Headless output is colour-free stable JSON, and a run
never stalls waiting for approval that cannot arrive.

## Bugs the spec work surfaced

Not features — things that were already broken and would have stayed broken.

- **Killing a background command left its children running.** Nothing set
  `Setpgid`, so only the shell was signalled, not what the shell started.
  Verified with a probe before fixing. One missing flag explained four separate
  spec symptoms.
- **Session teardown abandoned slow-to-stop processes** — SIGTERM sent, then the
  binary exited, taking the SIGKILL-escalation goroutine with it.
- **Ownership was recorded before the write**, so every file Klaudia edited
  looked like the user had edited it, and undo would have refused its own work.
- **The Bash parser did not see redirections** (`echo x > /etc/hosts` looked
  like a harmless `echo`) and fabricated absolute paths out of partial
  expansions.
- **Tools ran in Klaudia's process directory, not the project.**
- **Sandbox roots were compared unresolved**, so a macOS write to `/etc/foo`
  never matched `/private/etc`.
- **`/commit` ran `git add -A`**, sweeping unrelated dirty files into a commit
  describing something else.
- **The status bar's context percentage overstated pressure ~5×** — the model
  table said 200K for a lineup that is now 1M.

## The agent-loop torture test

`scripts/torture.sh` is the spec's own torture test, run for real. It builds a
27-file Go service with a deliberate concurrency bug, seeds the working tree
with uncommitted work belonging to the user, stands up a real sshd in a
container as "staging", and gives Klaudia one task that requires the whole arc.

The bug is two-layered on purpose, so the *obvious* fix is wrong. Rotation
deletes the old token immediately, so tabs get signed out — the reported
symptom, which the loadtest catches. The obvious fix is a grace map; it stops
the sign-outs and the unit tests stay green, but each concurrent refresh still
mints its own successor, so one user ends up with six live sessions. Nothing
catches that except the running server's log. All three states were verified by
hand before the agent ever saw it.

**Result:** every measurable item passed. 21 file inspections, 5 edits, 4 test
runs, 4 managed jobs, 15 log reads, 2 ssh commands, 7 loadtest runs, one
declared host change refused and reported rather than forced. Independently
verified afterwards: 1200 refreshes at 12 concurrent tabs with zero sign-outs
and zero drift, race detector clean, the user's uncommitted edit and scratch
file untouched, `/etc/hosts` unmodified, no job outliving the session.

Two items are marked unjudgeable rather than passed: "did the conversation
remain readable" needs a human, and "could the user redirect Klaudia" is not
reachable headlessly (steering has its own tests in `internal/agent`).

### What it found

**The model never used the job system.** In the first run it shell-backgrounded
the dev server with `(go run ./cmd/api &)` eleven times and then managed the
processes by hand with `pkill` and `kill -9 $(lsof -ti:8477)` — no name, no
managed log, no crash detection, no restart. Everything §11 built, bypassed,
because a model that already knows `&` will use `&`.

The timeout nudge could not catch it: a self-backgrounded command returns
immediately, so there is no timeout to nudge on. It had to be caught on the way
in. Bash now refuses to shell-background something that looks long-running and
names the alternative. In the second run the model hit that refusal once and
used `run_in_background` for every server start afterwards.

This is the clearest argument for running the test rather than reasoning about
it: the job system was complete, tested, and documented, and in practice it was
invisible.

## Verification

`scripts/smoke.sh` runs the whole thing against a live model: tools, resume,
plan mode, autonomous project work, the host boundary holding, remote work not
being gated locally, job lifecycle and port release, restart identity, shell
fidelity (`git commit` with no `-m` failing in ~3s rather than hanging for 120),
and exit codes.

## Still outstanding

- **Manual terminal verification.** Drag-select, tmux copy mode, VS Code's
  terminal, a light background, and resize while scrolled up. These need a
  human at a terminal.
- **§9's narration collapsing**, per the deviation above. The torture test's
  67 tool lines for one task is the number to judge it against.
- **Subagents**: parallel dispatch restricted to read-only types, child
  observability, per-type models, user-defined `.klaudia/agents/*.md`, and the
  adversarial-review idea. Deliberately parked.
