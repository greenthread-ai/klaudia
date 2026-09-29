#!/usr/bin/env bash
# Statement coverage across the hermetic test layers.
#
# Unit coverage alone undercounts: internal/cli is mostly exercised through the
# binary, which only the e2e suite runs. So this measures both — the unit tests
# with -coverpkg across the module, the e2e suite with a -cover build of the
# binary writing to GOCOVERDIR — merges them, and prints a per-package table
# with a column for each layer and the combined figure.
#
# Output: coverage/all.out (merged profile; `go tool cover -html=coverage/all.out`
# to browse it) plus the unit and e2e profiles beside it.
#
# Usage: scripts/cover.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/coverage"
cd "$ROOT"
rm -rf "$OUT"; mkdir -p "$OUT/e2e-raw"

# internal/fakeapi is test support; counting it would inflate the figure.
PKGS="$(go list ./internal/... | grep -v /internal/fakeapi | paste -sd, -)"
go test -count=1 -coverpkg="$PKGS" -coverprofile="$OUT/unit.out" ./internal/... >/dev/null
KLAUDIA_E2E_COVERDIR="$OUT/e2e-raw" go test -count=1 ./e2e/... >/dev/null
go tool covdata textfmt -i="$OUT/e2e-raw" -o="$OUT/e2e.out"

python3 scripts/covtable.py "$OUT/all.out" unit="$OUT/unit.out" e2e="$OUT/e2e.out"
