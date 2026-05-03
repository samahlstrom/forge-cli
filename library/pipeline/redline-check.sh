#!/usr/bin/env bash
# forge pipeline — redline-check: verify a Wave-0 redline test transitioned red→green
# Doctrine: <library_dir>/doctrine/tdd.md § Red-Green-Refactor Discipline
#
# Strategy:
#   1. Read the Wave-0 quality report at <runs_dir>/<task_id>/reports/wave0-quality.json
#   2. Confirm the redline artifact exists and is non-empty
#   3. Identify the test file(s) the quality agent created or modified
#   4. Run those test file(s) against the parent commit (in a worktree) and confirm at least one fails
#   5. Run those test file(s) against HEAD and confirm all pass
# Exit code 0 = redline verified, 1 = check failed
set -euo pipefail

TASK_ID="${1:-}"
if [[ -z "$TASK_ID" ]]; then
  echo "Usage: redline-check.sh <task-id>" >&2
  exit 1
fi

# Resolve runs dir from forge paths if available, else fall back to ~/.forge/runs
if command -v forge >/dev/null 2>&1; then
  RUNS_DIR="$(forge paths 2>/dev/null | jq -r '.runs_dir // empty')"
fi
RUNS_DIR="${RUNS_DIR:-${FORGE_GLOBAL:-$HOME/.forge}/runs}"

REPORT_FILE="${RUNS_DIR}/${TASK_ID}/reports/wave0-quality.json"
ARTIFACT_FILE="${RUNS_DIR}/${TASK_ID}/redline-wave0.txt"

if [[ ! -f "$REPORT_FILE" ]]; then
  echo "redline: missing Wave-0 quality report at ${REPORT_FILE}" >&2
  echo "redline: did the pipeline run Step 4.5 (Wave 0)?" >&2
  exit 1
fi

if [[ ! -s "$ARTIFACT_FILE" ]]; then
  echo "redline: missing or empty redline artifact at ${ARTIFACT_FILE}" >&2
  echo "redline: the Wave-0 quality agent must capture the failing test output" >&2
  exit 1
fi

# Extract test files authored by the Wave-0 quality agent
mapfile -t TEST_FILES < <(
  jq -r '(.files_created // []) + (.files_modified // []) | .[]' "$REPORT_FILE" 2>/dev/null \
    | grep -E '(\.test\.|\.spec\.|_test\.go$|/tests/)' \
    || true
)

if [[ "${#TEST_FILES[@]}" -eq 0 ]]; then
  echo "redline: Wave-0 report lists no test files (files_created/files_modified)" >&2
  exit 1
fi

# Read the project's test command from forge.yaml
CMD_TEST="${CMD_TEST:-}"
if [[ -z "$CMD_TEST" && -f "forge.yaml" ]]; then
  CMD_TEST=$(sed -n '/^commands:/,/^[^ ]/p' forge.yaml | grep '^  test:' | head -1 | sed 's/^[^:]*: *//' | sed 's/^ *"//' | sed 's/" *$//')
fi
CMD_TEST="${CMD_TEST:-npx vitest run}"

# Verify GREEN at HEAD: run the test files at the current commit, expect pass
GREEN_LOG=$(mktemp)
GREEN_EXIT=0
eval "$CMD_TEST ${TEST_FILES[*]}" >"$GREEN_LOG" 2>&1 || GREEN_EXIT=$?
if [[ $GREEN_EXIT -ne 0 ]]; then
  echo "redline: tests FAIL at HEAD — implementation does not satisfy the redline test" >&2
  tail -30 "$GREEN_LOG" >&2
  rm -f "$GREEN_LOG"
  exit 1
fi
rm -f "$GREEN_LOG"

# Verify RED at parent: stash the test files, check out parent into a worktree, run tests
PARENT_SHA=$(git rev-parse HEAD~1 2>/dev/null || echo "")
if [[ -z "$PARENT_SHA" ]]; then
  echo "redline: cannot determine parent commit (HEAD~1) — skipping parent-side check" >&2
  echo "redline: HEAD-side tests pass; treating as best-effort verified" >&2
  exit 0
fi

WORKTREE_DIR=$(mktemp -d -t forge-redline-XXXXXX)
trap 'git worktree remove --force "$WORKTREE_DIR" >/dev/null 2>&1 || true; rm -rf "$WORKTREE_DIR"' EXIT

if ! git worktree add --detach "$WORKTREE_DIR" "$PARENT_SHA" >/dev/null 2>&1; then
  echo "redline: failed to create worktree at parent commit" >&2
  exit 1
fi

# Copy the new test files into the parent worktree (they did not exist there)
for tf in "${TEST_FILES[@]}"; do
  target="${WORKTREE_DIR}/${tf}"
  mkdir -p "$(dirname "$target")"
  cp "$tf" "$target"
done

RED_LOG=$(mktemp)
RED_EXIT=0
( cd "$WORKTREE_DIR" && eval "$CMD_TEST ${TEST_FILES[*]}" ) >"$RED_LOG" 2>&1 || RED_EXIT=$?

if [[ $RED_EXIT -eq 0 ]]; then
  echo "redline: tests PASS at parent commit — they do not constrain new behavior" >&2
  echo "redline: a behavior test must fail without the new implementation" >&2
  tail -30 "$RED_LOG" >&2
  rm -f "$RED_LOG"
  exit 1
fi

rm -f "$RED_LOG"
echo "redline: verified — ${#TEST_FILES[@]} test file(s) red at parent, green at HEAD" >&2
exit 0
