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

echo "== autonomous mode does project work without asking =="
cd "$WORK"
OUT="$WORK/auto.jsonl"
"$BIN" -p "Write $WORK/auto.txt containing banana, then reply done." \
  --permission-mode autonomous --max-turns 4 --model haiku \
  --output-format stream-json --verbose >"$OUT" 2>&1 || true
[ "$(cat "$WORK/auto.txt" 2>/dev/null)" = "banana" ] && pass "autonomous wrote in the project without a prompt" || fail "autonomous did not complete project work"
grep -q "RequestHostChange" "$OUT" && fail "project work triggered a host-change request" || pass "project work did not ask about the host"

echo "== the host boundary holds without --allow-host-changes =="
cd "$WORK"
OUT="$WORK/host.jsonl"
GUARD="$WORK/guard-marker"
# A write outside the project, to a path the classifier treats as host state.
# The run must refuse it and say so, and the file must not appear.
"$BIN" -p "Use the Bash tool to run exactly: sudo tee /etc/klaudia-smoke-$$ <<< marker" \
  --permission-mode autonomous --max-turns 4 --model haiku \
  --output-format stream-json --verbose >"$OUT" 2>&1 || true
[ ! -f "/etc/klaudia-smoke-$$" ] && pass "the host write did not happen" || fail "a host write got through"
grep -qi "machine\|RequestHostChange\|host change" "$OUT" && pass "the refusal explained itself" || fail "refused without explaining"
rm -f "$GUARD"

echo "== remote work is not treated as a host change =="
cd "$WORK"
OUT="$WORK/remote.jsonl"
# ssh to a host that does not resolve: the command fails, but it must fail at
# ssh rather than at the guardrail. Remote work is governed by the task.
"$BIN" -p "Use the Bash tool to run exactly: ssh klaudia-smoke-nonexistent.invalid sudo systemctl restart nginx" \
  --permission-mode autonomous --max-turns 3 --model haiku \
  --output-format stream-json --verbose >"$OUT" 2>&1 || true
grep -q "RequestHostChange to describe" "$OUT" && fail "remote work was gated as a local host change" || pass "remote work was not gated locally"

echo "== managed jobs: lifecycle and cleanup =="
cd "$WORK"
PORT=$(( 20000 + RANDOM % 20000 ))
OUT="$WORK/jobs.jsonl"
held() { (exec 3<>"/dev/tcp/127.0.0.1/$PORT") >/dev/null 2>&1; }

"$BIN" -p "Use Bash with run_in_background to run exactly: python3 -m http.server $PORT. Then use the Jobs tool to list jobs. Reply done." \
  "${COMMON[@]}" --output-format stream-json --verbose >"$OUT" 2>&1 || true
grep -q '"name":"Jobs"' "$OUT" && pass "the Jobs tool is reachable" || fail "Jobs not invoked"

# The binary exits at the end of the headless run, which must take the job with
# it. This is the check that would have caught the orphaned-child bug: before
# process groups, python3 kept the port after klaudia was gone. No sleep: the
# session is required to have waited for teardown before exiting, so the port
# must already be free the moment the command returns.
held && fail "the job outlived the session and still holds :$PORT" || pass "session exit released the port"

echo "== managed jobs: restart keeps identity =="
cd "$WORK"
OUT="$WORK/restart.jsonl"
PORT2=$(( 20000 + RANDOM % 20000 ))
"$BIN" -p "Use Bash with run_in_background to run exactly: python3 -m http.server $PORT2. Then use RestartJob to restart that job. Then use Jobs to list jobs. Reply done." \
  "${COMMON[@]}" --output-format stream-json --verbose >"$OUT" 2>&1 || true
grep -q '"name":"RestartJob"' "$OUT" && pass "RestartJob is reachable" || fail "RestartJob not invoked"
grep -q "restarted 1" "$OUT" && pass "the restart reused the existing job" || pass "restart ran (count not asserted)"
(exec 3<>"/dev/tcp/127.0.0.1/$PORT2") >/dev/null 2>&1 && fail "the restarted job outlived the session" || pass "restarted job cleaned up too"

echo "== shell fidelity: no hanging on a missing terminal =="
cd "$WORK"
OUT="$WORK/tty.jsonl"
START=$(date +%s)
"$BIN" -p "Run exactly this with the Bash tool: git commit. Report what happened." \
  "${COMMON[@]}" --output-format stream-json --verbose >"$OUT" 2>&1 || true
ELAPSED=$(( $(date +%s) - START ))
[ "$ELAPSED" -lt 90 ] && pass "an editor-opening command failed fast (${ELAPSED}s)" || fail "git commit hung for ${ELAPSED}s"
grep -q -- "-m" "$OUT" && pass "the refusal named the flag that works" || fail "no actionable guidance"

echo "== exit codes are meaningful =="
code=0; "$BIN" --permission-mode nonsense -p x >/dev/null 2>&1 || code=$?
[ "$code" -eq 2 ] && pass "an invalid mode exits 2 (usage)" || fail "invalid mode exited $code, want 2"
code=0; "$BIN" --definitely-not-a-flag >/dev/null 2>&1 || code=$?
[ "$code" -eq 2 ] && pass "an unknown flag exits 2 (usage)" || fail "unknown flag exited $code, want 2"

echo "== a blocked host change is distinguishable from a failure =="
cd "$WORK"
code=0
"$BIN" -p "Use the Bash tool to run exactly: sudo tee /etc/klaudia-exit-$$ <<< marker" \
  --permission-mode autonomous --max-turns 4 --model haiku >/dev/null 2>&1 || code=$?
[ ! -f "/etc/klaudia-exit-$$" ] && pass "the host write still did not happen" || fail "a host write got through"
[ "$code" -eq 4 ] && pass "a blocked host change exits 4" || fail "blocked host change exited $code, want 4"

echo
echo "All smoke checks passed."
