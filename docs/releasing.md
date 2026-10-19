# Releasing

Klaudia releases are built by [GoReleaser](https://goreleaser.com) from a tag.
`.goreleaser.yaml` holds the build matrix, `scripts/release.sh` works out the
next version and cuts the tag, and `.github/workflows/release.yml` runs both.
This file records why the pipeline is shaped the way it is.

## Cutting a release

From the Actions tab: run **Release**, pick `minor` (default), `patch` or
`major`, and leave *dry run* unticked. CI vets, tests, stamps the changelog,
commits, tags, and publishes. Nothing else is manual.

Tick *dry run* to rehearse: the version is computed and printed, the matrix is
built, and nothing is committed, tagged or published.

The same thing locally, if you would rather watch it happen:

```bash
scripts/release.sh --dry-run          # what would the next version be?
scripts/release.sh minor              # commit, tag and push -> CI publishes
scripts/release.sh patch --no-push    # stop short of pushing
```

A tag pushed from a laptop triggers the workflow normally. A tag pushed by CI
does not — see below.

Rehearse just the build, with no tag, writing to `dist/` (git-ignored):

```bash
goreleaser check                                  # validate the config
goreleaser release --snapshot --clean --skip=publish
```

## Versioning

The version is the tag. There is no number in the source to bump, and the
bump size is chosen per release rather than derived from the commits.

Deriving it would mean [Conventional
Commits](https://www.conventionalcommits.org) — `feat:` for a minor, `fix:`
for a patch — and this project uses `area: lowercase sentence` instead, which
`CLAUDE.md` documents and every commit in the history follows. Tools like
release-please and semantic-release read nothing else, so adopting one means
changing the commit convention across the project to make a one-word choice
automatic. It is the same reason GoReleaser's generated changelog is off.

`scripts/release.sh` refuses to release an empty `## Unreleased` section. That
mistake is easiest to make immediately after a release, when the section is
empty and the habit says to run the command again.

The first release is `v0.1.0`: with no tags in the repository there is nothing
to bump from, and any bump size lands there.

## What a released binary reports

`version.Version` is a frozen constant, `2.1.66-klaudia`, mirroring the retired
JavaScript bundle so the parity harness sees identical `--version` output. It
does not move, and a release does not move it.

The release identity is the commit, which `version.Build()` reads from the VCS
metadata the Go toolchain stamps into the binary:

```
$ klaudia --version
2.1.66-klaudia (Klaudia) 2b25933 (2026-10-19)
```

That is why `.goreleaser.yaml` does **not** pass `-buildvcs=false`. Measured on
this repo: `-trimpath` and `-s -w` both preserve the stamp, while
`-buildvcs=false` removes it and `--version` silently degrades to the bare
constant — no error, no warning, and the one question `--version` exists to
answer ("does this binary have the fix?") stops being answerable. A shallow
`actions/checkout` breaks the same thing from the other end, which is why the
workflow sets `fetch-depth: 0`.

A binary built from a tree with uncommitted changes is marked `-dirty`.

An archive is named for its tag (`klaudia_0.1.0_darwin_arm64.tar.gz`) while the
binary inside names its commit. `git tag --contains <commit>` maps one to the
other. Injecting the tag through `-ldflags` was considered and left alone: the
point of `version.Build()` is that there is exactly one number worth trusting,
and a third one printed beside the other two works against that.

## Tagging from CI: one job, not two

The obvious shape — dispatch computes the tag, pushes it, and a separate
`on: push: tags` workflow publishes — does not work, and fails quietly.

[GitHub suppresses workflow runs for events raised with
`GITHUB_TOKEN`](https://docs.github.com/en/actions/security-for-github-actions/security-guides/automatic-token-authentication),
to stop a workflow triggering itself forever. `workflow_dispatch` and
`repository_dispatch` are the only exceptions. So a tag pushed by CI with the
default token lands in the repository and starts nothing: the tag is there, the
release is not, and there is no failed run to notice.

The fixes are a PAT or GitHub App token, or doing the work in one job. This
uses one job. A long-lived credential whose only purpose is to bounce from one
workflow run into another is a standing secret to rotate and leak, in exchange
for a job boundary nobody needed.

The `on: push: tags` trigger is still there, because a tag pushed from a
laptop is a normal user event and does trigger it.

## Branch protection

The dispatch path pushes a changelog commit straight to `main`. If `main` ever
requires pull requests, that push is rejected and the release stops before
anything is published — but only because the push is `--atomic`, and that is
load-bearing rather than tidiness.

`scripts/release.sh` creates the tag locally before pushing, so the push
carries two refs. A protected branch is declined per ref, so a plain
`git push origin HEAD vX.Y.Z` has the remote take the tag and refuse the
branch. Measured against a bare repo whose `update` hook declines
`refs/heads/main`: `* [new tag] v9.9.9 -> v9.9.9` alongside
`! [remote rejected] HEAD -> main`, exit status 1. The non-zero exit does stop
the script and skip the publish step, so no release is built — but the tag is
now on the remote, pointing at a commit that is not on `main`, and because a
tag pushed by CI starts no workflow run there is no failed run pointing at it
either. Someone has to notice and `git push --delete origin vX.Y.Z`. With
`--atomic` the remote rejects both refs or neither.

So the pipeline fails safe under branch protection, but it does fail: enabling
it needs either an exemption for `github-actions[bot]` or a change to open a
PR instead of pushing. Until then the atomic push is what keeps a rejection
from leaving debris.

## Platforms

darwin and linux, amd64 and arm64. All four come off one runner because the
project is pure Go and `CGO_ENABLED=0`.

Windows cross-compiles cleanly — verified for both amd64 and arm64, and
`internal/sandbox` carries a `!unix` fallback — but is not published. Nothing
here has been exercised on Windows: the Keychain credential path, the Chrome
discovery in `internal/browser`, and sandbox mode `os` are all
darwin/linux-shaped. Compiling is not the same as working, and shipping a
binary nobody has run invites bug reports against a platform with no
maintainer. Adding it later is two lines in `goos:`.

## Release notes

GoReleaser's generated changelog is disabled. `CHANGELOG.md` is written by hand
and explains reasoning rather than listing subjects, so a generated list beside
it would be a worse duplicate — and this repo's `area: lowercase sentence`
subjects are not conventional commits, so GoReleaser's grouping would file
everything under "Others" anyway.

The release body links `CHANGELOG.md` at the tag. To override for one release,
pass your own notes:

```bash
goreleaser release --release-notes=/path/to/notes.md
```

## Checks the pipeline owns

`ci.yml` triggers on `push: branches: [main]` and on pull requests. Neither a
tag push nor a dispatch matches, so `release.yml` runs `go vet` and
`go test ./internal/...` itself. Without that, the release build would be the
only build in the project that nothing gates. They run *before* the tag is
cut, so a failing test leaves no tag behind to clean up.
