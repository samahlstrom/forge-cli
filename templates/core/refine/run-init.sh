#!/usr/bin/env bash
# forge refine — run-init: execute one init run and collect measurements
# Usage: run-init.sh <fixture-id> <fixture-dir> <forge-binary> <run-dir>
# Produces: run-dir/ with exit_code, timing.json, expected_commands.json, idempotent
set -euo pipefail

FIXTURE_ID="${1:?Usage: run-init.sh <fixture-id> <fixture-dir> <forge-binary> <run-dir>}"
FIXTURE_DIR="${2:?}"
FORGE_BIN="${3:?}"
RUN_DIR="${4:?}"

mkdir -p "$RUN_DIR"

# --- Run 1: forge init ---
START=$(date +%s)

cd "$FIXTURE_DIR"

# Init git if needed
if [[ ! -d .git ]]; then
  git init -q
  git add -A
  git commit -q -m "fixture baseline" 2>/dev/null || true
fi

"$FORGE_BIN" init --yes > "${RUN_DIR}/stdout.log" 2> "${RUN_DIR}/stderr.log"
echo $? > "${RUN_DIR}/exit_code"

END=$(date +%s)
DURATION=$((END - START))
jq -n --argjson d "$DURATION" '{duration_seconds: $d}' > "${RUN_DIR}/timing.json"

# --- Snapshot for idempotency check ---
find . -not -path './.git/*' -not -path './node_modules/*' -type f | sort | \
  xargs md5sum 2>/dev/null > "${RUN_DIR}/snapshot1.md5"

# --- Run 2: forge init again (idempotency) ---
"$FORGE_BIN" init --yes > /dev/null 2>&1 || true

find . -not -path './.git/*' -not -path './node_modules/*' -type f | sort | \
  xargs md5sum 2>/dev/null > "${RUN_DIR}/snapshot2.md5"

if diff -q "${RUN_DIR}/snapshot1.md5" "${RUN_DIR}/snapshot2.md5" > /dev/null 2>&1; then
  echo "1" > "${RUN_DIR}/idempotent"
else
  echo "0" > "${RUN_DIR}/idempotent"
  diff "${RUN_DIR}/snapshot1.md5" "${RUN_DIR}/snapshot2.md5" > "${RUN_DIR}/idempotent_diff.log" 2>&1 || true
fi

echo "Init run complete: fixture=${FIXTURE_ID} exit=$(cat "${RUN_DIR}/exit_code") duration=${DURATION}s idempotent=$(cat "${RUN_DIR}/idempotent")"
