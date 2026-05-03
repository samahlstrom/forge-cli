#!/usr/bin/env bash
# forge pipeline — banned-patterns: grep test files in the diff for forbidden patterns
# Doctrine: <library_dir>/doctrine/tdd.md § Banned Test Patterns
# Exit code 0 = clean, 1 = at least one banned pattern found
set -euo pipefail

# Compare against the parent commit by default; allow override
DIFF_BASE="${DIFF_BASE:-HEAD~1}"

# Find changed test files in the diff
mapfile -t CHANGED_TESTS < <(
  git diff --name-only --diff-filter=AMR "$DIFF_BASE" HEAD 2>/dev/null \
    | grep -E '(\.test\.|\.spec\.|_test\.go$|/tests/)' \
    | grep -E '\.(ts|tsx|js|jsx|mjs|cjs|go|py|rb)$' \
    || true
)

if [[ "${#CHANGED_TESTS[@]}" -eq 0 ]]; then
  echo "No test files changed; banned-patterns check skipped." >&2
  exit 0
fi

# Patterns that indicate testing implementation rather than behavior.
# Each line: <regex>|<human description>
PATTERNS=(
  'toHaveBeenCalledWith\b|asserts on a spy call — tests internal collaborator instead of behavior'
  'toHaveBeenCalled\b|asserts a spy was called — tests internal collaborator instead of behavior'
  'toHaveBeenCalledTimes\b|asserts call count on a spy — tests internal collaborator instead of behavior'
  'toHaveBeenLastCalledWith\b|asserts last call on a spy — tests internal collaborator instead of behavior'
  'toHaveBeenNthCalledWith\b|asserts Nth call on a spy — tests internal collaborator instead of behavior'
  'expect\([A-Za-z_][A-Za-z0-9_]*\)\.toMatchSnapshot\(\)|snapshot-only behavior assertion — use a behavior assertion'
  'expect\([A-Za-z_][A-Za-z0-9_]*\)\.toMatchInlineSnapshot\(|inline snapshot — use a behavior assertion'
)

# Heuristic structural-test name shape: describe('SomeClass', ...) where the value
# inside the quotes is PascalCase and the next line opens a nested describe with a
# camelCase method name. Caught with a simpler regex: a describe whose first arg
# matches PascalCase (likely a class/module name) is suspicious enough to flag.
# This is a soft warning — we report but do not fail on this alone.

VIOLATIONS=()

for file in "${CHANGED_TESTS[@]}"; do
  [[ -f "$file" ]] || continue
  for entry in "${PATTERNS[@]}"; do
    pattern="${entry%%|*}"
    desc="${entry#*|}"
    while IFS=: read -r matched_line lineno content; do
      [[ -z "$matched_line" ]] && continue
      VIOLATIONS+=("${file}:${matched_line} — ${desc}")
    done < <(grep -nE "$pattern" "$file" 2>/dev/null | awk -F: '{print $1":"$1":"substr($0, index($0,$2))}' || true)
  done
done

if [[ "${#VIOLATIONS[@]}" -eq 0 ]]; then
  echo "banned-patterns: clean (${#CHANGED_TESTS[@]} test file(s) scanned)" >&2
  exit 0
fi

echo "banned-patterns: VIOLATIONS FOUND" >&2
echo "" >&2
echo "Doctrine: see <forge_home>/library/doctrine/tdd.md § Banned Test Patterns" >&2
echo "" >&2
for v in "${VIOLATIONS[@]}"; do
  echo "  - $v" >&2
done
echo "" >&2
echo "Behavior tests must verify outputs and persisted state through the public surface," >&2
echo "not assert on whether internal collaborators were called." >&2

exit 1
