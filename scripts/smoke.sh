#!/usr/bin/env bash
# Klaudia end-to-end smoke test.
#
# Builds the binary and exercises the real agent loop across modes and the
# client-side tools (Bash/Write/Read/Glob/Grep), then verifies the session
# resume path and plan-mode read-only enforcement against a live model.
#
# Requires a working credential (ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN, or a
# Claude Code OAuth session in the macOS Keychain). Uses the cheap `haiku` model.
#
# Usage: scripts/smoke.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$(mktemp -d)/klaudia"
WORK="$(mktemp -d)"
COMMON=(--dangerously-skip-permissions --max-turns 6 --model haiku)
trap 'rm -rf "$WORK" "$(dirname "$BIN")"' EXIT

pass() { printf '  \033[32m✓\033[0m %s\n' "$1"; }
fail() { printf '  \033[31m✗ %s\033[0m\n' "$1"; exit 1; }

echo "== build =="
CGO_ENABLED=0 go build -o "$BIN" "$ROOT/cmd/klaudia"
"$BIN" --version | grep -q klaudia && pass "binary builds and reports version" || fail "version"

echo "== unit tests =="
(cd "$ROOT" && go test ./internal/... -count=1 >/dev/null) && pass "go test ./internal/..." || fail "unit tests"

echo "== headless: client-side tools (Write/Read/Glob/Grep) =="
cd "$WORK"
OUT="$WORK/out.jsonl"
"$BIN" -p "With tools, no commentary: 1) Write $WORK/a.txt containing apple. 2) Read it. 3) Glob *.txt here. 4) Grep apple here. Then reply done." \
  "${COMMON[@]}" --output-format stream-json --verbose >"$OUT" 2>&1 || true
[ "$(cat "$WORK/a.txt" 2>/dev/null)" = "apple" ] && pass "Write+Read created the file" || fail "Write/Read"
for tool in Write Read Glob Grep; do
  grep -q "\"name\":\"$tool\"" "$OUT" && pass "$tool invoked" || fail "$tool not invoked"
done
grep -q '"is_error":true' "$OUT" && fail "a tool returned is_error" || pass "no tool errors"
grep -q '"type":"result".*"is_error":false' "$OUT" && pass "well-formed result envelope" || fail "result envelope"

echo "== resume round-trip =="
cd "$WORK"
"$BIN" -p "Remember this secret word for later: PINEAPPLE. Reply ok." "${COMMON[@]}" >/dev/null 2>&1
ANSWER="$("$BIN" -p "What was the secret word? Reply with ONLY that word." --continue "${COMMON[@]}" 2>/dev/null | tr -d '[:space:]')"
[ "$ANSWER" = "PINEAPPLE" ] && pass "--continue recalled prior context" || fail "resume lost context (got: $ANSWER)"

echo "== plan mode is read-only =="
cd "$WORK"
"$BIN" -p "Create nope.txt here containing x using the Write tool." --permission-mode plan --max-turns 3 --model haiku >/dev/null 2>&1 || true
[ ! -f "$WORK/nope.txt" ] && pass "plan mode blocked the write" || fail "plan mode allowed a write"

echo
echo "All smoke checks passed."
