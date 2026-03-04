#!/usr/bin/env bash
set -euo pipefail

# Klaudia test harness — runs klaudia with nest-detection bypass
# Uses existing Anthropic auth (API key or OAuth token)

KLAUDIA="node dist/cli.js"
export CLAUDECODE=  # bypass nest detection

echo "=== Klaudia Test Harness ==="
echo ""

# Test 6 is web search (at bottom, optional/slow)
TOTAL=5
if [[ "${1:-}" == "--with-websearch" ]]; then
  TOTAL=6
fi

# Test 1: Version
echo "[1/$TOTAL] Version check..."
VERSION=$($KLAUDIA --version 2>&1)
echo "  Got: $VERSION"
echo ""

# Test 2: Simple prompt (non-interactive)
echo "[2/$TOTAL] Simple prompt..."
RESPONSE=$($KLAUDIA --print "Respond with exactly: KLAUDIA_OK" 2>&1)
if echo "$RESPONSE" | grep -q "KLAUDIA_OK"; then
  echo "  PASS: Got expected response"
else
  echo "  FAIL: Expected KLAUDIA_OK, got: $RESPONSE"
fi
echo ""

# Test 3: Model selection
echo "[3/$TOTAL] Model override..."
RESPONSE=$($KLAUDIA --print --model claude-haiku-4-5 "What model are you? Reply with just your model ID." 2>&1)
echo "  Response: $RESPONSE"
echo ""

# Test 4: JSON output format
echo "[4/$TOTAL] JSON output..."
RESPONSE=$($KLAUDIA --print --output-format json "Say hi" 2>&1)
if echo "$RESPONSE" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
  echo "  PASS: Valid JSON output"
else
  echo "  FAIL: Invalid JSON: $RESPONSE"
fi
echo ""

# Test 5: Help (no API call)
echo "[5/$TOTAL] Help output..."
HELP=$($KLAUDIA --help 2>&1 | head -3)
echo "  $HELP"
echo ""

# Test 6: Web search (optional, needs API call with tool use)
if [[ "${1:-}" == "--with-websearch" ]]; then
  echo "[6/$TOTAL] Web search..."
  RESPONSE=$($KLAUDIA --print "Use web search to find what year Anthropic was founded. Reply with ONLY the year, nothing else." 2>&1)
  if echo "$RESPONSE" | grep -q "2021"; then
    echo "  PASS: Web search returned correct answer (2021)"
  else
    echo "  RESULT: $RESPONSE"
    echo "  (Expected '2021' — if model answered from training data, web search may not be enabled)"
  fi
  echo ""
fi

echo "=== Done ==="
