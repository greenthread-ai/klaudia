# Testing

Klaudia is tested in layers. Each layer checks something the one below cannot,
and costs more to run. `make check` runs every hermetic layer and is what CI
runs; the live layers are run by hand.

| Layer | Command | Needs | Time | Checks |
|---|---|---|---|---|
| static | `make static` | Go | ~5s | gofmt, `go vet`, the `CGO_ENABLED=0` build |
| unit | `make unit` | Go | ~10s | package behaviour with in-memory fakes, race detector on |
| hermetic | `make hermetic` | Go | ~8s | the unit tests don't depend on the developer's `$HOME` |
| e2e | `make e2e` | Go | ~2s | the shipped binary, end to end, against a scripted model |
| smoke | `make smoke` | a credential | minutes | the binary against a live model (`haiku`) |
| torture | `make torture` | credential, docker, python3 | long | the Phase 2 agent-loop torture test, scored against its checklist |

## Unit — `internal/...`

Package tests beside the code. The agent loop is driven by scripted
`api.Provider` fakes (`scriptedProvider`, `recordingProvider` and friends in
`internal/agent`), the TUI by feeding Bubble Tea messages to the model, the
HTTP client by `httptest` servers. These are where most tests belong: fast,
precise, and they fail next to the code that broke.

A unit test must not read the developer's environment. Anything that reaches
`os.UserHomeDir` — config, skills, `CLAUDE.md`, sessions — has to pin `HOME`
with `t.Setenv("HOME", t.TempDir())`.

`make unit` runs these under the race detector with
`GORACE=atexit_sleep_ms=0`. A race-built binary otherwise sleeps a second on
exit, and the `lsp` and `mcp` tests re-exec the test binary as a fake server,
so each server they stop cost a second — 3s for one test, 13s for the `lsp`
package.

## Hermetic — `scripts/hermetic.sh`

Runs the unit tests again with `HOME` pointed at a directory full of plausible,
conflicting state: a skill, a skill directory without a `SKILL.md`, a global
`CLAUDE.md`, a `config.toml` naming a different model, an MCP server. A test
that forgot to isolate itself sees all of it and fails.

It is deliberately not an *empty* HOME. An empty directory matches what most
tests expect, so it would hide the bug rather than surface it. The first run of
this check found four skill tests that failed only for developers with their own
skills installed.

## e2e — `e2e/`

Builds the real binary once and runs it as a subprocess against `FakeModel`, an
`httptest` server that speaks the Anthropic Messages API's streaming protocol
and answers from a script. The binary finds it through `KLAUDIA_CUSTOM_ENDPOINT`.
The fake lives in `internal/fakeapi`, which the in-process `internal/cli` tests
use too, so both layers script the model the same way.

This is the layer that checks composition: flag parsing, config loading, the
SDK client, SSE decoding, the agent loop, real tools on a real disk, the trust
gate, transcripts, the stream-json output and exit codes, all in the shipped
binary. Unit tests cover each of those alone; nothing else covers them together
without a live model.

Because the model's side is scripted, every assertion is about Klaudia. A
scenario reads as the conversation it models:

```go
m := NewFakeModel(t,
	Use("Write", map[string]any{"file_path": "a.txt", "content": "apple"}),
	Use("Read", map[string]any{"file_path": "a.txt"}),
	Say("done."),
)
e := NewEnv(t, m)
r := e.Headless("use the tools", "--dangerously-skip-permissions")
// r.ExitCode, r.Events(), r.ResultEvent(); m.Requests() is what klaudia sent.
```

`m.Requests()` makes some checks stronger than their live equivalents. The
smoke test verifies `--continue` by asking a model to recall a word, which tests
the model's recall as much as the resume; the e2e test asserts the prior
transcript was in the request.

Each run gets its own `HOME`, `KLAUDIA_CONFIG_DIR`, `TMPDIR` and project
directory, and an environment built from scratch rather than inherited, so a
real API key or `KLAUDIA_*` setting in your shell cannot reach it. The fake
fails the test on any endpoint it doesn't model, so a new startup call shows up
as a named failure instead of a hang.

Host-change scenarios probe with a write to `~/.bashrc` under the run's own
temporary HOME — host state per [trust.md](trust.md) — so a regression in the
gate modifies a temp directory, not the machine running the tests.

### When to add an e2e test

When the behaviour lives in how the pieces are wired rather than inside one of
them: a flag that must reach the request, an exit code, a transcript that must
survive a restart, a process that must not outlive the session. If a unit test
can pin it, write the unit test.

### What it cannot see

The TUI and stream-json input (the e2e tests drive headless mode only), real
model judgement, and the real API's wire format as it evolves — the fake emits
what the SDK expects today. Those belong to the live layers.

## Live — `scripts/smoke.sh`, `scripts/torture.sh`, `KLAUDIA_LIVE_LSP=1`

`KLAUDIA_LIVE_LSP=1 go test ./internal/lsp/` runs the one test that talks to a
real `gopls`; it is off by default so the unit layer doesn't depend on what is
installed. The scripts need a working credential. `smoke.sh` covers the same ground as much of
the e2e suite, but against a live model, so it also checks that the real API
accepts what Klaudia sends and that a model can drive the tools from their
descriptions. Run it before a release or after touching `internal/api`.
`torture.sh` is the long agent-loop evaluation; see its header.

## Coverage

`make cover` measures statement coverage for the unit and e2e layers
separately, merges them, and prints a per-package table with a column for each.
The merged profile lands in `coverage/all.out`; `go tool cover
-html=coverage/all.out` browses it.

Look at the combined column, not the unit one. `internal/cli` is mostly reached
through the binary, so its unit figure alone says little. The e2e half works
by building the binary with `-cover` when `KLAUDIA_E2E_COVERDIR` is set; every
run then writes its counters there.

Coverage says what ran, not what was checked. A test that calls a function
and asserts nothing moves the number and protects nothing. So treat the table
as a map of untested code, not as a target to hit.

## Not yet covered

- **macOS in CI.** Klaudia ships for macOS, but CI runs Linux only. The e2e
  suite is written for unix and should run there; it hasn't been tried.
- **stream-json input** (`--input-format stream-json`) and the permission
  prompt round-trip it carries.
- **The OpenAI-compatible provider end to end.** `FakeModel` speaks the
  Anthropic protocol only. A Chat Completions twin would cover `provider =
  "openai"` the same way.
