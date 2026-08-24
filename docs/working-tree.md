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

## Resume

Resuming reconciles rather than reports. The working tree is re-read, ownership
is recovered from the transcript's own Write/Edit calls, and jobs are named as
*stopped* — they were children of a process that has exited, and the
conversation you are about to continue implies they are still up.

Approvals are deliberately **not** restored. They are session-scoped, and
resurrecting them would mean a permission you granted yesterday silently
applying today.

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
