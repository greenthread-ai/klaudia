# Releasing

Klaudia releases are built by [GoReleaser](https://goreleaser.com) from a tag.
`.goreleaser.yaml` holds the build matrix; `.github/workflows/release.yml` runs
it. This file records why the pipeline is shaped the way it is.

## Cutting a release

```bash
git switch main && git pull                  # release from what origin has
# edit CHANGELOG.md: rename "## Unreleased" to "## v0.2.0 — 2026-10-19"
git commit -am "changelog: cut v0.2.0"
git tag -a v0.2.0 -m "v0.2.0"
git push origin main v0.2.0
```

The tag push triggers the workflow: vet, test, then build and publish. Nothing
else is manual — no version constant to bump (see below).

Dry-run the whole thing without a tag, writing to `dist/` (git-ignored):

```bash
goreleaser check                                  # validate the config
goreleaser release --snapshot --clean --skip=publish
```

## What the version reports, and why nothing is bumped

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

`ci.yml` triggers on `push: branches: [main]` and on pull requests. A tag push
matches neither, so `release.yml` runs `go vet` and `go test ./internal/...`
itself. Without that, the release build would be the only build in the project
that nothing gates.
