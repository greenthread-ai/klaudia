#!/usr/bin/env bash
# Cut a Klaudia release: work out the next version, stamp the changelog,
# commit and tag.
#
# Publishing is NOT done here — GoReleaser does that, and the workflow runs it
# in the same job as this script. A tag pushed by CI with GITHUB_TOKEN does not
# start a second workflow run (GitHub suppresses it to prevent recursion), so
# splitting "tag" and "publish" across two workflows fails silently: the tag
# appears, the release never does.
#
# Usage:
#   scripts/release.sh [patch|minor|major] [--dry-run] [--no-push]
#
#   scripts/release.sh                 # minor bump, commit, tag, push
#   scripts/release.sh patch --dry-run # print what would happen, change nothing
#
# The version lives in the git tag, not in the source. internal/version.Version
# is a frozen constant mirroring the retired JS bundle for the parity harness;
# a release must not move it. See docs/releasing.md.
set -euo pipefail
# A CDPATH in the caller's environment makes `cd` print the directory it
# resolved, so ROOT="$(cd … && pwd)" captured the path twice and the script
# died on its first cd (reported: a release dry run from an interactive shell).
unset CDPATH

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHANGELOG="$ROOT/CHANGELOG.md"

BUMP="minor"
DRY_RUN=false
PUSH=true
for arg in "$@"; do
  case "$arg" in
    patch | minor | major) BUMP="$arg" ;;
    --dry-run) DRY_RUN=true ;;
    --no-push) PUSH=false ;;
    *)
      echo "usage: scripts/release.sh [patch|minor|major] [--dry-run] [--no-push]" >&2
      exit 2
      ;;
  esac
done

die() {
  echo "release: $1" >&2
  exit 1
}

cd "$ROOT"

# A dirty tree would be swept into the changelog commit, and GoReleaser marks
# the binaries -dirty, so the published artefact would not match any commit.
if [[ -n "$(git status --porcelain)" ]] && [[ "$DRY_RUN" == false ]]; then
  die "working tree is dirty; commit or stash first"
fi

# Latest reachable v-tag. The repo starts with none, so the first release is
# v0.1.0 rather than an error.
PREV="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)"
if [[ -z "$PREV" ]]; then
  MAJOR=0 MINOR=1 PATCH=0
  PREV="(none)"
else
  [[ "$PREV" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] ||
    die "latest tag '$PREV' is not vMAJOR.MINOR.PATCH; tag by hand this once"
  MAJOR="${BASH_REMATCH[1]}" MINOR="${BASH_REMATCH[2]}" PATCH="${BASH_REMATCH[3]}"
  case "$BUMP" in
    major) MAJOR=$((MAJOR + 1)) MINOR=0 PATCH=0 ;;
    minor) MINOR=$((MINOR + 1)) PATCH=0 ;;
    patch) PATCH=$((PATCH + 1)) ;;
  esac
fi
VERSION="v${MAJOR}.${MINOR}.${PATCH}"

git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null && die "tag $VERSION already exists"

grep -q '^## Unreleased$' "$CHANGELOG" || die "no '## Unreleased' heading in CHANGELOG.md"

# Refuse to publish an empty section. Releasing nothing is always a mistake,
# and it is easiest to make right after the previous release.
BODY="$(awk '/^## Unreleased$/{f=1;next} /^## /{f=0} f' "$CHANGELOG" | tr -d '[:space:]')"
[[ -n "$BODY" ]] || die "the Unreleased section is empty; nothing to release"

DATE="$(date -u +%Y-%m-%d)"
echo "release: $PREV -> $VERSION ($BUMP, $DATE)"

if [[ "$DRY_RUN" == true ]]; then
  echo "release: dry run, stopping before any change"
  exit 0
fi

# Stamp the section and open a fresh one for the next cycle.
awk -v v="$VERSION" -v d="$DATE" '
  /^## Unreleased$/ && !done { print "## Unreleased"; print ""; print "## " v " — " d; done=1; next }
  { print }
' "$CHANGELOG" >"$CHANGELOG.tmp" && mv "$CHANGELOG.tmp" "$CHANGELOG"

git add "$CHANGELOG"
git commit -q -m "changelog: cut $VERSION

Released by scripts/release.sh. The version is the tag; internal/version
stays frozen and the binary reports its commit."
git tag -a "$VERSION" -m "$VERSION"

if [[ "$PUSH" == true ]]; then
  # --atomic or the two refs go up independently. Measured against a bare repo
  # with an `update` hook declining refs/heads/main, which is how a protected
  # branch rejects per ref: the plain push reports "[new tag] v9.9.9" and
  # "[remote rejected] HEAD -> main", exits 1, and leaves the tag on the
  # remote with its changelog commit nowhere. A tag pushed by CI starts no
  # workflow run, so that state is a tag with no release and no failed run
  # pointing at it. Atomic makes the remote reject both or neither.
  git push --atomic origin HEAD "$VERSION"
  echo "release: pushed $VERSION"
else
  echo "release: created $VERSION locally (not pushed)"
fi
