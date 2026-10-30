# The Agent Client Protocol frontend

```bash
klaudia --input-format acp
```

Klaudia serves [ACP](https://agentclientprotocol.com) **v1** over stdio, so an
editor drives it as a coding agent while keeping its own UI for messages, tool
calls, diffs and permission prompts. The client launches the process, speaks
JSON-RPC on stdin/stdout, and Klaudia's terminal UI never starts.

It is the third frontend over the same `agent.RunFunc` as the TUI and the
stream-json driver (see `agent.Turn`), and it exists for a reason stream-json
cannot match: an adapter for the stream-json channel is written once *per
editor*, where ACP support is written once per editor **for every agent** — and
the editors already shipped theirs.

## Setting it up

Zed — `~/.config/zed/settings.json`:

```json
{
  "agent_servers": {
    "Klaudia": {
      "type": "custom",
      "command": "klaudia",
      "args": ["--input-format", "acp"]
    }
  }
}
```

JetBrains IDEs — `~/.jetbrains/acp.json` (same shape, no `type`, and the command
wants an absolute path). Neovim: point `acp.nvim` at the same command.

Klaudia's own flags still apply, because they run before the agent starts:
`--model`, `--permission-mode`, `--resume`/`--continue`, `--allow-host-changes`.
Two combinations are refused rather than
half-honoured:

- `--output-format` with `acp`. ACP owns stdout — every byte on it is a JSON-RPC
  frame — so a renderer writing there would corrupt the stream.
- `--print` with `acp`. ACP is a persistent session, not a single shot.

Everything else is unchanged: the same tools, the same `.mcp.json` servers, the
same `AGENTS.md`/`CLAUDE.md` instructions, the same trust gate, the same
transcripts under `~/.klaudia/sessions/`.

## What is served

| Method | Notes |
|---|---|
| `initialize` | Negotiates v1 and exchanges capabilities |
| `session/new` | One conversation; refuses a `cwd` other than the process's |
| `session/load` | Replays a persisted transcript as notifications |
| `session/list` | The project's sessions, newest first |
| `session/close` | Drops the session and closes its transcript |
| `session/prompt` | Runs a turn; returns an ACP stop reason |
| `session/cancel` | Interrupts the running turn (a notification; no reply) |
| `session/set_mode` | Switches permission mode |

Outbound: `session/update` notifications for assistant text, thought chunks,
tool calls and their outcomes, plans, diffs, token usage, mode changes and the
available commands; `session/request_permission` for every ask;
`fs/read_text_file` when the client offers it.

### Modes are Klaudia's three

`availableModes` is `autonomous`, `plan` and `bypassPermissions` — the modes
`/mode` offers, with the same meanings [trust.md](trust.md) gives them, so the
editor's mode picker *is* `/mode`. The mode is **per session**: a client with
three threads open has three conversations, and one process-wide mode cannot be
right for all of them.
`ExitPlanMode` being approved mid-turn changes the mode for that session and
emits a `current_mode_update`, so the editor's picker does not go stale.

### Commands are skills

`availableCommands` lists `.klaudia/skills` entries and nothing else. The
built-in slash commands — `/undo`, `/context`, `/jobs`, `/theme` — are *frontend*
commands: they read and mutate TUI state and render into a terminal, so there is
nothing behind them to run from here, and advertising them would put entries in
the editor's palette that answer "unknown command". A skill is already ACP's
model: a name, a description, and a body that takes free-text arguments.

A prompt is matched on the command name, not on the leading slash. `"/etc/hosts
is wrong — why?"` is a legitimate prompt, and treating every slash-prefixed line
as a failed command would be worse than passing it to the model unchanged.

### Reading files goes through the editor

When the client advertises `fs.readTextFile`, `Read` asks it instead of reading
disk. That is a correctness fix rather than a nicety: the client answers from
the buffer it has open, so a file the user has edited and not saved comes back as
they see it. Reading disk while someone looks at unsaved edits is how a model
ends up explaining text that is no longer there — and, worse, basing an `Edit`
on an `old_string` the user already changed.

Any failure falls back to disk. A client that cannot serve a file is not a
reason to refuse to work.

## What is declined, and why

**`fs/write_text_file`.** Reading through the client is safe; writing is not.
`Edit` is a read-modify-write with a uniqueness check on `old_string` and a
staleness check against what was read, and every mutating path goes through the
trust gate, the permission prompt and `/undo`'s checkpoint — all of which are
expressed in terms of a path on disk. `/undo` restores a file; it cannot restore
an editor buffer. So Klaudia writes to disk and lets the editor notice, exactly
as it does for a `git checkout` from Bash or a code generator. The asymmetry is
the point: reading someone else's uncommitted view is safe, writing into it is
not.

**`terminal/*`.** Declined, not deferred. Routing `Bash` through the editor's
terminal hands over execution, and with it the sandbox, the trust gate, job
control and output clamping. A prettier widget is not worth any of those.

**`session/delete`.** Not advertised. A transcript is the user's record of what
an agent did on their machine, and an editor's "close tab" gesture should not be
able to erase it. `rm` and a file browser are right there.

**Image and audio prompt blocks.** The loop takes a string prompt, so there is
nowhere for the bytes to go, and claiming the capability would have the editor
send a screenshot that Klaudia silently discarded. `embeddedContext` *is*
claimed: a resource block carrying text is inlined into the prompt, which is the
whole point of an editor pasting the open buffer in.

**MCP servers from the client.** Klaudia connects the ones in `.mcp.json`
itself, with its own reload, liveness and elicitation handling.

**`resume` and `additionalDirectories` session capabilities.** `resume` is
`session/load` under another name. The extra directories Klaudia has — the
TUI's `/add-dir` — are informational context told to the model, not a second root the tools
work in, so accepting them here would promise an editor that a folder it added
to the workspace is one Klaudia will edit in.

## Decisions worth knowing

**v1, not v2.** v2 exists as a published Draft and changes real things —
`tool_call` folded into an upsert-only `tool_call_update`, a `state_update` for
the turn lifecycle, agent-owned terminals — but v1 is what every shipping client
negotiates today. The version is agreed at `initialize`, so v2 is additive
later: answer 2 when a client asks for 2, keep answering 1 for everyone else.

**ACP session ids are Klaudia transcript ids.** Not a second numbering with a
mapping table beside it, which is how `session/load` ends up loading something
adjacent to what the user picked. `--resume`/`--continue` reaches ACP too: the
editor's first thread *is* the session the CLI resolved, and keeps appending to
its transcript.

**One transcript per session, not per process.** ACP is the first frontend with
more than one conversation at a time. With a shared recorder, two threads
appended to the same file — interleaving two conversations into one, so
`session/load` replayed something that never happened.

**One prompt at a time, across all sessions.** A client may open several
threads, but this process has one tool registry, one job store, one executor and
one sandbox, so two concurrent turns would interleave. A prompt arriving while
another runs waits, and a cancel while it waits is still honoured.

**Every ask is `session/request_permission`.** v1 has exactly one "ask the user
to choose" primitive, and Klaudia has three asks that fit its shape: tool
permission, a host change, and the model's own `AskUserQuestion` /
`ExitPlanMode`. What is lost is wording — a client renders the request as a
permission dialog, so an ordinary multiple-choice question arrives looking like
a security prompt. The mitigations are the title, a content block carrying the
options with their descriptions (which ACP's `PermissionOption` cannot), and
`_meta` keys under `klaudia/` carrying the structured form for a client that
wants to render Klaudia's asks properly. A host change is never titled "allow
Bash?": its summary becomes the title and its scope becomes readable content.

**`TodoWrite` is a plan update, not a tool call.** Clients render plans as a
checklist, which is the entire point of the tool; left as a tool call the user
sees a raw JSON array. Statuses pass through unmapped because the tool's three
are spelled exactly as ACP's, and anything else the model invents becomes
`pending` so a malformed status cannot produce an entry a client rejects. The
plan is posted from the session's plan store when it changes, not from the
call's input, which arrives before the tool has validated anything. On
`session/load` the stored plan is posted once after the replay; only a list
that never reached the store (unapproved lists are not written to disk) falls
back to the last `TodoWrite` in the history.

**`Edit` sends its own `old_string`/`new_string` as the diff.** Reconstructing a
whole-file diff would mean reimplementing `Edit`'s replacement rules here, and a
preview that computes the change slightly differently from the tool is worse
than a narrower one — this is the text the permission prompt asks you to
approve.

**Token usage is sent once per turn.** Klaudia's `usage` event is a per-call
delta; ACP's `usage_update` means "tokens resident in the context, out of this
many". Forwarding the deltas reported a growing total against a fixed window and
showed a session at 300% of its context.

**A host-gate refusal maps to `failed`.** Klaudia distinguishes a tool the gate
*refused* from one that *failed*, because a refused `2>/dev/null` the model
routes around is a non-event. v1 has only
pending/in_progress/completed/failed — wrong in flavour, not in fact — and
`completed` would claim the tool ran. The refusal's own text goes out as the
call's content so the user can read what happened.

**Compaction and notices go out as thought chunks.** There is no ACP update for
either, and a thought chunk is the one channel a client renders as "the agent
said something that is not the answer".

**A loaded session is replayed in wire order.** Tool calls go out as the live
stream sends them — a `tool_call` where the assistant asked for it, a
`tool_call_update` when the result arrives in the next message — so a client
needs no separate code path for a loaded session. The replay logs how many
messages produced an update: a transcript that replays to nothing is the
signature of a format change, and otherwise indistinguishable from an empty
session.

## Limits

- No authentication methods are advertised. Klaudia authenticates from its own
  config and environment, so there is nothing for the client to log into —
  `authenticate` answers null rather than "method not found", because a client
  that calls it unconditionally should still work.
- `session/new` refuses a `cwd` other than the one the process was started in.
  The tool registry, system prompt and sandbox are all bound to it; serving a
  second project would mean a second process, which is what the editor's
  per-worktree agent already is.
- Stop reasons are mapped down to v1's five. Klaudia's vocabulary is wider
  (`user_halt`, `blocked_by_hook`, `model_context_window_exceeded`), and the
  detail survives in the message stream rather than in the stop reason.
