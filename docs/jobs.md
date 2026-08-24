# Managed jobs, logs, and shell behaviour

A long-running command is a job, not a blocked turn. This covers what that
means, how logs work, and — importantly — what Klaudia's shell does *not* do.

## Jobs

`npm run dev` has no natural end. Run in the foreground it holds the agent until
the timeout; started and forgotten it becomes an untracked process owning port
3000 for the rest of the afternoon.

Klaudia starts such commands as **jobs** (`Bash` with `run_in_background`). Each
gets:

- **An id and a name.** `bash_1` is stable and exact; `dev` is what you say out
  loud. Both work everywhere. Names are derived from the command — `npm run dev`
  is `dev`, `make dev-api` is `dev-api` — not mapped from a table that guesses
  at your vocabulary. A collision gets `-2`.
- **A log file** at `~/.klaudia/jobs/<session>/<name>.log`, pruned after a week.
- **Exit detection.** A crash is reported when it happens, not when someone next
  looks.
- **A port**, when the job announces one recognisably.
- **A location** — `local`, or the host a job started over ssh runs on.

```
/jobs                   what's running, on what port, where
/logs <job>             page the log in $PAGER
/logs -f <job>          tail it into scrollback
/logs --errors <job>    pull just the failure lines into the conversation
/restart <job>          stop and start again in place
/stopjob <job|all>      stop it and its whole process group
```

With exactly one job running, the name is optional.

The model has the same reach through `Jobs`, `BashOutput`, `RestartJob` and
`KillShell`.

### Restart, not kill-and-start

`RestartJob` stops the process group, waits for the port to be released, and
runs the same command in the same slot — same id, same name, same log, with a
marker where the restart happened. Starting a command that is already running
returns the existing job instead of a second copy: two dev servers fighting over
one port is a confusing failure, and the loser usually looks like broken code.

### Logs use your pager, not ours

There is no in-app log viewer, deliberately. Paging, incremental search,
jump-to-end and selection-copy are things `less` already does, configured the
way you like. More to the point, "incoming logs snap the viewport back down" is
only ever a problem *because* an application owns a viewport — so `/logs -f`
prints into your terminal's real scrollback, where scrolling up cannot be
undone by new output arriving.

`/logs <job>` hands `less` the actual file, so `F` follows it from there.

## Process groups

Every command runs in its own process group, and stopping one signals the whole
group (SIGTERM, then SIGKILL after two seconds).

This matters more than it sounds. Before, cancelling `sh -c "npm run dev"`
reaped the shell and left node holding the port — so stopping a job did not stop
it, Esc during a command did not end it, and the next start became a second
copy.

A consequence: Ctrl+C at your terminal no longer reaches Klaudia's children by
accident. That is the point — a stray signal should not kill a managed job — and
Klaudia handles SIGINT itself, tearing jobs down properly on the way out.

## Shell behaviour

Commands inherit your environment: `PATH`, `SSH_AUTH_SOCK`, git credential
helpers, language version managers, proxy settings. What Klaudia adds is the
small set of hints that tell a program nobody is watching:

| Variable | Value | Why |
|---|---|---|
| `GIT_TERMINAL_PROMPT` | `0` | fail fast instead of blocking on a TTY that does not exist |
| `GIT_PAGER`, `PAGER` | `cat` | a pager in a captured command hangs it |
| `COLUMNS`, `LINES` | your terminal's | output wraps to the width you are looking at |

Your own `$PAGER` is untouched where it matters — Klaudia's long-output view and
`/logs` run it with a real terminal.

### No PTY

Klaudia does not allocate a pseudo-terminal, so programs that require one do not
run. `vim`, `less`, `top`, `git rebase -i`, `git commit` with no `-m`, `ssh` with
no command, `docker run -it` are refused immediately, with the non-interactive
alternative named.

This is a choice, not an omission. A PTY would make `vim` "work" in a surface
the model cannot drive and you cannot see: the turn would look successful while
sitting in an editor forever. Failing in one second with "pass the message with
-m" is more useful than hanging for two minutes.

The cost is real and worth stating: **terminal resize does not propagate to
child processes.** `COLUMNS`/`LINES` covers programs that consult it; anything
that queries the terminal directly sees nothing.

## Steering

Type while Klaudia works and it reads your message before its next step, not
after the turn. "Don't modify the API" applies to what happens next.

- **Enter** queues the correction; it lands at Klaudia's next step.
- **Enter again** (empty input) interrupts immediately and sends it.
- **↑** pulls it back to edit.
- **`/stop`** asks Klaudia to finish the current step and report. "Stop after
  this test run" does not throw away the test run.
- **Esc** interrupts now. Completed work stands.

## Running commands yourself

A leading `!` runs a command directly:

```
> work out why the auth test is failing
$ git diff
> keep the test change but revert the API change
```

The marker switches from `›` to `$` as soon as the line starts with `!`, so what
Enter will do is visible before you press it. Output goes into the conversation
as well as the screen, which is why the third line above works.

`!` commands are yours: the host boundary does not apply, because you typed
them. Klaudia still says what a command touches before running it, so a line
pasted from a README announces itself.

## Exit codes

Headless runs exit with a code an automation can branch on:

| Code | Meaning |
|---|---|
| 0 | completed |
| 1 | failed — the reason is in the result payload |
| 2 | invoked wrongly (bad flag, invalid mode); nothing ran |
| 3 | hit `--max-turns` with work outstanding |
| 4 | needed a host change and had no way to ask — see `--allow-host-changes` |
| 130 | interrupted (SIGINT/SIGTERM) |

4 is the one worth wiring up: it distinguishes "the task needed a package
installed and nobody said it could" from "the model got it wrong", and those
want completely different responses.
