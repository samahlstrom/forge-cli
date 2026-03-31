#!/usr/bin/env bash
# Measure forge init quality — outputs JSON with metric values (0.0 to 1.0)
# Used by: forge refine templates/core/refine/criteria-init.yaml
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
FIXTURE_DIR="${REPO_ROOT}/test/fixtures/refine-target"
FORGE_BIN="${REPO_ROOT}/forge"

# Build forge if binary is stale
if [[ ! -f "$FORGE_BIN" ]] || [[ "$REPO_ROOT/cmd/init.go" -nt "$FORGE_BIN" ]]; then
  (cd "$REPO_ROOT" && go build -o forge . 2>/dev/null)
fi

# Create temp dir
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Copy fixture
cp -R "$FIXTURE_DIR/" "$TMPDIR/"
cd "$TMPDIR"

# Init git
git init -q
git add -A
git commit -q -m "fixture baseline" 2>/dev/null

# ── Run forge init ──────────────────────────────────────────────
"$FORGE_BIN" init --yes > /dev/null 2>&1
INIT_EXIT=$?

# ── Metric 1: init_score (composite) ───────────────────────────
# Starts at 1.0, deducted for each failure

score=1.0
deductions=0
total_checks=5

# Check 1: did it exit 0?
if [[ $INIT_EXIT -ne 0 ]]; then
  deductions=$((deductions + 1))
fi

# Check 2: are all expected files present?
files_ok=1
expected_files=(
  "forge.yaml"
  "CLAUDE.md"
  ".claude/settings.json"
  ".claude/skills/forge/SKILL.md"
  ".forge/pipeline/helpers.sh"
  ".forge/pipeline/intake.sh"
  ".forge/pipeline/classify.md"
  ".forge/pipeline/review-plan.md"
  ".forge/pipeline/verify.sh"
  ".forge/pipeline/deliver.sh"
  ".forge/agents/architect.md"
  ".forge/agents/edgar.md"
  ".forge/agents/code-quality.md"
  ".forge/agents/um-actually.md"
  ".forge/context/stack.md"
  ".forge/context/project.md"
  ".forge/hooks/pre-edit.sh"
  ".forge/hooks/session-start.sh"
)
total_files=${#expected_files[@]}
present_files=0
for f in "${expected_files[@]}"; do
  if [[ -f "$f" ]]; then
    present_files=$((present_files + 1))
  fi
done
files_generated=$(echo "scale=4; $present_files / $total_files" | bc | sed 's/^\./0./')
if [[ "$present_files" -ne "$total_files" ]]; then
  deductions=$((deductions + 1))
fi

# Check 3: valid YAML config?
yaml_ok=1
for key in "version:" "project:" "commands:" "agents:" "verification:" "evaluation:" "pipeline:"; do
  if ! grep -q "^${key}" forge.yaml 2>/dev/null; then
    yaml_ok=0
    break
  fi
done
if [[ $yaml_ok -eq 0 ]]; then
  deductions=$((deductions + 1))
fi

# Check 4: no unresolved template variables?
templates_resolved=1.0
if grep -rq '{{.*}}' .forge/ forge.yaml CLAUDE.md .claude/ 2>/dev/null; then
  templates_resolved=0.0
  deductions=$((deductions + 1))
fi

# Check 5: idempotent?
snapshot1=$(find . -not -path './.git/*' -not -path './node_modules/*' -type f | sort | xargs md5 -q 2>/dev/null | md5 -q)
"$FORGE_BIN" init --yes > /dev/null 2>&1 || true
snapshot2=$(find . -not -path './.git/*' -not -path './node_modules/*' -type f | sort | xargs md5 -q 2>/dev/null | md5 -q)
idempotent=1.0
if [[ "$snapshot1" != "$snapshot2" ]]; then
  idempotent=0.0
  deductions=$((deductions + 1))
fi

# Compute composite score
init_score=$(echo "scale=4; 1.0 - ($deductions / $total_checks)" | bc | sed 's/^\./0./')

# ── Output JSON ────────────────────────────────────────────────
printf '{"init_score": %s, "files_generated": %s, "templates_resolved": %s, "idempotent": %s}\n' \
  "$init_score" "$files_generated" "$templates_resolved" "$idempotent"
