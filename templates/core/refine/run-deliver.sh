#!/usr/bin/env bash
# forge refine — run-deliver: execute one deliver run and collect measurements
# Usage: run-deliver.sh <task-id> <task-description> <fixture-dir> <run-dir>
# Produces: run-dir/ with timing, diff, exit codes, and placeholders for LLM judge
set -euo pipefail

TASK_ID="${1:?Usage: run-deliver.sh <task-id> <task-description> <fixture-dir> <run-dir>}"
TASK_DESC="${2:?}"
FIXTURE_DIR="${3:?}"
RUN_DIR="${4:?}"

mkdir -p "$RUN_DIR"

cd "$FIXTURE_DIR"

# Ensure clean state
git checkout . 2>/dev/null || true
git clean -fd 2>/dev/null || true

# --- Run pipeline ---
START=$(date +%s)

# The deliver pipeline is invoked via Claude Code / forge run.
# For measurement, we simulate by running the orchestrator directly.
if [[ -f .forge/pipeline/orchestrator.sh ]]; then
  bash .forge/pipeline/orchestrator.sh "$TASK_ID" "$TASK_DESC" \
    > "${RUN_DIR}/stdout.log" 2> "${RUN_DIR}/stderr.log"
  echo $? > "${RUN_DIR}/exit_code"
else
  echo "1" > "${RUN_DIR}/exit_code"
  echo "orchestrator.sh not found" > "${RUN_DIR}/stderr.log"
fi

END=$(date +%s)
DURATION=$((END - START))
jq -n --argjson d "$DURATION" '{duration_seconds: $d}' > "${RUN_DIR}/timing.json"

# --- Collect artifacts ---

# Diff
git diff > "${RUN_DIR}/diff.patch" 2>/dev/null || true
git diff --name-only > "${RUN_DIR}/changed_files.txt" 2>/dev/null || true

# Typecheck
npm run check --prefix . > /dev/null 2>&1
echo $? > "${RUN_DIR}/typecheck_exit_code"

# Tests
npm test --prefix . > /dev/null 2>&1
echo $? > "${RUN_DIR}/test_exit_code"

# Store task description for LLM judge
echo "$TASK_DESC" > "${RUN_DIR}/task_description.txt"

# Placeholders for LLM judge (caller fills these in)
echo "0" > "${RUN_DIR}/llm_correct"
echo "0" > "${RUN_DIR}/llm_scoped"

echo "Deliver run complete: task=${TASK_ID} exit=$(cat "${RUN_DIR}/exit_code") duration=${DURATION}s"
