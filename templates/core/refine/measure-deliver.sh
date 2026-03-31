#!/usr/bin/env bash
# Measure /forge quality by running a task against isomed-caregiver
# Outputs JSON metrics for forge refine
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
ISOMED_DIR="/Users/samahlstrom/Documents/GitHub/isomed-caregiver"

if [[ ! -d "$ISOMED_DIR" ]]; then
  echo '{"deliver_score": 0, "tests_pass": 0, "ts_errors_fixed": 0, "diff_scoped": 0}'
  exit 0
fi

# ── Snapshot baseline ──────────────────────────────────────────
cd "$ISOMED_DIR"

# Count TS errors before
TS_ERRORS_BEFORE=$(npx tsc --noEmit 2>&1 | grep "error TS" | wc -l | tr -d ' ')
TESTS_FAILING_BEFORE=$(npx vitest run 2>&1 | grep "Tests" | sed 's/.*\([0-9]*\) failed.*/\1/' | head -1)
TESTS_FAILING_BEFORE="${TESTS_FAILING_BEFORE:-36}"

# ── Clean state ────────────────────────────────────────────────
git stash -q 2>/dev/null || true

# ── Pick the task ──────────────────────────────────────────────
# Rotate through tasks to test different scenarios
TASKS=(
  "Fix the TypeScript type errors in src/auth/auth.controller.ts. The login method returns a union type and the code accesses .tokens without narrowing. Add a type guard or conditional check before accessing tokens, and fix the cookie-setting lines that follow."
  "Fix the TypeScript error in src/admin/admin.controller.ts line 228. The upsertSetting call is missing the required settingValueJson field — it should not be optional in the argument. Add a default value or make the field required in the input validation."
  "Fix the TypeScript error in src/auth/mfa/mfa.schema.ts line 35. The uniqueIndex argument type is wrong — it expects SQL but gets ExtraConfigColumn. Use the correct drizzle-orm API for defining a unique composite index on user_id and method columns."
)

# Use iteration count to rotate tasks (read from env or default)
ITER="${FORGE_REFINE_ITER:-0}"
TASK_IDX=$((ITER % ${#TASKS[@]}))
TASK="${TASKS[$TASK_IDX]}"

# ── Run /forge via claude -p ───────────────────────────────────
PROMPT="You are working in: $ISOMED_DIR

This is a TypeScript backend project using Drizzle ORM, jose for JWT, bcryptjs for hashing.

YOUR TASK:
$TASK

RULES:
1. Read the file(s) mentioned in the task first
2. Make the minimum change needed to fix the issue
3. Do NOT refactor surrounding code
4. Do NOT modify unrelated files
5. After fixing, run: npx tsc --noEmit 2>&1 | head -20
6. If your fix introduced new errors, fix those too
7. Keep changes minimal and scoped"

PROMPT_FILE=$(mktemp)
echo "$PROMPT" > "$PROMPT_FILE"

echo "  Measuring: running claude -p on task $((TASK_IDX + 1))/${#TASKS[@]}..." >&2

cat "$PROMPT_FILE" | claude -p \
  --dangerously-skip-permissions \
  --model claude-sonnet-4-6 \
  > /dev/null 2>&1 || true

rm -f "$PROMPT_FILE"

# ── Measure results ───────────────────────────────────────────

# 1. TS errors after
TS_ERRORS_AFTER=$(npx tsc --noEmit 2>&1 | grep "error TS" | wc -l | tr -d ' ')

# 2. Tests after
TEST_OUTPUT=$(npx vitest run 2>&1)
TESTS_FAILING_AFTER=$(echo "$TEST_OUTPUT" | grep -oE '[0-9]+ failed' | grep -oE '[0-9]+' | head -1)
TESTS_FAILING_AFTER="${TESTS_FAILING_AFTER:-999}"
TESTS_PASSING_AFTER=$(echo "$TEST_OUTPUT" | grep -oE '[0-9]+ passed' | grep -oE '[0-9]+' | head -1)
TESTS_PASSING_AFTER="${TESTS_PASSING_AFTER:-0}"

# 3. Diff scope — check only expected files changed
CHANGED_FILES=$(git diff --name-only 2>/dev/null)
TOTAL_CHANGED=$(echo "$CHANGED_FILES" | grep -c '.' 2>/dev/null || echo 0)

# Score: diff_scoped (1.0 if <=3 files changed, 0.5 if <=6, 0 otherwise)
diff_scoped=0
if [[ "$TOTAL_CHANGED" -le 3 ]]; then
  diff_scoped=1.0
elif [[ "$TOTAL_CHANGED" -le 6 ]]; then
  diff_scoped=0.5
fi

# Score: ts_errors_fixed (proportion of errors fixed)
ts_errors_fixed=0
if [[ "$TS_ERRORS_BEFORE" -gt 0 ]]; then
  FIXED=$((TS_ERRORS_BEFORE - TS_ERRORS_AFTER))
  if [[ $FIXED -lt 0 ]]; then FIXED=0; fi
  ts_errors_fixed=$(echo "scale=4; $FIXED / $TS_ERRORS_BEFORE" | bc | sed 's/^\./0./')
fi

# Score: tests_pass (1.0 if no regressions, 0 if new failures)
tests_pass=1.0
if [[ "$TESTS_FAILING_AFTER" -gt "$TESTS_FAILING_BEFORE" ]]; then
  tests_pass=0
elif [[ "$TESTS_FAILING_AFTER" -lt "$TESTS_FAILING_BEFORE" ]]; then
  tests_pass=1.0
fi

# Composite deliver_score
deliver_score=$(echo "scale=4; ($ts_errors_fixed * 0.4) + ($tests_pass * 0.3) + ($diff_scoped * 0.3)" | bc | sed 's/^\./0./')

# ── Revert changes ───────────────────────────────────────────
git checkout -- . 2>/dev/null
git clean -fd 2>/dev/null
git stash pop -q 2>/dev/null || true

# ── Output ────────────────────────────────────────────────────
printf '{"deliver_score": %s, "tests_pass": %s, "ts_errors_fixed": %s, "diff_scoped": %s}\n' \
  "$deliver_score" "$tests_pass" "$ts_errors_fixed" "$diff_scoped"
