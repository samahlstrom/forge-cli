#!/usr/bin/env bash
# validate-gate.sh — PreToolUse(Bash) hook. Hard-blocks `gh pr create` until a
# branch-keyed validation receipt exists. Shared across repos (next, one).
#
# Receipt: <repo>/.claude/.validate-receipt
#   first line: "<branch> | <validated|skipped> | <reason/linear-id + timestamp>"
#   written by the /validate skill at its Definition of Done, or as an explicit
#   justified skip for changes that touch no UI surface.
#
# Contract: exit 0 = allow the command; exit 2 = block and feed stderr to the
# model. Every command that is not `gh pr create` passes through untouched, so
# this never interferes with normal Bash.

input="$(cat)"
cmd="$(printf '%s' "$input" | jq -r '.tool_input.command // ""' 2>/dev/null)"
cwd="$(printf '%s' "$input" | jq -r '.cwd // ""' 2>/dev/null)"
[ -z "$cwd" ] && cwd="$(pwd)"

# Only gate an actual `gh pr create` invocation — at command start or right
# after a shell separator (; && || | newline ( ). This deliberately ignores the
# phrase appearing inside a quoted string or message body (e.g. an echo, a
# comment, a coordination message), which would otherwise false-block.
printf '%s' "$cmd" | grep -Eq '(^|[;&|(])[[:space:]]*gh[[:space:]]+pr[[:space:]]+create' || exit 0

branch="$(git -C "$cwd" branch --show-current 2>/dev/null)"
receipt="$cwd/.claude/.validate-receipt"

if [ -n "$branch" ] && [ -f "$receipt" ]; then
  rb="$(head -n1 "$receipt" 2>/dev/null | cut -d'|' -f1 | tr -d '[:space:]')"
  [ "$rb" = "$branch" ] && exit 0
fi

cat >&2 <<EOF
BLOCKED: gh pr create requires a validation receipt for branch "${branch:-unknown}".

Answer honestly before proceeding: does this change touch ANYTHING a user can
see or interact with in the browser — any page serving a +page.svelte it
reaches, a button, a rendered data point, a visible state?

- If YES: you have NOT validated, and you should — this is visually verifiable.
  Run /validate, capture a screenshot per affected state, post the flow to the
  Linear card. /validate writes the receipt when its Definition of Done is met.
- If genuinely NO UI surface is touched: record the justification —
    mkdir -p "$cwd/.claude" && echo "$branch | skipped | <one-line reason: no UI surface touched>" > "$cwd/.claude/.validate-receipt"

Do not retry gh pr create until the receipt names this branch.
EOF
exit 2
