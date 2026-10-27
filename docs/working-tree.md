# Your changes, Klaudia's changes, and undo

A working tree is a shared surface. You have two files half-edited before
Klaudia starts; it touches four more; while it works you fix a typo in a third.
Afterwards `git status` cannot tell those apart — which is how an undo eats an
afternoon and how a commit message ends up describing a third of its own diff.

## Ownership

Klaudia tracks three facts, all cheap:

- **What was already dirty** when the session began, captured once at startup.
- **What it wrote**, recorded when each write completes.
- **Whether anything moved underneath it** — a size and mtime stamp taken after
  each write, so an edit you make in another window is noticed.

`/changes` renders the split:

```
Working tree

Your existing changes
  M src/config.ts

Klaudia
  M src/auth/session.ts
  M test/auth/session.test.ts

Both
  M shared.go
  changed by Klaudia and by you — undo will not touch these
```

**Both** is the interesting category. A file Klaudia wrote that was already
dirty, or that you edited afterwards, belongs to neither alone. Klaudia cannot
merge the two changes, but it can refuse to pretend they are not there.

## `/commit`

Stages only Klaudia-owned files, lists what it is leaving out, and — if you
staged something yourself — commits exactly that and adds nothing on top. An
explicit `git add` is a statement about what belongs in the commit; overriding
it would be the same mistake as `git add -A` in the other direction.

Shared files are deliberately excluded. Sweeping your edit into a commit
describing Klaudia's work is precisely the bug this replaced.

`/commit <message>` uses your message. A bare `/commit` has the model draft
one from the diff of exactly what will be committed, the repository's recent
subjects (so it follows house style) and your last request. That is a single
call outside the conversation, so it adds nothing to the history. The draft
is shown and still needs your y; `n` and `/commit <message>` writes your own.

## `/undo`

**The one guarantee: undo cannot destroy work you did.** Everything else — how
far back it goes, how clever it is about hunks — is secondary, because an undo
that occasionally eats an afternoon is worse than no undo at all.

Before a turn writes a file, Klaudia stores the current contents as a git blob:

```
git hash-object -w -- path
```

That is a plain object write. It does not touch the index, does not touch HEAD,
creates no stash entry, and is invisible to `git status`. Your staging area is
exactly as you left it.

A stash would have been simpler and is wrong: it moves the *whole* working tree
including your unrelated edits, and popping it later can conflict. The index is
wrong for the same reason — it is yours.

`/undo` shows the plan before doing anything, including the equivalent commands:

```
Undo: "fix the refresh-token race"
  restore  src/auth/session.ts
  delete   src/auth/session.test.ts  (Klaudia created it)

Leaving alone — you changed these too:
  · shared.go

Equivalent by hand:
  git cat-file -p a1b2c3d4 > src/auth/session.ts
```

Undo is per turn, not per write: several edits to one file restore to the state
before the turn began. The stack holds ten; deeper history is what git is for.

Outside a git repository there is no object database, so nothing is undoable —
and Klaudia says so rather than pretending.

## Sub-agents get a checkout of their own

Two sub-agents editing one working tree is not a race in the usual sense. No
file is corrupted and nothing reports an error: one writes a file, the other
reads a half-finished version and reasons about it, a third rewrites the first
one's edit, and every step succeeds. The alternative is running children one at
a time, which is the thing concurrency was for.

So a sub-agent whose toolset can write — anything holding Write, Edit,
NotebookEdit or Bash, and not running read-only — is given its own `git
worktree` (under the config root, `~/.klaudia` by default):

```
~/.klaudia/worktrees/<project>/<agent-type>-<timestamp>/
```

Not inside the project. A checkout under the project root is walked by the
parent's own Glob and Grep, so the model finds two of every file and reads a
copy of the code it is editing.

Explore and Plan do not get one. They hold Bash, but they run in plan mode
(`subagent.Type.Mode`), so nothing they run can write and there is nothing to
isolate them from — and their entire output is paths, which a checkout would
make wrong. (Under `bypassPermissions` plan mode does not bind them, so they
are given a checkout like any writer.)

### It holds your uncommitted work, not HEAD

`git worktree add <dir> HEAD` on its own would hand the child the last commit:
the one state nobody asked about. A child told to "test the function I just
wrote" would look at a tree without it and report, convincingly, that there is
no such function.

The checkout is therefore seeded with the tracked delta (`git diff HEAD`,
staged and unstaged together) and the untracked, non-ignored files, and that
state is committed on the checkout's detached HEAD as the baseline the child's
work is measured against. The commit skips hooks and signing: a repository's
pre-commit hook is aimed at the user's commits, and running their linter every
time a sub-agent spawns is a side effect nobody asked for.

**Ignored files are not copied.** `node_modules`, `target/`, `.venv` — build
output is what makes a copy expensive, and a tree that builds is a different
promise from a tree that matches. The consequence is real and worth knowing: a
child that must run an install step before it can test will pay for it, or
fail. That is what `[subagents] worktree = false` is for.

### Coming back

When the child finishes, its work is diffed against the baseline and applied to
your tree with `git apply` — which touches files and not the index, the same
reason `/undo` writes loose objects instead of stashing. Your staging area is
exactly as you left it.

`git apply` is all-or-nothing, and that is the behaviour worth having: a patch
that no longer fits is one whose file changed underneath us. It is then retried
file by file, so one collision does not discard four good files, and what did
not fit is reported to both you and the model:

```
[Working tree: 3 files applied to the working tree; 1 file NOT applied
 (changed meanwhile): internal/api/client.go. The sub-agent's versions of
 those files are in ~/.klaudia/worktrees/…]
```

The model is told because it has to be: it asked a child to change files, and
a silent conflict would have it carry on describing work that is not there.
The checkout survives when anything conflicted — it holds the only copy of that
version — and is removed when everything landed.

Adoption is serialised per repository. Two children finishing at the same
instant would otherwise each read, compute and write the same file, and the
loser's version could land on top of the winner's with neither reporting a
conflict: the precise failure the feature exists to remove. Measured, not
assumed — without the lock, two concurrent adoptions of one file reported
success twice and kept one version.

### When the child fails

A failed or interrupted sub-agent keeps its checkout, and the error says where
it is. Half a change applied to your tree is the outcome isolation exists to
prevent, and nobody has looked at what that child left behind. Abandoned
checkouts are pruned after seven days, lazily, the next time one is created.

### Two sharp edges

- `.git` inside the checkout is a *file* pointing into the main repository, not
  a directory. Everything git works; a tool that looks for a `.git` directory
  to decide "is this a repo" may disagree.
- Under `sandbox.mode = "os"`, the writable roots follow the child's working
  directory, so the checkout is writable — but the object database it needs is
  back in the project's `.git`, which is not. A child that runs git commands
  under OS confinement is the combination to watch.


## Resume

Resuming reconciles rather than reports. The working tree is re-read, ownership
is recovered from the transcript's own Write/Edit calls, and jobs are named as
*stopped* — they were children of a process that has exited, and the
conversation you are about to continue implies they are still up.

Approvals are deliberately **not** restored. They are session-scoped, and
resurrecting them would mean a permission you granted yesterday silently
applying today.

On screen that is one line on every resume — `↻ continuing the last session
here · 14 messages · approvals start fresh · /clear to start over` — with a
line each for a goal, uncommitted work and stopped jobs only when there are
any, and a warning only when the host guardrail is off.

## `/rewind`

`/rewind` lists the prompts you have sent this session, newest first. Picking
one shows the plan — every file Klaudia changed from that turn on, put back as
it was before it (deleted if Klaudia created it), and the conversation cut
back to before the prompt — and on `y` does it, putting the prompt back in the
input box to edit or send again. The rules are `/undo`'s: a file you also
changed is left alone and named. The cut is recorded in the transcript (a
`rewind` boundary), so `--resume` comes back rewound.

Only this process's prompts are offered: the checkpoints that make the file
half possible are kept in memory, and offering a point whose files could not
be restored would rewind the conversation and not the work. Outside a git
repository there are no checkpoints at all, and the plan says so.

## What Klaudia has in context

`/context` lists pinned files, what changed this session, the directories the
work has been in, and what was recently read — then says plainly that this is
what Klaudia looked at, not what the task needed.

```
/pin <path>      keep a file in context every turn; survives compaction
/unpin <path>
/forget <path>   drop it from tracking
```

`/pin` is the only one that changes behaviour rather than reporting it: a pinned
file is re-stated to the model every turn, which is what keeps "the architecture
doc" relevant after forty turns instead of quietly falling out of the window.

`/forget` cannot unsee what the model has already read — the message history is
the message history. `/compact` is the thing that shrinks that.
