#!/usr/bin/env bash
set -euo pipefail

# Klaudia differential test harness.
#
# Runs the same invocation through the JS reference (node dist/cli.js) and the
# Go port, then diffs normalized output. The JS app is the golden oracle while
# the Go port is built phase by phase.
#
# Usage:
#   bash test-diff.sh              # no-API checks (version, help) — safe, free
#   bash test-diff.sh --with-api   # also diff a real -p stream-json run (costs tokens)
#
# Env:
#   JS="node dist/cli.js"          # override the reference command
#   GO=/tmp/klaudia                # override the Go binary path

JS=${JS:-"node dist/cli.js"}
GO=${GO:-/tmp/klaudia}
export CLAUDECODE=  # bypass nested-session detection in the JS app

PASS=0
FAIL=0

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

build_go() {
  echo "Building Go binary -> $GO"
  CGO_ENABLED=0 go build -o "$GO" ./cmd/klaudia
}

# Strip volatile fields so two semantically-equal runs compare equal:
# UUIDs, session ids, timings, cost, and usage counters.
normalize() {
  python3 - "$@" <<'PY'
import sys, json, re
def scrub(o):
    if isinstance(o, dict):
        for k in list(o):
            if k in ("uuid","session_id","total_cost_usd","usage","modelUsage",
                     "duration_ms","duration_api_ms","fast_mode_state"):
                o[k] = None
            else:
                scrub(o[k])
    elif isinstance(o, list):
        for v in o: scrub(v)
    return o
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    try:
        print(json.dumps(scrub(json.loads(line)), sort_keys=True))
    except json.JSONDecodeError:
        # Non-JSON line (text mode): emit as-is for textual diff.
        print(line)
PY
}

echo "=== Klaudia Differential Harness ==="
build_go
echo ""

# --- Check 1: --version (no API) ---
echo "[version]"
js_v=$($JS --version 2>&1 | head -1)
go_v=$($GO --version 2>&1 | head -1)
if [[ "$js_v" == "$go_v" ]]; then
  pass "--version matches ($go_v)"
else
  fail "--version: JS='$js_v' GO='$go_v'"
fi
echo ""

# --- Check 2: --help is non-empty and exits 0 (shape differs by design) ---
echo "[help]"
if $GO --help >/dev/null 2>&1; then
  pass "--help exits 0"
else
  fail "--help non-zero exit"
fi
echo ""

# --- Check 3 (opt-in): real -p stream-json diff ---
if [[ "${1:-}" == "--with-api" ]]; then
  echo "[stream-json diff] (real API call)"
  prompt="Respond with exactly: KLAUDIA_OK"
  js_out=$($JS -p "$prompt" --output-format stream-json --verbose 2>/dev/null | normalize)
  go_out=$($GO -p "$prompt" --output-format stream-json --verbose 2>/dev/null | normalize)
  if diff <(echo "$js_out") <(echo "$go_out") >/tmp/klaudia-diff.txt; then
    pass "stream-json event streams match"
  else
    fail "stream-json diff (see /tmp/klaudia-diff.txt)"
    head -40 /tmp/klaudia-diff.txt
  fi
  echo ""
fi

echo "=== $PASS passed, $FAIL failed ==="
[[ $FAIL -eq 0 ]]
