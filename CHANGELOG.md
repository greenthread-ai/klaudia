# Changelog

Notable changes to Klaudia. The version tracks the Claude Code reference the Go
port mirrors (see `internal/version`).

## Unreleased

### Fixed
- **Interrupt-to-send during a command could strand the message.** Queue a
  message, press Enter again to interrupt and send it now, and — if a tool was
  running — the message sometimes vanished: the turn cancelled but no new turn
  started, just an idle prompt. Two things drain the queue, on different
  goroutines: the agent's mid-turn steering poll and the UI's interrupt path.
  Cancelling an in-flight command completes its tool batch, which drives the
  agent straight into its post-batch poll — draining the message into the turn
  being killed, where it sat in history unanswered. Confirmed from the real
  transcript, which ended with the queued message as a trailing user turn with
  no reply. The interrupt now takes the message out of the queue before
  cancelling, so a late poll finds nothing, and the message always becomes the
  next turn.
- **Interrupting no longer loses track of running jobs.** Cancelling a turn
  kills the foreground command but leaves managed background jobs running (a dev
  server keeps its port). The resend turn now tells the model what it left up so
  it can decide whether each still matters for the new instruction — inspect,
  stop, or leave running. It does not kill them automatically; that is the
  model's call.

### Changed
- **Auto-resume skips a session that ended in a model refusal.** A refusal is
  tripped by the accumulated context, not the last message, so reviving that
  conversation makes the very next prompt — even an unrelated one — refuse too.
  Klaudia now starts fresh and says so ("Last session ended in a model refusal —
  starting fresh"), leaving the transcript on disk for an explicit --resume. Only
  the implicit pick-the-most-recent case; naming a session by --resume/--continue
  is honoured. Detected by the refusal's signature — an assistant turn with no
  content, or an older transcript's stop_reason — so it needs no new stored
  field.

### Fixed
- **A web search in the history could 400 the whole turn** with
  `web_search_tool_result.content ...: Input should be a valid array`. The
  transcript recorder marshalled the raw response message, and the SDK's
  response union types have no marshaller — so a server-tool result's content
  serialised as an object full of leaked field names
  (`OfBetaWebSearchResultBlockArray`) instead of an array. On resume that parsed
  back into an empty error block and the API rejected it. Two fixes: the
  recorder now stores the param form, which round-trips cleanly (RawJSON is not
  an option — after streaming the SDK sets it to the same leaky marshal); and
  sanitize drops an already-corrupted result and its server_tool_use before
  send, with a visible placeholder, so a transcript written before this fix
  resumes instead of failing. Verified against the real on-disk transcript that
  hit it.

- **A refused or truncated turn showed nothing but "✓ done".** Reported from a
  real session: three messages in a row got a silent completion, no answer above
  them, indistinguishable from a bug. Three stop reasons cause it — the model
  refused (`refusal`), ran out of output budget (`max_tokens`), or overflowed the
  context window — and all three rendered as dead air. Klaudia now names what
  happened and how to get out of it.

  Refusal is the one that compounds: the model's safety system can be tripped by
  earlier context rather than the latest message, so once a conversation has
  drifted into refused territory even "hi" keeps refusing — and an auto-resumed
  session brings the tainted history back with it. The note points at `/clear`
  (or `--new-session`), which is the actual way out. Headless runs no longer
  report a refusal as `success` with empty stdout: the payload carries the
  explanation and the exit code is non-zero, so a pipeline can branch on it.


### Added
- **The read-only sub-agents can reach read-only MCP tools.** `Explore` and
  `Plan` were limited to `Read`, `Glob` and `Grep`, so the two agents whose whole
  purpose is read-only fan-out research could not touch a wiki, an issue tracker
  or a chat archive. Naming MCP tools in their whitelist was never an option:
  they are discovered at connect time and differ per project.

  This is also where the context argument points. Searching four sources to
  answer one question is exactly the work that should not run in the main
  thread — a sub-agent spends its own window and hands back a summary.

  Eligibility is read from what a tool declares, not from what its agent is
  asked to do. A tool qualifies by setting the protocol's `readOnlyHint`, and a
  tool that says nothing is treated as a write, because the annotation is
  optional and the safe reading of silence is that nobody considered it. That
  matters: `mcp__gitea__delete_branch` is one connected server away from an
  agent whose only previous guarantee was a system prompt asking it not to
  write, and there is a test that fails if it ever arrives.

  For a server that annotates nothing — including one launched in its own
  read-only mode, where the operator knows something the protocol was not told —
  `"readOnly": true` on the server in `.mcp.json` says so. `"readOnly": false`
  says the opposite: `readOnlyHint` is a claim a server makes about itself and
  nothing verifies it, so an operator who does not believe a third-party server
  can decline to take its word without giving up the server, which the main
  agent keeps and still asks about before every call. Unset trusts the
  annotations. Measured against the case that prompted this: gitea-mcp annotates
  all 54 of its tools, 33 of them read-only, matching exactly what its own `-r`
  flag exposes.
- **Klaudia tells your working-tree changes apart from its own.** `/changes`
  splits them three ways: yours, Klaudia's, and *both* — a file it wrote that
  was already dirty or that you edited afterwards. Klaudia cannot merge those
  two changes, but it can refuse to pretend they are not there, which is what
  keeps undo and `/commit` honest. Startup says once how many files were already
  modified, for the case where you had forgotten.
- **`/undo`, and it cannot destroy work you did.** Before a turn writes a file,
  its contents are stored as a git blob with `git hash-object -w` — a plain
  object write that leaves your index, HEAD and `git status` untouched. A stash
  would have been simpler and is wrong: it moves the whole working tree
  including your unrelated edits. Undo shows the plan first, including the
  equivalent `git cat-file -p <sha> > path` for each file, because "undo 2
  files?" is a promise rather than an inspection. Files you also touched are
  skipped and named.
- **The completion block says what was *not* verified.** "auth tests 83/83
  passing" reads as proof until you notice the full suite was never run. A
  targeted run now names its subset and reports the gap, and files changed with
  nothing run at all says so outright.
- **`/context`, `/pin`, `/unpin`, `/forget`.** What Klaudia has read, changed
  and been working in, instead of a token percentage — with a closing line
  saying that list is what it looked at, not what the task needed. A pinned file
  is re-stated every turn, which is how it survives compaction; a file mentioned
  once forty turns ago is not really in context at all.
- **Resume reconciles the work, not just the chat.** The working tree is
  re-read, ownership is recovered from the transcript's own Write/Edit calls,
  and jobs are reported as stopped — they were children of a process that has
  exited, and the conversation you are resuming implies they are still up.
  Approvals are deliberately not restored: session-scoped means session-scoped,
  and resurrecting them would be the flaky remembered permissions the trust
  model replaced.
- **Meaningful exit codes** for headless runs: 0 done, 1 failed, 2 invoked
  wrongly, 3 hit `--max-turns`, 4 needed a host change with no way to ask, 130
  interrupted. 4 is the one worth wiring up — it distinguishes "the task needed
  a package installed" from "the model got it wrong".

### Changed
- **`/commit` stages only Klaudia-owned files.** A file it wrote that you also
  edited is left out and listed: the two changes cannot be separated without
  hunk-level surgery, and sweeping your edit into a commit describing Klaudia's
  work is the bug `/commit` already stopped doing once.

### Fixed
- **Resizing the terminal left a stack of orphaned prompt boxes.** Reported from
  a live drag-resize. Bubble Tea's inline renderer returns to the top of the live
  region with `CursorUp(linesRendered-1)`, where that count is the *logical* line
  count of the previous frame. On a resize it updates its width and calls repaint
  but does not reset the count — and the terminal has meanwhile reflowed the rows
  already on screen. A four-line region drawn at 120 columns occupies seven rows
  at 90, so the renderer moves up three when it needed six; the cursor lands
  inside the old frame, `EraseScreenBelow` clears from there, and the rows above
  survive. Every intermediate size in a drag deposits another band.

  The deficit is arithmetic, not a mystery: we rendered the previous frame, so we
  know each line's visual width, and the terminal wraps a line of width w into
  ceil(w/newWidth) rows. The next frame is now prefixed with exactly that many
  `CursorUp` plus an `EraseScreenBelow`, which puts the renderer back on the true
  top of the old frame. The escapes ride inside the frame rather than going
  straight to stdout, because the renderer owns that stream — `ansi.Truncate`
  keeps CSI sequences at zero width, which was verified rather than assumed.

  Separately, three of the four live-region lines were *exactly* the terminal
  width — measured. The renderer only appends `EraseLineRight` to lines narrower
  than the terminal, so a full-width line never erased what was to its right, and
  writing the last column parks the cursor in the pending-wrap state. The live
  region now stops one column short at every width, with a test asserting it.
- **A question could be shown with its escapes intact.** Seen in a real
  session: `On \"ask whether they want to change…\" \u2014 which did you mean?`.
  The transcript shows the model escaped its JSON twice, so one round of
  decoding — which is all that is correct — leaves backslashes as literal
  characters. Nothing was decoding it wrongly; it genuinely contained them. Short
  human-facing strings (a question, its option labels, a host-change summary) are
  now repaired on the way out, and only when the result parses cleanly as a
  quoted literal. Plans, diffs and anything multi-line or long are deliberately
  untouched: they legitimately contain backslashes, and rewriting real content
  would be worse than the bug.
- **A stale prompt box could be stranded in the middle of the conversation.**
  Seen mid-turn: box borders, "› Ask Klaudia…" and the status line sitting in
  scrollback with later tool output written across them. When `tea.Println`
  flushes new output, the renderer returns to the top of the live region with
  `CursorUp(linesRendered-1)` — a *logical* line count. Any live-region line
  that fills the terminal width is two physical rows, so the cursor lands inside
  the region and everything above it is stranded for good.

  The earlier fix stopped the input box and status line reaching the last
  column, but its test built an *idle* model — so it never rendered the two
  components that were still doing it: the streaming preview (truncated to
  exactly the width, off by one) and the approval prompts (not truncated at all;
  326 columns for a long path). The clamp now lives at one choke point covering
  the whole live region, and the test drives every state that renders something.
- **Scrollback lines longer than the terminal left residue on their last row.**
  Spotted in a screenshot of a real session: a 160-character prompt echoed at 149
  columns wrapped, and the short second row still showed "0k tokens" from the
  status line that had been there before. Bubble Tea appends EraseLineRight to a
  queued line only when it is *narrower* than the terminal, so an over-wide line
  gets none and the terminal's own wrap leaves a partial final row. Klaudia now
  wraps its own output to one column short of the terminal, making every physical
  row a line the renderer will clean up. The transcript keeps the unwrapped text,
  so /copy and /export are unchanged.
- **The status bar showed "ask" while the session was autonomous.** shortMode had
  no case for the mode added in the six-to-three collapse, so it fell through to
  the default. Of everything on that line, the mode is the one field that must
  not be wrong — it was telling the user Klaudia would stop and check while it
  was working straight through. There is now a test that every mode has its own
  label and no two share one.
- **The host gate told the model off and never told the user.** Reported from a
  live session: a blocked call printed a paragraph of policy as a red `✗`
  failure, the model quietly took another route, and the user — who might well
  have said yes — was never asked. The message instructed the model to call
  `RequestHostChange`, and the model, being a volunteer, generally didn't.

  The first instinct was to make the gate ask every time. That was wrong, and
  the sessions that prompted it prove why: both blocks were an incidental
  `2>/dev/null` and a scratch file in `/tmp`, where routing around is not a
  workaround but the correct answer. Asking would have been pure interruption,
  which is the prompt fatigue the design already avoids on purpose.

  So the split is by whether Klaudia can proceed. A block it can route around is
  a non-event: the refusal to the model is now three short sentences that prefer
  another route and mention the declaration tool second, and it draws as a muted
  `⊘ changes this machine: writes /dev/null — trying another way` rather than as
  a failure. A block it cannot route around still reaches the user, and the
  prompt grew a third answer — **(s)omething else** — because declining a host
  change usually means "not like that" rather than "give up"; it keeps the turn
  alive and the redirect lands before Klaudia's next action.

  The residual risk is the quiet one: giving up without saying so. A host change
  stopped and never approved is now named in the completion block under `Not
  done — needs your agreement`, judged at end of turn so the good path — gate
  stops it, model declares it properly, user agrees — is not misreported as
  undone.
- **`2>/dev/null` was gated as a host change.** Reported from a live session.
  Writing to a pseudo-device changes nothing about the machine, and discarding
  output is one of the commonest things a command does — a false prompt on it
  costs more than the guardrail is worth. `/dev/null`, `/dev/stdout`,
  `/dev/stderr`, `/dev/tty`, `/dev/fd/*`, `/dev/pts/*` and the random/zero
  sources are all ordinary now. Block devices deliberately are not: `dd
  of=/dev/disk2` still asks, which is the reason `/dev` is watched at all.
- **`/tmp` was a host change on macOS and not on Linux.** Found while fixing the
  above. macOS resolves `/tmp` to `/private/tmp`, and the resolved form was being
  added to the host prefixes — so writing a scratch file asked for permission on
  one machine and not the other. `/tmp` is scratch space everywhere now.
- **A dev server backgrounded with `&` bypassed the job system entirely.** Found
  by running the spec's agent-loop torture test: the model shell-backgrounded
  the server eleven times and then managed the processes by hand with `pkill`
  and `kill -9 $(lsof -ti:PORT)` — no name, no managed log, no crash detection,
  no restart. A model that already knows `&` will use `&`, and the timeout nudge
  cannot help because a self-backgrounded command returns immediately. Bash now
  refuses to detach something long-running and names `run_in_background`; the
  refusal fires only when the command both backgrounds *and* looks long-running,
  so `sleep 1 &` is nobody's business.
- **Ownership was recorded before the write, not after.** The stamp used to be
  taken from the `tool_use` event, which fires before the tool runs — so every
  file Klaudia edited looked like it had changed underneath, i.e. like *you* had
  edited it, and undo would have refused to restore its own work. A failed Write
  no longer claims a file it never wrote either.
- **Session teardown abandoned processes that were slow to stop.** KillAll sent
  SIGTERM and returned; the binary then exited, taking with it the goroutine
  that would have escalated to SIGKILL two seconds later. A server that ignores
  SIGTERM therefore kept its port forever — the exact symptom process groups
  were introduced to fix. Teardown now waits for each job to actually go. Found
  by the smoke test, which was itself checking too early.
- **A declared host change that was refused left no trace.** The guardrail only
  logged changes it *caught*, so a model that did the right thing — declared its
  intent up front and was told no — was invisible to /trust and to the exit
  code. It is recorded now, which is what makes exit 4 reliable.
- **The undo snapshot could race the write it was meant to precede.** A frontend
  learns about a tool from an event on a channel; a snapshot taken when that
  event arrives can happen after the write and capture the new contents as the
  "before". It now runs through a synchronous hook immediately before execution.

### Added
- **Long-running commands become managed jobs.** `npm run dev` used to either
  hold the agent until the timeout or vanish into an untracked process owning
  port 3000 for the afternoon. A job now has an id and a name you can say out
  loud (`npm run dev` → `dev`, derived rather than mapped from a table guessing
  at your vocabulary), a log file under `~/.klaudia/jobs/`, a port when it
  announces one, and a location — `local`, or the host it started on over ssh.
  `/jobs`, `/logs`, `/restart`, `/stopjob`, and the same reach for the model
  through `Jobs`, `BashOutput`, `RestartJob` and `KillShell`.

  **A crash is reported when it happens.** Nothing used to notice a job dying
  until something read it, so "why is the site down" got the answer "it's
  running fine".

  **Restart replaces the process in place** — same id, same name, same log, with
  a marker where the restart happened — and starting an already-running command
  hands back the existing job. Two dev servers fighting over one port is a
  confusing failure, and the loser looks like broken code.
- **`/logs` uses your pager, and follow mode cannot fight you.** `/logs <job>`
  hands `less` the real file (so `F` follows from there); `/logs -f` prints into
  the terminal's own scrollback, where scrolling up cannot be undone by new
  output arriving — there is no viewport to snap. `/logs --errors` pulls just
  the failure lines, stack traces intact, into the conversation, so a crash can
  reach the model without four thousand lines of request logging.
- **You can steer Klaudia while it works.** Typing "don't modify the API"
  mid-turn used to hold the text until the turn ended and send it afterwards, by
  which point the API had been modified. The correction now lands in the request
  that decides the next action. `/stop` asks Klaudia to finish the current step
  and report what it did — "stop after this test run" should not throw away the
  test run.
- **`!command` runs the shell without leaving the conversation**, and its output
  becomes context, so "revert that" has a referent. The input marker switches
  from `›` to `$` as soon as the line starts with `!`, so what Enter will do is
  visible before you press it.
- **A turn ends with what changed and what was verified**, kept apart on
  purpose: conflating them is how "I fixed it" comes to mean "it compiles". Only
  test runners, typecheckers, linters and vet count as verification — a passing
  `go build` is not evidence the behaviour is right. Failures stay visible with
  their count. A turn that changed nothing prints nothing.

### Changed
- **Commands run in their own process group**, and stopping one signals the
  whole group. See Fixed.
- **Children are told there is no terminal**: `GIT_TERMINAL_PROMPT=0`,
  `GIT_PAGER=cat`, `PAGER=cat`, and the real `COLUMNS`/`LINES`. Credential
  helpers are untouched. Your own `$PAGER` still drives Klaudia's long-output
  view, which has a real terminal.
- **Programs needing a terminal are refused before launch**, naming the flag
  that would have worked: `git rebase --onto`, `git add --`, `-m`,
  `ssh <host> '<command>'`. Klaudia does not allocate a PTY — one would make
  `vim` "work" in a surface the model cannot drive and you cannot see, so the
  turn would look successful while sitting in an editor forever. **Known
  limitation:** terminal resize does not reach child processes.
- **A timed-out service says so.** A bare `exit 124` reads as "the command is
  broken" and invites a retry with a longer timeout; when the command looks like
  a service, the result names `run_in_background` instead.
- **Klaudia handles SIGINT.** It had no signal handling at all, which was
  survivable only because children shared its process group. Now that they do
  not, Ctrl+C cancels the run and tears jobs down properly.

### Fixed
- **Stopping a background command left its children running.** Nothing set
  `Setpgid`, so `exec.CommandContext` signalled only the shell, not what the
  shell started — verified with a probe: a `sleep 60` grandchild survived cancel
  and Wait. That single gap is why stopping a job did not stop it, why Esc
  during a command did not end it, and why the next `npm run dev` became a
  second copy. Kill now signals the group, SIGTERM then SIGKILL after two
  seconds so a server releases its port and flushes its log rather than being
  truncated mid-sentence.
- **Background output was never written down.** It lived in a slice that only
  grew, so an afternoon's dev server held every line it ever printed in
  Klaudia's heap, none of it pageable, searchable or readable after the session.

### Added
- **Klaudia works autonomously inside the project and asks before changing your
  machine.** The old model asked per command, which produced prompt fatigue for
  ordinary work and approvals that neither covered the operation you meant nor
  stopped at its edges. Every tool call is now classified into a zone: project
  work, network fetches and work on a remote host the task calls for all proceed
  without asking — including the destructive parts, because `rm -rf ./dist` and
  `git reset --hard` are ordinary. Changes to this machine, and local credential
  material, stop.

  A few consequences are deliberate and worth knowing. Build caches under `$HOME`
  (`~/.cache`, `~/go/pkg/mod`, `~/.npm`, `~/.cargo`, `~/.m2`) are project zone,
  or every build would prompt. The rest of `$HOME` is your data and is not
  protected — this protects the machine, not the home directory — but `~/.zshrc`,
  `~/.gitconfig` and `~/Library/LaunchAgents` are host, because they configure
  your login session and persist. A project at `/opt/app` or `/usr/local/src` is
  still the project. `sudo` is not itself the trigger: `sudo -u deploy
  ./scripts/deploy.sh` in the project is project work.

  `ssh staging sudo systemctl restart nginx` is the job you asked for; the same
  line without the `ssh` is a change to the machine you are typing on. Using a
  credential (`ssh -i ~/.ssh/key`, `curl --cert`) is ordinary; printing one
  (`cat ~/.ssh/id_rsa`) is not.

  **This is a guardrail against well-intentioned mistakes, not a security
  boundary.** It reads command lines and tool inputs; it does not observe what
  programs do, so a command that computes its own target or a package's
  postinstall hook goes past it. For enforcement the kernel applies, set
  `[sandbox] mode = "os"`. See [docs/trust.md](docs/trust.md).
- **`RequestHostChange`: one approval covers a whole operation.** Klaudia
  declares what it intends to change and why, in your terms — "install nginx and
  configure it as a development proxy" rather than `sudo apt-get install -y
  nginx`. Approving it covers the package install, the config directory, the
  write, the validate and the restart. Anything outside the scope stops and says
  "this wasn't part of what you approved"; declining fails that one tool call, so
  work already done stands.

  Approving one file inside a directory covers that directory, so the second step
  of an approved change does not ask again — but approving `/etc/hosts` does not
  hand over `/etc`, a request for a whole system directory is refused before it
  reaches you, and an install grant does not authorise a removal. Approvals are
  session-scoped and never written to disk. There is no always-allow for a host
  change: a standing permission to reconfigure your machine is one you cannot see
  and did not schedule the end of.
- **`/trust`** shows the guardrail's state, the approvals live this session and
  what they reach, what the classifier has found, and any allow/deny rules
  carried over from the per-command model. `/trust revoke <id>`, `/trust revoke
  all`, `/trust upgrade`, `/trust observe`, `/trust off`.
- **`--allow-host-changes`** for unattended runs on a machine you are willing to
  have reconfigured. Without it, headless runs still do project and remote work
  and refuse host changes with a message naming the flag — rather than the old
  behaviour of denying everything that would prompt, in silence.

### Changed
- **The status line counts running jobs.** A dev server holding a port was
  invisible until the next start collided with it. Shown only when something is
  up, so it costs nothing the rest of the time — and it fills the slot the mode
  segment vacated.
- **`bypass` is styled as a warning.** In the same dim grey as the token count,
  the one mode where nothing is checking anything read as ordinary chrome.
- **The status line no longer announces the mode when nothing is unusual.** Once
  autonomous became the default it was on the line in almost every session, and
  a segment that never changes stops being read — while still outranking the
  context percentage, which is the one number there anyone acts on. `plan`,
  `bypass` and the legacy modes still appear, and still survive first when a
  narrow terminal starts dropping segments; "working normally" does not.
- **Permission modes collapse from six to three:** `autonomous` (the new
  default), `plan`, `bypassPermissions`. The old set asked you to pick a stance
  on file edits versus commands versus network, which is a question about tool
  categories and never had a good answer; zones answer it now. `default`,
  `acceptEdits` and `dontAsk` stay valid so existing configs keep working, and
  are no longer offered as a choice. `autonomous` is refused unless the host
  guardrail is enforcing — without it, it would be `bypassPermissions` under
  another name.
- **A config that already has `[permissions]` rules starts in observe mode**,
  with a one-time notice: the classifier runs and `/trust` reports what it found,
  but nothing is refused and your per-action prompts continue until you run
  `/trust upgrade`. Existing allow/deny rules keep working; Klaudia no longer
  creates new ones.
- **`--loop` no longer requires `--dangerously-skip-permissions`.** It needed it
  only because no mode would edit a file in the project without asking, so
  running unattended meant turning off every check to get past prompts about
  ordinary work. Use `--permission-mode autonomous`.
- **`/commit` stages what Klaudia changed, not everything.** It ran `git add -A`,
  which swept up the half-finished change you left open in another editor and the
  scratch file you meant to delete, into a commit whose message described
  something else. It now stages only the files this session edited, lists what it
  is leaving out, and — if you staged something yourself — commits exactly that
  and adds nothing on top.
- **Sub-agents share the parent's guardrail and its approvals.** A sub-agent must
  not be a way around the boundary, and an approval you gave the parent should
  cover the child doing the work.

### Fixed
- **The Bash parser did not see output redirections**, so `echo x > /etc/hosts`
  looked like a harmless `echo`. It also silently fabricated paths from partial
  expansions — `> "$HOME/notes.txt"` was recorded as a write to `/notes.txt`, an
  absolute path that looks real and is not — and dropped whole commands whose
  program name came from a variable (`$SUDO apt-get install`). Words now carry
  whether their text is the whole story, and anything deciding what a command
  touches has to check.
- **Tools ran in Klaudia's process directory, not the project.** `WorkingDir` was
  declared and never set, so `cmd.Dir` was never assigned and Grep/Glob rooted
  themselves wherever Klaudia happened to start.
- **Sandbox roots were compared unresolved**, so on macOS a write to `/etc/foo`
  never matched the policy prefix `/private/etc` and looked harmless.

### Changed
- **The input is drawn in a box, and the status line is its caption.** A dim
  full-width "model · mode · turns · tokens" line reads as a status bar, and
  status bars belong pinned to the bottom of a window — so inline rendering,
  which leaves it wherever the cursor is, made it look misplaced. The position
  was never the problem: the line had nothing visible to belong to. Framing the
  input gives it one. The caption also drops whole segments rather than
  characters when the terminal is narrow, so it degrades to "opus-5 · ask"
  instead of "opus-5 · ask · 0 tur", and the box gives way to a bare input below
  30 columns or 10 rows, where the border costs more than it buys.
- **The default model is now `claude-opus-5`** (was `claude-sonnet-4-6`), and
  the `opus`/`sonnet` aliases track the current lineup (`claude-opus-5` /
  `claude-sonnet-5`); `fable` is added for `claude-fable-5`.
- **The TUI renders inline instead of taking over the screen.** Klaudia used the
  alternate screen with a custom viewport, which hid the shell scrollback you
  launched from, denied the terminal's own search and selection over the
  conversation, threw the session away on exit, and — measured — performed worse
  than the pager it replaced: re-wrapping the whole transcript on every streamed
  token cost 102µs at 10 turns, 476µs at 50 and 1880µs (plus 2MB of garbage) at
  200. Finished output now goes into real scrollback via `tea.Println` and only
  the input and status bar are redrawn in place, so scrolling, drag-to-select,
  terminal search, tmux copy mode and output-survives-exit all work again.
  Per-token cost is now flat in session length and allocation-free (~130ns).
  `PgUp`/`PgDn` and mouse capture are gone rather than reimplemented; `/theme`
  applies to new output only, since printed text cannot be restyled.
- **`Ctrl+C` no longer quits on the first press.** It now does the smallest
  useful thing available — interrupt a running turn, cancel an open prompt,
  clear a non-empty draft — and quits only when pressed twice in a row.
- **`/last` takes an argument** (`/last <n>`, `/last list`) and opens long output
  in `$PAGER` rather than printing it, so paging, search and copy are the
  pager's job.

### Added
- **Model discovery in `/model`.** With no argument it now asks the provider
  which models it actually serves and offers them as a picker, instead of
  requiring you to type an exact ID from memory. Both backends answer at
  `GET /v1/models` — Anthropic via the SDK (with the OAuth beta header the rest
  of the client sends), OpenAI-compatible endpoints by convention — exposed as
  an optional `api.ModelLister` so a future backend that can't enumerate still
  satisfies `api.Provider` and simply falls back to type-the-ID. Selecting a
  model also records the context window the provider reports for it.
- **`/copy`** — put the last answer, a code block, a tool result or the whole
  conversation on the system clipboard using OSC 52, so it works over SSH and
  inside tmux. It copies from raw sources, never from rendered output.
- **`/search`, `/outline`, `/errors`, `/show`** — index the session and report
  matches. Inline rendering means the app can't move the terminal's scroll
  position, so these print results rather than jumping.
- **`/open <path:line>`** — open a reference copied from a stack trace or
  compiler error at the right line in `$EDITOR`.
- **A `light` theme**, and `NO_COLOR` support. Every previous theme assumed a
  dark background, and glamour was hardcoded to a TrueColor profile that ignored
  `NO_COLOR` entirely.

### Fixed
- **Pasting is byte-exact.** bubbles' textarea sanitises all input, replacing
  tabs with four spaces and mapping `\r` and `\n` to newlines *independently* —
  so CRLF text arrived with every line doubled and pasted Go, Python or Makefile
  source silently lost its tabs. Pastes are now intercepted before the widget
  sees them; large or tab-bearing ones are stored verbatim and shown as a chip
  (`[#1 pasted · 42 lines]`) that expands on submit, which also stops a
  thousand-line paste from swamping the six-row input box.
- **Rendered code copies as source.** Glamour padded every line to the wrap
  width and indented code blocks, so a copied snippet carried a four-space
  prefix and dozens of trailing spaces. Margins and block backgrounds are
  dropped and the residual padding is stripped from the rendered string.
- **Tabs survive.** lipgloss expanded them to four spaces everywhere, destroying
  the column structure of anything tabular a tool printed.
- **Earlier tool output is recoverable.** Only the single most recent result was
  kept, so a long build log became unreachable as soon as any later tool ran. A
  bounded ring now holds the last 200 results, and the inline preview names the
  number to ask for.
- **Tool-result previews are rune-safe.** They were truncated with a byte slice
  that cut multi-byte characters in half and printed replacement characters.
- **`@` completion finds nested files.** It was prefix-only, so `@session.ts`
  could never match `src/auth/session.ts`. It is now fuzzy, caches the repo
  listing instead of re-walking it on every Tab keystroke, and ranks files
  Klaudia recently read or wrote first — the previous code discarded
  `search.Glob`'s mtime ordering by re-sorting alphabetically.
- **The model and the UI no longer share one truncated string.** `tools.Result`
  gained a `Full` field (surfaced as `agent.Event.FullContent`) carrying the
  untruncated output for local display only. Bash clamps what the model sees to
  protect the context window; `/last` now shows everything the command actually
  printed — 245 KB where the model saw 30 KB, in the end-to-end test — and says
  so. This also removes the TUI's coupling to a magic string in the tool's own
  output: it was parsing the spill-file path back out of the notice. That notice
  remains, because it is what lets the *model* read the elided middle.
- **Bash output keeps its tail.** Output over 30 KB was truncated head-only, so
  the part people actually want — the `FAIL` summary from `go test ./...`, the
  error that stopped a build, the end of a stack trace — was thrown away, for
  the model as well as the user. The same budget is now split head+tail, cut on
  line boundaries, and the untruncated text is written to `~/.klaudia/outputs/`
  and named in the notice, so `/last` shows the complete log and the model can
  grep it. Spill files are pruned after 24 hours. The old truncation also
  sliced bytes, which could cut a multi-byte character in half.
- **Context-window reporting was understating the window ~5×.** The static
  per-model table claimed 200K for the current lineup, on the theory that 1M
  needed the `context-1m-2025-08-07` beta that `DefaultBetas` doesn't send.
  `GET /v1/models` reports 1M for those models, so the status bar's `ctx N%`
  had been overstating context pressure accordingly. The table is corrected
  against the live endpoint, and `/model` now prefers the provider's own figure
  over any table at all. `humanTokens` gained an M tier — a 1M window rendered
  as the unreadable "1000.0k".
- **`Ctrl+U` and `Ctrl+D` reach the input.** They were bound to viewport paging
  and matched before the textarea saw them, so readline's kill-line and
  delete-forward never worked.

### Added
- **Prompt caching (Anthropic).** Requests now set `cache_control` breakpoints on
  the stable prefix (tools + system prompt) and a rolling conversation
  breakpoint, so each turn no longer re-pays full price for the whole history.
  Because the system prompt and tools are byte-stable across launches, this also
  caches across `--continue`/resume (verified live: the resumed turn reads the
  whole prior prefix from cache rather than re-creating it). Cache usage is
  reported in the result envelope (`cache_read_input_tokens` /
  `cache_creation_input_tokens`). Disable with `KLAUDIA_DISABLE_PROMPT_CACHE`.

### Fixed
- **Tool order is now stable.** `Registry.Names()` iterated a map, so the tool
  list (and thus the request's cached prefix) was shuffled every turn — which by
  itself defeated prompt caching. Names are now sorted; verified live to take
  cache reads from 0 to most of the prefix.
- **Streaming no longer hangs forever.** A stalled model stream (half-open SSE
  connection) used to leave the TUI stuck on "thinking…". An idle watchdog now
  breaks a stalled turn — transparently retrying when nothing has been emitted
  yet, otherwise failing with a clear timeout. Configurable via
  `KLAUDIA_STREAM_IDLE_TIMEOUT` (seconds, default 120; `0` disables). Covers both
  the Anthropic and OpenAI-compatible providers.
- **Long-context-credits 429** now reports honestly: the *"Usage credits are
  required for long context requests"* error is a billing gate, so the message no
  longer suggests retrying — it points at adding credits or reducing context.
- **Session resume "lost memory".** Auto-resume could land on an empty transcript
  left by a launch that recorded nothing (quit before typing, or every turn
  errored on an expired token), reporting "resumed" with no context. `MostRecent`
  now skips contentless transcripts, and transcripts are created lazily on first
  write so aborted launches leave no file behind.
- **Markdown files rendered as soup.** Reading a `.md` file in the TUI ran its
  `cat -n` output through the Markdown renderer, collapsing every line into one
  reflowed block with inlined line numbers. Line-numbered tool output is now
  shown verbatim.

### Changed
- Docs: corrected the prompt-caching status in `docs/parity.md` (planned, not
  implemented) and documented the streaming/reliability knobs in the README.
