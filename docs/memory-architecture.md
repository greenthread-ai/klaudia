# Memory Architecture

How klaudia's auto-memory works today, and exactly what's needed to swap the
filesystem backend for a Postgres-backed one when [pgmarkdown][1] ships.
Implemented in `internal/memory`, surfaced to the model through
`internal/tools.Memory`, and surfaced to humans through the TUI's `/memory`
slash command.

[1]: https://github.com/greenthread-ai/pgmarkdown — see `../pgmarkdown/PRD.md`

## Why markdown, not vectors

The shape klaudia uses — a short always-loaded index + detail notes opened
on demand + a separate curated knowledge surface — is a hand-rolled
attention system. Vector retrieval is good at "find semantically similar
text"; agent memory needs something different:

- **Salience over volume.** Most failures aren't because the right note is
  missing; they're because it's not surfaced at the right moment. A small
  curated index in context beats a large fuzzy recall.
- **Exact identifiers matter.** Function names, branch names, error
  strings, config keys — embeddings are notoriously weaker at these than
  lexical search.
- **Editable.** Memory needs to be rewritten, contradicted, marked stale,
  superseded, promoted. Vector stores accumulate sediment; markdown
  invites curation.
- **Epistemic cues.** A note can declare `status: superseded` or
  `supersedes: old-decision` in frontmatter — vector chunks rarely carry
  enough metadata for that without a layer on top.

Vector stores are retrieval infrastructure. Markdown is cognitive
infrastructure.

## The three surfaces

```
.klaudia/
  MEMORY.md         # always-loaded session index — bullets + linked-memory section
  memory/*.md       # detail notes — opened on demand via Read or Memory.search
  KNOWLEDGE.md      # curated, durable project knowledge — system-prompt-injected verbatim
```

- **`MEMORY.md`** is the always-loaded index. Session bullets at the top
  (`- 2026-06-04T15:00Z observation here`), a `## Linked memory` section
  at the bottom listing detail notes. The model reads this every session.
- **`memory/<name>.md`** files are detail notes. The agent (or a human)
  writes them with optional YAML frontmatter (`tags`, `status`,
  `supersedes`, etc.). They're not inlined into the prompt — the model
  opens them on demand via `Read` or via `Memory.search`. Keeps recall
  cheap as memory grows.
- **`KNOWLEDGE.md`** is the curated, durable knowledge surface — "facts
  we've validated, conventions, lessons learned". System-prompt-injected
  verbatim under `# Project knowledge`. Has a different lifecycle from
  session memory: durable, near-canonical, gated.

## The Store + Knowledge interfaces

Both are in `internal/memory/store.go`. The filesystem implementation
lives in `internal/memory/fs.go` + `fs_lookup.go` + `fs_lifecycle.go`
+ `fs_links.go`. A future Postgres implementation will land in the
same package as `pg.go` when pgmarkdown is buildable.

```go
type Store interface {
    Path() string

    // Index surface (always-loaded MEMORY.md analogue)
    Index() (string, error)
    Entries() ([]string, error)
    Add(text string) error
    Search(query string) ([]string, error)
    FilePointers() []string
    SyncLinks() error

    // Detail-note lookup (added in the refactor — pgmarkdown-shaped from day one)
    Recent(within time.Duration) ([]Entry, error)
    Stale(olderThan time.Duration) ([]Entry, error)
    ByTag(tag string) ([]Entry, error)

    // Lifecycle
    Promote(name string) error
    Supersede(oldName, newName string) error
}

type Knowledge interface {
    Path() string
    Read() (string, error)
    Add(text string) error
}
```

Both interfaces have a `Disabled()` constructor returning a no-op
implementation for headless mode — reads return zero values; writes
return `ErrDisabled`. Lets the TUI / CLI / tool drop nil-guard
boilerplate.

## The conformance suite IS the contract

`internal/memory/conformance_test.go` is the contract document for any
backend. `RunStoreSuite(t, name, factory, wantWrites)` exercises every
method against a fresh `Store` from `factory`. `wantWrites` toggles
between writable (must round-trip `Add`, must surface
`ErrEmpty`/`ErrNotFound` for semantic failures) and read-only (writes
must return `ErrDisabled`).

Today the suite drives `fsStore` (writable) and `Disabled()` (read-only).
22 subtests, all green. When the pgmarkdown backend lands, the same
suite will drive `pgStore` with `wantWrites: true` — and if it passes,
klaudia can swap backends at config time without touching any agent
code.

Note: the conformance suite asserts **behaviour**, not on-disk format.
The FS-shape assertions (timestamp format, header structure, linked-
section idempotency) stay in `fs_test.go` and `knowledge_test.go` —
they encode "this is what `.klaudia/MEMORY.md` looks like on disk",
which the PG implementation will and should not mimic.

## Frontmatter

YAML, parsed by `gopkg.in/yaml.v3` (already a transitive dep). The
recognised subset:

```yaml
---
tags: [decision, memory]
created: 2026-06-04T12:00:00Z
updated: 2026-06-04T15:00:00Z
status: active
supersedes: old-decision
superseded_by: KNOWLEDGE.md
---
```

All fields are optional. Files without an opening `---\n` line parse to
a zero-value frontmatter struct and the full body — this is the common
case today and is not an error. Unclosed fences and malformed YAML also
fall back to no-frontmatter; the note remains recallable via title and
mtime even when metadata is broken.

YAML chosen because (a) it's already a transitive dep, (b) it round-trips
cleanly to JSON, which is what pgmarkdown assumes for its `frontmatter
jsonb` column. TOML is a possible future fallback if hand-authored
notes turn out to prefer `+++`-delimited blocks — switchable later by
sniffing the first line.

## The Memory tool's protocol

`internal/tools.Memory` exposes the Store ops to the LLM through a
single tool with an `operation` enum:

| op           | inputs                       | does                              |
| ------------ | ---------------------------- | --------------------------------- |
| `view`       |                              | read the MEMORY.md index          |
| `search`     | `query`                      | match across index + detail notes |
| `add`        | `content`, `scope` (default `session`) | append bullet to MEMORY.md, or to KNOWLEDGE.md when `scope=project` |
| `recent`     | `within` (default `7d`)      | detail notes touched within window |
| `stale`      | `older_than` (default `30d`) | detail notes older than threshold |
| `by_tag`     | `tag`                        | detail notes whose frontmatter tags contain `tag` |
| `promote`    | `name`                       | copy detail body to KNOWLEDGE.md, mark source superseded |
| `supersede`  | `name`, `replacement`        | record the supersession link |

Durations accept Go's `time.ParseDuration` syntax plus an `Nd` (days)
suffix so the model can say `7d` instead of `168h`. The same shape
backs the `/memory recent`/`stale` slash subcommands.

## Promotion lifecycle

The canonical pipeline:

```
observation (one-off, surprising)        →  MEMORY.md bullet (via Add)
repeated + useful                         →  detail note under memory/* (agent writes via Write tool)
confirmed durable lesson                  →  KNOWLEDGE.md (via Promote)
contradicted by newer evidence            →  Supersede the old note pointing at the new one
```

`Promote(name)` reads `memory/<name>.md`, strips its frontmatter, appends
the body to `KNOWLEDGE.md`, and rewrites the source's frontmatter with
`status: superseded` and `superseded_by: KNOWLEDGE.md`. The source file
stays on disk — the supersession trail is walkable, and a hypothetical
"unpromote" path stays possible.

`Supersede(oldName, newName)` rewrites both notes' frontmatter — old
gets `status: superseded` + `superseded_by: newName`, new gets
`supersedes: oldName`. Idempotent; calling twice produces the same
bytes.

The destructive alternative (delete on promote) was considered and
rejected: it loses the audit trail and can't recover from a bad
promotion.

## What pgmarkdown adds when it ships

This section is the migration runbook. When the [pgmarkdown PRD][2] hits
Phase 7 (filesystem ingestion) and the extension is loadable into a
Postgres instance, klaudia can adopt it as a memory backend.

[2]: ../pgmarkdown/PRD.md

### 1. Add the pgmarkdown driver dep

```sh
go get github.com/jackc/pgx/v5
```

`pgx` is the recommended driver — `lib/pq` is unmaintained. Add to
`go.mod` only at this step; the substitutable-backend refactor was
specifically designed so the dependency tree stays clean until the
backend is actually wired.

### 2. Implement `pgStore` against the conformance suite

Create `internal/memory/pg.go`:

```go
package memory

type pgStore struct {
    db   *pgx.Conn  // or *pgxpool.Pool
    name string     // project / tenant label
}

func NewPg(ctx context.Context, dsn, name string) (Store, error) { ... }

// implement Store methods by wrapping pgmd.* SQL functions
// (see pgmarkdown PRD §"Core primitives" for the contract)
```

Each method should map to one pgmd primitive:

| `Store` method                  | pgmarkdown surface                            |
| ------------------------------- | --------------------------------------------- |
| `Index()`                       | materialised view over `pgmd.documents`       |
| `Add(text)`                     | INSERT into `pgmd.documents` with type=`bullet` |
| `Search(q)`                     | `pgmd.search(q, mode := 'lexical')`           |
| `Recent(within)`                | `pgmd.search('', mode := 'recent', within := within)` |
| `Stale(olderThan)`              | `pgmd.search('', mode := 'stale', within := olderThan)` |
| `ByTag(tag)`                    | `pgmd.search('', mode := 'by_tag', tag := tag)` |
| `Promote(name)`                 | `pgmd.promote(name, 'knowledge')`             |
| `Supersede(old, new)`           | `pgmd.supersede(old, new)`                    |
| `FilePointers()`                | derived from the documents table              |
| `SyncLinks()`                   | no-op (PG keeps its own materialised view)    |

### 3. Add a conformance test

Create `internal/memory/pg_test.go` with the standard PGXS test
pattern (requires a Postgres instance with pgmarkdown installed, gated
by an env var):

```go
func TestPgStoreConformance(t *testing.T) {
    dsn := os.Getenv("PGMARKDOWN_TEST_DSN")
    if dsn == "" {
        t.Skip("set PGMARKDOWN_TEST_DSN to run pg backend tests")
    }
    RunStoreSuite(t, "pg", func(t *testing.T) Store {
        // fresh schema per test for isolation
        return mustNewPgStoreInFreshSchema(t, dsn)
    }, true)
}
```

If the suite passes, the contract is satisfied.

### 4. Add a config switch

In `internal/config/config.go`, extend `Memory` (or add a new
`Memory.Backend` field):

```toml
[memory]
backend = "postgres"               # "filesystem" (default) | "postgres"
dsn     = "postgres://…"           # required when backend = "postgres"
name    = "klaudia/my-project"     # tenant label, default = cwd
```

In `internal/cli/root.go`, the existing `memStore := memory.New(...)`
line forks based on `cfg.Memory.Backend`:

```go
var memStore memory.Store = memory.Disabled()
switch cfg.Memory.Backend {
case "", "filesystem":
    memStore = memory.New(filepath.Join(cwd, ".klaudia"))
case "postgres":
    memStore, err = memory.NewPg(ctx, cfg.Memory.DSN, cfg.Memory.Name)
    if err != nil { ... }
}
```

No other call site changes. The TUI, the LLM tool, the slash command,
the prompt assembly — all already depend on the interface.

### 5. Document and migrate

- Add a `klaudia memory migrate --to postgres` subcommand that walks
  `.klaudia/memory/*.md` and calls pgmarkdown's `pgmd.ingest_dir`
  (PRD Phase 7).
- Add a paragraph here pointing at the migration command.
- Bump the `pg_test` env var into CI so the contract stays enforced.

### What multi-app sharing looks like

Once the PG backend ships, multiple klaudia instances (and any other
agent framework that satisfies the `Store` interface or speaks
pgmarkdown SQL directly) can read and write the same memory store.
The episodic / promote / supersede shape applies the same way; the
backend is just the substrate.

## Risks and decisions

- **YAML lock-in.** Switchable later by sniffing for `+++` (TOML).
- **Promote keeps the source file** rather than deleting it. Keeps the
  audit trail; matches pgmarkdown's supersession graph; lets a future
  "unpromote" exist. The destructive alternative is simpler but loses
  history.
- **KNOWLEDGE.md grows unbounded** under repeated Promote. Documented;
  pgmarkdown will solve this server-side via the supersession graph.
- **No length budget on the system-prompt-injected MEMORY.md.** If
  memory grows past the model's context window, the model sees
  truncation errors. Mitigation today: prune detail notes via Stale +
  manual archive. Future: pgmarkdown can score-rank the index so only
  the salient bullets get injected.
- **`Disabled.SyncLinks()` returns nil**, not `ErrDisabled`. Today's
  call site (`internal/cli/root.go:672`) calls it for side effect and
  treats errors as best-effort; surfacing `ErrDisabled` there would
  print a spurious warning every headless run.

## See also

- [`internal/memory/store.go`](../internal/memory/store.go) — interface
  definitions and the package-level `New` / `Disabled` constructors.
- [`internal/memory/conformance_test.go`](../internal/memory/conformance_test.go)
  — the contract any backend must satisfy.
- [`docs/compaction.md`](compaction.md) — the parallel mechanism for
  keeping conversation history within the context window.
- [`../pgmarkdown/PRD.md`](../../pgmarkdown/PRD.md) — the extension this
  doc's migration plan targets.
