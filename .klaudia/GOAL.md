# Goal: Auto-resume project sessions

## Objective

Make Klaudia automatically resume the most recent existing session for the current working directory by default, using the project-scoped session storage under `~/.klaudia/sessions/<encoded-cwd>/` (for example `Users-nickglynn-Projects-claude-code...`) when available. Starting a fresh session should become an explicit opt-out via a new `--new-session` flag, making the default startup behavior feel continuous without requiring the current manual session/resume flow.

## Acceptance criteria

- [x] Starting `klaudia` in a directory with one or more stored sessions automatically loads the most recent session for that current working directory.
- [x] Starting `klaudia` in a directory with no stored sessions creates a new session without error.
- [x] Passing `--new-session` always starts a fresh session and does not auto-resume an existing one.
- [x] Auto-resume is scoped to the current working directory and does not pick up sessions from unrelated projects.
- [x] Existing explicit resume/session behavior remains compatible or is intentionally superseded with clear CLI help text.
- [x] CLI help documents the default auto-resume behavior and the `--new-session` opt-out flag.
- [ ] Session persistence continues to store sessions under the existing project-scoped `~/.klaudia/sessions/...` layout.

## Verify

```bash
CGO_ENABLED=0 go build ./cmd/klaudia && go test ./internal/...
```

## Progress

- Done: Added default project-scoped auto-resume by selecting `session.MostRecent(cwd)` when neither `--resume` nor `--new-session` is supplied; added `--new-session` as the explicit opt-out; preserved explicit `--resume` precedence and kept `--continue` compatible.
- Done: Added CLI tests for auto-resume, no-session fresh start, `--new-session` opt-out, unrelated-project scoping, and explicit `--resume` compatibility.
- Done: Verified CLI help includes the default auto-resume wording and the `--new-session` flag.
- Verified: `CGO_ENABLED=0 go build ./cmd/klaudia && go test ./internal/...` passes.
- Remains: The implementation still stores transcripts under the current `~/.klaudia/projects/<encoded-cwd>/` root; the spec says `~/.klaudia/sessions/...`, so the storage-layout acceptance item needs either an implementation update or a spec clarification.
- Next step: Align session storage with the requested `~/.klaudia/sessions/<encoded-cwd>/` layout while preserving compatibility with existing `~/.klaudia/projects/<encoded-cwd>/` transcripts.
