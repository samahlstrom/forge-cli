#!/usr/bin/env bash
# deliver-test.sh — Set up a throwaway project and run /deliver against it
#
# Usage:
#   bash test/deliver-test.sh              # interactive (opens Claude Code)
#   bash test/deliver-test.sh --headless   # runs claude -p directly
#
# What it does:
#   1. Creates a temp project from the fixture
#   2. Installs deps, runs forge init
#   3. Initializes beads, creates a test task
#   4. Runs /deliver against that task
#   5. Reports: did it work?
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FORGE_BIN="${REPO_ROOT}/forge"
HEADLESS=false

for arg in "$@"; do
  case "$arg" in
    --headless) HEADLESS=true ;;
  esac
done

# ── Build forge ─────────────────────────────────────────────────
echo "==> Building forge..."
(cd "$REPO_ROOT" && go build -o forge .)

# ── Create test project ────────────────────────────────────────
TEST_DIR=$(mktemp -d -t forge-deliver-test)
echo "==> Test project: $TEST_DIR"

cp -R "$REPO_ROOT/test/fixtures/refine-target/" "$TEST_DIR/"
cd "$TEST_DIR"

# Add a vitest config so tests actually run
cat > vitest.config.js << 'EOF'
import { defineConfig } from 'vitest/config'
export default defineConfig({ test: { globals: true } })
EOF

# Init git
git init -q
git add -A
git commit -q -m "initial commit"

# ── Install deps ───────────────────────────────────────────────
echo "==> Installing dependencies..."
npm install --save-dev vitest > /dev/null 2>&1

git add -A
git commit -q -m "add vitest"

# ── Run forge init ─────────────────────────────────────────────
echo "==> Running forge init..."
"$FORGE_BIN" init --yes 2>&1 | tail -5

git add -A
git commit -q -m "forge: initialize harness"

# ── Initialize beads ──────────────────────────────────────────
echo "==> Initializing beads..."
bd init --quiet 2>/dev/null || true

# ── Create test task ──────────────────────────────────────────
TASK_DESC="Add a GET /status endpoint to src/index.js that returns JSON with the server uptime in seconds and current memory usage in MB. The response should look like: {\"status\": \"ok\", \"uptime_s\": 123.45, \"memory_mb\": 45.2}. Add a test for it in test/status.test.js that verifies the endpoint returns 200, has the correct content-type, and includes uptime_s and memory_mb fields."

echo "==> Creating test task..."
TASK_ID=$(bd create \
  --title="Add /status endpoint with uptime and memory" \
  --description="$TASK_DESC" \
  --type=task \
  --priority=2 \
  --json 2>/dev/null | jq -r '.id // empty')

if [[ -z "$TASK_ID" ]]; then
  echo "ERROR: Failed to create bead task"
  exit 1
fi

echo "==> Task created: $TASK_ID"
bd update "$TASK_ID" --claim 2>/dev/null || true

# ── Show project state ────────────────────────────────────────
echo ""
echo "=========================================="
echo "  DELIVER TEST READY"
echo "=========================================="
echo ""
echo "  Project:  $TEST_DIR"
echo "  Task:     $TASK_ID"
echo "  Title:    Add /status endpoint with uptime and memory"
echo ""

# ── Verify current state ─────────────────────────────────────
echo "  Pre-flight checks:"
TYPECHECK_OK=false
TEST_OK=false

if npx tsc --noEmit > /dev/null 2>&1; then
  TYPECHECK_OK=true
  echo "    ✓ Typecheck passes"
else
  echo "    ✗ Typecheck fails (expected — no strict TS)"
fi

if node --test test/*.test.js > /dev/null 2>&1; then
  TEST_OK=true
  echo "    ✓ Tests pass"
else
  echo "    ✗ Tests fail"
fi

echo ""

if [[ "$HEADLESS" == "true" ]]; then
  # ── Headless mode: run /deliver via claude -p ──────────────
  echo "==> Running /deliver headless..."

  PROMPT="You are working in: $TEST_DIR

Your task: run /deliver for task $TASK_ID

The task is: $TASK_DESC

Follow the /deliver skill instructions exactly. Run the orchestrator, execute each stage, and produce the code change. Do NOT create a PR (there is no remote). Instead, commit directly to the current branch.

After completing the work:
1. Run the typecheck command
2. Run the tests
3. Report whether both pass"

  PROMPT_FILE=$(mktemp)
  echo "$PROMPT" > "$PROMPT_FILE"

  START_TIME=$(date +%s)

  cat "$PROMPT_FILE" | claude -p \
    --dangerously-skip-permissions \
    --output-format json \
    --model claude-sonnet-4-6 \
    > "$TEST_DIR/.forge/deliver-test-output.json" 2>&1 || true

  rm "$PROMPT_FILE"
  END_TIME=$(date +%s)
  DURATION=$((END_TIME - START_TIME))

  echo ""
  echo "=========================================="
  echo "  DELIVER TEST RESULTS"
  echo "=========================================="
  echo ""
  echo "  Duration: ${DURATION}s"

  # ── Check results ──────────────────────────────────────────
  PASS_COUNT=0
  FAIL_COUNT=0

  # 1. Did new files get created?
  if [[ -f src/index.js ]] && git diff --name-only HEAD~1 2>/dev/null | grep -q "index.js"; then
    echo "  ✓ src/index.js was modified"
    PASS_COUNT=$((PASS_COUNT + 1))
  elif git diff --name-only 2>/dev/null | grep -q "index.js"; then
    echo "  ✓ src/index.js was modified (uncommitted)"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "  ✗ src/index.js was not modified"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  # 2. Does the status test exist?
  if [[ -f test/status.test.js ]] || [[ -f test/status.test.ts ]]; then
    echo "  ✓ Status test file created"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "  ✗ Status test file not created"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  # 3. Does /status endpoint exist in code?
  if grep -q "/status" src/index.js 2>/dev/null; then
    echo "  ✓ /status endpoint found in code"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "  ✗ /status endpoint not found in code"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  # 4. Do tests pass?
  if npx vitest run --reporter=verbose > "$TEST_DIR/.forge/test-output.txt" 2>&1; then
    echo "  ✓ All tests pass"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    # Try node --test as fallback
    if node --test test/*.test.js > /dev/null 2>&1; then
      echo "  ✓ All tests pass (node --test)"
      PASS_COUNT=$((PASS_COUNT + 1))
    else
      echo "  ✗ Tests fail"
      FAIL_COUNT=$((FAIL_COUNT + 1))
      tail -10 "$TEST_DIR/.forge/test-output.txt" 2>/dev/null
    fi
  fi

  # 5. Is the diff scoped?
  CHANGED=$(git diff --name-only 2>/dev/null; git diff --name-only HEAD~1 2>/dev/null)
  UNRELATED=$(echo "$CHANGED" | grep -v "index.js\|status\|package" | grep -v "^$" || true)
  if [[ -z "$UNRELATED" ]]; then
    echo "  ✓ Diff is scoped (no unrelated files)"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "  ✗ Diff touches unrelated files: $UNRELATED"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  echo ""
  echo "  Score: $PASS_COUNT/5"
  echo "  Project left at: $TEST_DIR"
  echo ""

  if [[ $PASS_COUNT -ge 4 ]]; then
    echo "  PASS — deliver pipeline is working"
  else
    echo "  FAIL — deliver pipeline needs work"
    echo "  Check: $TEST_DIR/.forge/deliver-test-output.json"
  fi

else
  # ── Interactive mode ───────────────────────────────────────
  echo "  To test /deliver, open Claude Code in this directory:"
  echo ""
  echo "    cd $TEST_DIR"
  echo "    claude"
  echo ""
  echo "  Then run:"
  echo ""
  echo "    /deliver \"Add a GET /status endpoint to src/index.js"
  echo "     that returns JSON with uptime_s and memory_mb."
  echo "     Add test in test/status.test.js.\""
  echo ""
  echo "  After it completes, check:"
  echo "    1. Does src/index.js have a /status route?"
  echo "    2. Does test/status.test.js exist?"
  echo "    3. Do tests pass? (node --test test/*.test.js)"
  echo ""
fi
