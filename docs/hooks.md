# Lifecycle hooks

A hook is a shell command Klaudia runs at a fixed point in a turn. It is the
escape hatch for everything the agent should not have an opinion about: run the
project's formatter after a write, refuse edits to generated files, paste the
current ticket into every prompt, notify you when a long run finishes.

```toml
# .klaudia/config.toml — or ~/.klaudia/config.toml

[[hooks]]
event = "PostToolUse"
matcher = "Edit|Write"
command = "gofmt -w $(jq -r '.tool_input.file_path') 2>/dev/null"

[[hooks]]
event = "PreToolUse"
matcher = "Edit|Write"
command = '''
case "$(jq -r .tool_input.file_path)" in
  *.pb.go|*_generated.go) echo "generated file — edit the source" >&2; exit 2 ;;
esac
'''

[[hooks]]
event = "UserPromptSubmit"
command = "echo \"Branch: $(git branch --show-current)\""
timeout = "5s"
```

## What a hook is not

**A hook is not a security boundary, and must not be used as one.** The host
gate (see [trust.md](trust.md)) decides whether a tool call may change this
machine; `internal/sandbox` is the only kernel-enforced answer.

A `PreToolUse` hook runs *after* the gate and the permission check have both
allowed the call. So a hook can narrow what Klaudia will do and can never widen
it. That ordering is the entire reason hooks are allowed to block at all: a
config file that could *grant* permission would be a way to switch the host gate
off by writing a file.

## The four events

Deliberately few. Claude Code has grown past thirty lifecycle events; each one
is a promise about when the loop will call out, which is a promise about the
loop's shape. These four cannot be expressed any other way.

| Event | When | Can add context | Can block |
|---|---|---|---|
| `SessionStart` | once, before the first request | yes | no |
| `UserPromptSubmit` | a prompt is about to be sent | yes | yes |
| `PreToolUse` | a tool call is about to run | no | yes |
| `PostToolUse` | a tool call finished | yes (as feedback on the result) | fails the call |

`SessionStart` cannot block because there is nothing to block — the session is
already starting, you asked for it, and the only honest response to "your
`SessionStart` hook said no" is to say so and carry on.

`Stop`/`SubagentStop` — "the model thinks it is done, make it keep going" — are
absent on purpose. A hook that can deny completion can hang a session, and
nothing in a shell command knows better than the loop whether the work is done.

`SessionStart` fires once per *session*, not once per turn, and not again when a
sub-agent starts. Sub-agents run the two tool events and skip
`UserPromptSubmit`: their prompt is the parent's instruction, not yours.

## Fields

| Field | Meaning |
|---|---|
| `event` | one of the four above. A typo is reported, not silently ignored. |
| `matcher` | regexp matched against the tool name. Only meaningful for the two tool events; omit to match every tool. |
| `command` | run through the same shell as the Bash tool (`bash -c`, or `sh -c` where bash is absent), in the project directory. |
| `timeout` | Go duration (`"5s"`, `"2m"`). Defaults to 30s. |

Hooks run **one at a time, in declaration order**, yours before the project's.
They are allowed to have side effects on the working tree — formatting a file is
the motivating case — and two of them writing the same file at once is a
corruption you cannot debug from inside Klaudia. The first block stops the
sequence.

The 30-second default is half Claude Code's minute. A `PreToolUse` hook is in
the critical path of *every* tool call, so a hook that hangs is a session that
looks frozen, and the formatter and linter cases finish in well under a second.
A hook that genuinely needs longer can say so; the default protects the person
who did not think about it.

## What a hook is told

A JSON object on stdin. The field names are Claude Code's, because hook scripts
are the most portable artefact in the ecosystem — a few lines of shell reading
`jq -r .tool_input.file_path` — and a gratuitously different payload would mean
editing every existing script for no benefit. Fields Klaudia does not produce
are absent.

```json
{
  "hook_event_name": "PostToolUse",
  "session_id": "…",
  "cwd": "/path/to/project",
  "tool_name": "Edit",
  "tool_input": { "file_path": "…", "old_string": "…" },
  "tool_response": "the tool's output",
  "tool_error": false,
  "prompt": "the text about to be sent (UserPromptSubmit)"
}
```

`tool_input` is the arguments the model produced, verbatim. Two environment
variables are also set: `KLAUDIA_PROJECT_DIR` and `KLAUDIA_HOOK_EVENT`.

## What a hook says back

**Exit status** covers almost everything:

- **0** — fine. Anything on stdout becomes context (see below).
- **2** — block it. stderr is the reason, and it is shown to the model.
- **anything else** — the hook malfunctioned. Reported to *you*; the model is
  never told.

That last distinction is the point of using 2 specifically. A hook that exits
127 because the formatter is not installed has not made a judgement about the
tool call, and reporting it to the model as a refusal would have it reason
about — and route around — a broken environment. A timeout is a malfunction, not
a refusal, for the same reason.

**stdout** is injected into the conversation: `SessionStart` and
`UserPromptSubmit` output is prepended to the prompt in a `<hook-context>`
block; `PostToolUse` output is appended to the tool's result, labelled, so the
model can see it is feedback rather than the tool's own words. It is capped at
8 KB, because hook stdout is prompt: `command = "git log"` in a long-lived repo
is megabytes in front of every message. Truncation is reported to you.

A hook may instead print JSON to be explicit:

```json
{ "decision": "block", "reason": "generated file", "additionalContext": "…" }
```

Claude Code's nested `hookSpecificOutput` shape is also accepted. Neither is
required: a hook that prints a shopping list is treated as having printed a
shopping list.

## Hooks that arrive with a repository

Cloning a repo and starting Klaudia in it would otherwise be enough to run
whatever its `.klaudia/config.toml` says, before you have read a line of it.
That is the attack shape that made editors stop sourcing project-local config,
and the reason `.vscode` tasks and `direnv` both grew an approval step.

So:

- Hooks in `~/.klaudia/config.toml` are **yours** and run unconditionally.
  Asking you to confirm your own settings file is the kind of prompt that
  teaches people to stop reading prompts.
- Hooks in the project's `.klaudia/config.toml` are **confirmed once**. You are
  shown every command and the file it came from, and the answer is recorded in
  `~/.klaudia/hooks.json`, keyed by project directory.
- The unit of approval is the whole **set**, identified by a fingerprint over
  each hook's event, matcher, command and timeout, in order. Editing the set
  comes back for confirmation — the dangerous case is not the repo that asks on
  day one, it is the repo that asks for something harmless and changes it in a
  later pull. Reverting to a previously-approved set re-asks too, since that is
  precisely the move available to an attacker who was approved once.
- Nothing inspects what a command *is*. An approved hook runs unconfined with
  your privileges, which is what makes the feature useful and what makes the
  confirmation load-bearing. The prompt is the control; there is no second line
  of defence behind it.

In a headless run there is nobody to ask, so project hooks do not run and a
notice says so — unless the run passed `--allow-host-changes`, which approves
the card as it would any other host change, and is remembered the same way.
"Nobody objected" is not agreement.

### Project MCP servers

A server in a project's `.mcp.json` (or `.klaudia/.mcp.json`) is the same
thing as a project hook — a command that arrived with the clone — and used to
launch the moment Klaudia started. It now takes the same card, with the same
rules: servers in `~/.klaudia/.mcp.json` are yours and start at once; a
project's are held until you approve the set, which is asked at your first
message (the first point every frontend has someone to ask) and started before
that turn's request, so the turn can use them. The approval is recorded in
`~/.klaudia/mcp-approvals.json` — separate from `hooks.json`, since agreeing to
a repository's hooks says nothing about its servers — and the fingerprint
covers each server's name, command, args, env, URL and headers, so an edit
asks again; a project entry that redefines a global server is the project's.
The card lists each command or URL and the *names* of its env variables, not
their values. HTTP servers are held too: nothing is launched, but the model's
tool calls, with whatever it has read, go to a URL the repository chose. A
no lasts the session; a headless run behaves as for hooks. `/mcp` lists held
servers with their commands and offers an approve row — the same consent as
the card, given sooner, and the way to change your mind after a no.

Hooks are not sandboxed, whatever `[sandbox]` says. That setting confines the
*model's* commands; a hook is your own automation, and the things it exists to
do — run a formatter, touch a git index, post a notification — are exactly what
confinement forbids.

## Seeing what is configured

`/doctor` lists every hook by event and matcher in the order they will run,
counts them by scope, and warns when a project set is configured but still
waiting on your approval — the state whose only symptom is otherwise silence.
Config problems (an unknown event, a bad regexp, a matcher on an event that has
no tool to match) are reported as notices on the first turn; the bad entry is
dropped and the rest still run.

Hook notices go to you and never to the model. A misbehaving hook is the
operator's problem to fix, and telling the model about it only invites it to
work around a broken script.

Implementation: `internal/hooks` (events, trust, execution) and
`pkg/agent/hooks.go` (the four call sites in the loop).
