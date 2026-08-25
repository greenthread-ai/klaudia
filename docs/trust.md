# Autonomy and the host boundary

Klaudia finishes tasks without asking permission for each step, and stops before
changing the machine it is running on. This document explains what that means
exactly, what enforces it, and — importantly — what does not.

## What it is not

**This is a guardrail against well-intentioned mistakes, not a security
boundary.**

The mechanism reads command lines and tool inputs. It does not observe what
programs actually do. A command that computes its own target, a script Klaudia
wrote a moment ago and then ran, a package's postinstall hook, a Makefile
target — all of them do whatever they do without this noticing. A model that
wanted to evade it could.

That is accepted by design, not an oversight. The thing it is good at is the
thing that actually happens: an agent reaching for `sudo systemctl restart
nginx` because the task seemed to need it. It stops that, explains it, and asks.

If you want enforcement the kernel applies, set:

```toml
[sandbox]
mode = "os"   # sandbox-exec on macOS, bubblewrap on Linux
```

That is real confinement — writes outside the allowed roots are refused by the
operating system, including through `sh -c` and under `sudo`. It is off by
default because it also breaks ordinary tooling: `go build` cannot write its
module cache, `~/.cache` is denied, and making a language toolchain work again
means curating an allowlist that rots. See `internal/sandbox`.

## Zones

Every tool call is classified into a zone. The zone decides whether Klaudia acts
or asks.

| Zone | What it is | Klaudia |
|---|---|---|
| **task** | Todos, memory, skills — Klaudia's own bookkeeping | acts |
| **project** | The project and its build caches: read, edit, build, test, git, dev servers | acts |
| **network** | Fetching things | acts |
| **remote** | Another machine or a container the task calls for | acts |
| **host** | This machine's operating system | **asks** |
| **sensitive** | Local credential material | **asks** |

Some consequences worth being explicit about:

- **Destructive project work is autonomous.** `rm -rf ./dist` and `git reset
  --hard` are ordinary. Prompting for them would defeat the point.
- **`~/.cache`, `~/go/pkg/mod`, `~/.npm`, `~/.cargo`, `~/.m2` and friends are
  project zone.** They are in `$HOME` but they are build state, and gating them
  would prompt on every build.
- **The rest of `$HOME` is neither.** Your documents are your data, not the
  operating system. This protects the machine, not the home directory. A
  deliberate decision.
- **But `~/.zshrc`, `~/.gitconfig` and `~/Library/LaunchAgents` are host.** They
  configure the machine or your login session and persist into every future
  shell.
- **A project inside a system prefix is still the project.** `/opt/app` and
  `/usr/local/src/foo` are ordinary work when they are your project root.
- **Remote work is governed by the task.** `ssh staging sudo systemctl restart
  nginx` is the job you asked for. The same line without the `ssh` is a change
  to the machine you are typing on. Credentials are the exception that does not
  travel: `ssh host cat ~/.ssh/id_rsa` pulls a secret into this transcript
  wherever the file lives.
- **Using a credential is not disclosing one.** `ssh -i ~/.ssh/deploy_key host`
  and `curl --cert client.pem` are ordinary. `cat ~/.ssh/id_rsa` and
  `Read(~/.aws/credentials)` put the secret into the model's context.
- **Scratch space and pseudo-devices are free.** `/tmp`, `/var/tmp`, `/dev/null`,
  `/dev/stdout`, `/dev/tty` and friends change nothing, so `2>/dev/null` and
  `> /tmp/out` never ask. Block devices are the exception — `dd of=/dev/disk2`
  destroys a disk and asks.
- **`sudo` is not itself the trigger.** `sudo -u deploy ./scripts/deploy.sh`
  inside the project is project work. Treating every `sudo` as a host change is
  how a protection gets switched off.

## One approval per operation

When Klaudia needs to change this machine it calls `RequestHostChange` first,
describing the whole operation:

```
Install nginx and configure it as a development proxy
why: the task asks for the app to run behind a local proxy
  paths: /etc/nginx/nginx.conf
  services: nginx
  packages: nginx
```

You approve the operation, not the commands. The package install, the config
directory, the write, the validate and the restart then all proceed without
further interruption, because they are inside the scope you agreed to.

Approving one file inside a directory covers that directory — approving
`/etc/nginx/nginx.conf` covers `/etc/nginx`, so writing `conf.d/proxy.conf`
does not ask again. The widening stops well short of anything dangerous:
approving `/etc/hosts` does not hand over `/etc`, and a request for a whole
system directory is refused before it reaches you.

Approvals are **session-scoped and never written to disk**. There is no
always-allow: a standing permission to reconfigure your machine is one you
cannot see and did not schedule the end of. `/trust revoke <id>` withdraws one
immediately; `/trust revoke all` withdraws everything.

Kinds are not interchangeable. Approving an install does not approve a removal.
Approving writes in a directory does not approve deleting the directory. A
recursive delete aimed at `/`, `$HOME` or a project root always asks, whatever
is granted.

### Scope drift

Anything outside the approved scope stops and asks again, and the card says so:
*"This wasn't part of what you approved."* Declining fails that one tool call —
work already done stands, and Klaudia carries on with the rest of the task and
tells you what it could not do.

## Modes

Three, not six:

| Mode | Meaning |
|---|---|
| `autonomous` | Finish the task; ask before changing this machine. **Default.** |
| `plan` | Read-only exploration; mutations blocked. |
| `bypassPermissions` | No checks at all, including the host gate. |

`autonomous` requires the host guardrail to be enforcing. Without it, autonomous
is `bypassPermissions` with a friendlier name, so Klaudia refuses the
combination rather than offering it.

The old modes (`default`, `acceptEdits`, `dontAsk`) still work so existing
configs keep running; they are no longer offered as a choice.

## `/trust`

Shows the guardrail's state, the approvals live in this session and what they
reach, what the classifier has found, and any allow/deny rules carried over from
the per-command model.

```
/trust                  show everything
/trust upgrade          switch from observing to enforcing
/trust observe          classify and report, change no decisions
/trust off              disable classification for this session
/trust revoke <id>      withdraw one approval
/trust revoke all       withdraw all of them
```

## Migration

A config that already has `[permissions]` allow/deny rules starts in **observe**:
the classifier runs and `/trust` shows what it found, but nothing is refused and
your existing per-action prompts continue. You get a one-time notice at startup.
`/trust upgrade` switches over.

Everything else starts enforcing.

Existing allow/deny rules keep working, and Klaudia no longer creates new ones —
approving an operation replaced "allow always". Your rules are listed in
`/trust`.

Configure it explicitly with:

```toml
[trust]
mode = "enforce"   # enforce | observe | off
```

## Unattended runs

Headless runs have no one to ask. Ordinary project and remote work proceeds;
a host change is refused with a message naming the flag that would permit it:

```
klaudia -p "provision the box" --allow-host-changes
```

`--allow-host-changes` is for a machine you are willing to have reconfigured —
a disposable CI image, a provisioning run. It permits the *declared operation*,
not everything: a run that asked to install nginx has not asked to reboot.

`--loop` no longer needs `--dangerously-skip-permissions`; use
`--permission-mode autonomous`.

## Where it lives

- `internal/trust` — zones, classification, grants. A pure package with no
  wiring, judged by a corpus of command lines in `classify_test.go`.
- `internal/agent/hostgate.go` — the gate, which runs in `dispatch` **before**
  `permission.Check`. That ordering matters: an allow rule says a command prefix
  is fine, not that anything sharing that prefix may change the machine.
- `internal/tools/hostchange.go` — the `RequestHostChange` tool.
- `internal/tui/trustview.go` — `/trust`.
- `internal/sandbox` — the real boundary, off by default.
