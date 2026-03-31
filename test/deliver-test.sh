#!/usr/bin/env bash
# deliver-test.sh — Set up a throwaway project, run a task via claude -p, score it
#
# Usage:
#   bash test/deliver-test.sh              # runs headless, streams output
#   bash test/deliver-test.sh --interactive # sets up project, you run /deliver
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FORGE_BIN="${REPO_ROOT}/forge"
INTERACTIVE=false

for arg in "$@"; do
  case "$arg" in
    --interactive) INTERACTIVE=true ;;
  esac
done

# ── Build forge ─────────────────────────────────────────────────
echo "==> Building forge..."
(cd "$REPO_ROOT" && go build -o forge .)

# ── Create test project ────────────────────────────────────────
TEST_DIR="/tmp/forgetest"
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"
echo "==> Test project: $TEST_DIR"

cp -R "$REPO_ROOT/test/fixtures/refine-target/" "$TEST_DIR/"
cd "$TEST_DIR"

# Init git
git init -q
git add -A
git commit -q -m "initial commit"

# ── Install deps ───────────────────────────────────────────────
echo "==> Installing deps..."
npm install --save-dev vitest > /dev/null 2>&1
git add -A
git commit -q -m "add vitest"

# ── Run forge init ─────────────────────────────────────────────
echo "==> Running forge init..."
"$FORGE_BIN" init --yes > /dev/null 2>&1
git add -A
git commit -q -m "forge: initialize harness"

# ── Pre-flight ────────────────────────────────────────────────
echo "==> Pre-flight:"
node --test test/*.test.js > /dev/null 2>&1 && echo "    ✓ Tests pass" || echo "    ✗ Tests fail"
echo ""

TASK_DESC="Add a GET /status endpoint to src/index.js that returns JSON: {\"status\": \"ok\", \"uptime_s\": <process.uptime()>, \"memory_mb\": <process.memoryUsage().rss / 1048576>}. Add a test in test/status.test.js that verifies: 200 status, correct content-type, and that response body has uptime_s and memory_mb as numbers."

if [[ "$INTERACTIVE" == "true" ]]; then
  echo "=========================================="
  echo "  DELIVER TEST — INTERACTIVE"
  echo "=========================================="
  echo ""
  echo "  Project: $TEST_DIR"
  echo ""
  echo "  Run:"
  echo "    cd $TEST_DIR && claude"
  echo ""
  echo "  Then:"
  echo '    /deliver "Add GET /status endpoint returning uptime_s and memory_mb. Add test."'
  echo ""
  exit 0
fi

# ── Headless: run via claude -p ───────────────────────────────
echo "=========================================="
echo "  DELIVER TEST — HEADLESS"
echo "=========================================="
echo ""

PROMPT="I am in the directory: $TEST_DIR

This is a Node.js HTTP server project. Here is the current src/index.js:

$(cat src/index.js)

Here is the current test file test/health.test.js:

$(cat test/health.test.js)

YOUR TASK:
$TASK_DESC

RULES:
1. Edit src/index.js to add the /status route
2. Create test/status.test.js with tests for the new endpoint
3. Do NOT modify any other files
4. Do NOT add any dependencies
5. Use the same patterns as the existing code (http module, node:test, node:assert)
6. After making changes, run: node --test test/health.test.js test/status.test.js
7. Fix any test failures before finishing"

PROMPT_FILE=$(mktemp)
echo "$PROMPT" > "$PROMPT_FILE"

START_TIME=$(date +%s)

echo "==> Running claude -p (streaming)..."
echo ""

# Run claude and tee output so user sees progress
cat "$PROMPT_FILE" | claude -p \
  --dangerously-skip-permissions \
  --model claude-sonnet-4-6 \
  2>&1 | tee "$TEST_DIR/claude-output.txt"

CLAUDE_EXIT=$?
rm -f "$PROMPT_FILE"

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo ""
echo "=========================================="
echo "  RESULTS  (${DURATION}s)"
echo "=========================================="
echo ""

# ── Score ─────────────────────────────────────────────────────
PASS=0
FAIL=0

# 1. Was src/index.js modified?
if grep -q "/status" src/index.js 2>/dev/null; then
  echo "  ✓ /status endpoint exists in src/index.js"
  PASS=$((PASS + 1))
else
  echo "  ✗ /status endpoint NOT found in src/index.js"
  FAIL=$((FAIL + 1))
fi

# 2. Does status test file exist?
if [[ -f test/status.test.js ]] || [[ -f test/status.test.ts ]] || [[ -f test/status.test.mjs ]]; then
  echo "  ✓ Status test file created"
  PASS=$((PASS + 1))
else
  echo "  ✗ Status test file NOT created"
  FAIL=$((FAIL + 1))
fi

# 3. Does response include uptime_s?
if grep -q "uptime_s\|uptime" src/index.js 2>/dev/null; then
  echo "  ✓ uptime field in response"
  PASS=$((PASS + 1))
else
  echo "  ✗ uptime field NOT in response"
  FAIL=$((FAIL + 1))
fi

# 4. Does response include memory_mb?
if grep -q "memory_mb\|memory" src/index.js 2>/dev/null; then
  echo "  ✓ memory field in response"
  PASS=$((PASS + 1))
else
  echo "  ✗ memory field NOT in response"
  FAIL=$((FAIL + 1))
fi

# 5. Do ALL tests pass?
echo ""
echo "  Running tests..."
if node --test test/*.test.js 2>&1; then
  echo "  ✓ All tests pass"
  PASS=$((PASS + 1))
else
  echo "  ✗ Tests fail"
  FAIL=$((FAIL + 1))
fi

echo ""
echo "  Score: $PASS/5"
echo "  Project: $TEST_DIR"
echo ""

if [[ $PASS -ge 4 ]]; then
  echo "  ✅ PASS"
else
  echo "  ❌ FAIL — check $TEST_DIR/claude-output.txt"
fi
