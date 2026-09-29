#!/usr/bin/env bash
# Runs the unit tests against a poisoned HOME.
#
# Klaudia reads a lot from $HOME: ~/.klaudia/config.toml, ~/.claude/CLAUDE.md,
# skills under ~/.claude/skills and ~/.klaudia/skills, ~/.claude.json. A unit
# test that forgets to pin HOME passes or fails depending on whose machine it
# runs on — TestLoadDirWarnsOnSkillDirWithoutDefinition did exactly that, failing
# only for a developer with a skill directory lacking SKILL.md.
#
# An empty HOME would hide that class of bug, since an empty directory looks
# like the tests' expectations. So this fills HOME with plausible, conflicting
# state instead: any test that reads it without isolating itself sees a skill,
# an instruction file, a config and an MCP server it did not ask for, and fails.
#
# Usage: scripts/hermetic.sh [go test flags...]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POISON="$(mktemp -d)"
trap 'rm -rf "$POISON"' EXIT

mkdir -p "$POISON/.claude/skills/poison-skill" "$POISON/.claude/skills/no-definition" \
         "$POISON/.klaudia/skills/poison-klaudia-skill"
cat >"$POISON/.claude/skills/poison-skill/SKILL.md" <<'EOF'
---
name: poison-skill
description: HERMETIC-POISON — a unit test read the real HOME
---
HERMETIC-POISON
EOF
cp "$POISON/.claude/skills/poison-skill/SKILL.md" "$POISON/.klaudia/skills/poison-klaudia-skill/SKILL.md"
sed -i.bak 's/poison-skill/poison-klaudia-skill/' "$POISON/.klaudia/skills/poison-klaudia-skill/SKILL.md"
echo "HERMETIC-POISON: a unit test read ~/.claude/CLAUDE.md" >"$POISON/.claude/CLAUDE.md"
cat >"$POISON/.klaudia/config.toml" <<'EOF'
model = "hermetic-poison-model"
EOF
echo '{"mcpServers":{"hermetic-poison":{"command":"/nonexistent/hermetic-poison"}}}' >"$POISON/.claude.json"

# The Go caches normally live under HOME; pin them to the real ones so the run
# doesn't rebuild the world into the poisoned directory.
cd "$ROOT"
export GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)" GOPATH="$(go env GOPATH)"
export HOME="$POISON" KLAUDIA_CONFIG_DIR="$POISON/.klaudia"
unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN KLAUDIA_CUSTOM_ENDPOINT
exec go test "$@" ./internal/...
