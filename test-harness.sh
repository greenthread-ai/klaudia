#!/usr/bin/env bash
set -euo pipefail

# Klaudia test harness — runs klaudia with nest-detection bypass
# Uses existing Anthropic auth (API key or OAuth token)

KLAUDIA="node src/cli.js"
export CLAUDECODE=  # bypass nest detection

echo "=== Klaudia Test Harness ==="
echo ""

# Test 1: Version
echo "[1/5] Version check..."
VERSION=$($KLAUDIA --version 2>&1)
echo "  Got: $VERSION"
echo ""

# Test 2: Simple prompt (non-interactive)
echo "[2/5] Simple prompt..."
RESPONSE=$($KLAUDIA --print "Respond with exactly: KLAUDIA_OK" 2>&1)
if echo "$RESPONSE" | grep -q "KLAUDIA_OK"; then
  echo "  PASS: Got expected response"
else
  echo "  FAIL: Expected KLAUDIA_OK, got: $RESPONSE"
fi
echo ""

# Test 3: Model selection
echo "[3/5] Model override..."
RESPONSE=$($KLAUDIA --print --model claude-haiku-4-5 "What model are you? Reply with just your model ID." 2>&1)
echo "  Response: $RESPONSE"
echo ""

# Test 4: JSON output format
echo "[4/5] JSON output..."
RESPONSE=$($KLAUDIA --print --output-format json "Say hi" 2>&1)
if echo "$RESPONSE" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
  echo "  PASS: Valid JSON output"
else
  echo "  FAIL: Invalid JSON: $RESPONSE"
fi
echo ""

# Test 5: Help (no API call)
echo "[5/5] Help output..."
HELP=$($KLAUDIA --help 2>&1 | head -3)
echo "  $HELP"
echo ""

echo "=== Done ==="
