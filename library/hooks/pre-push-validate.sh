#!/usr/bin/env bash
# pre-push-validate.sh — git pre-push gate. Blocks `git push` of a branch whose
# validation receipt is missing or names a different branch. Installed to
# ~/.forge/hooks/ by `forge setup`; invoked by the committed .githooks/pre-push
# wrapper that `forge init` installs into each repo.
#
# Why a pre-push hook (not a PreToolUse string-match): git hands us the ACTUAL
# refs being pushed on stdin, one per line:
#
#   <local ref> <local sha> <remote ref> <remote sha>
#
# so we gate the real push and never false-block a message, echo, or script that
# merely contains the words "git push" or "gh pr create". A branch DELETION has
# local sha = all-zeros; we skip those (nothing to validate).
#
# Receipt: <repo>/.claude/.validate-receipt — first line is pipe-delimited and
# field 1 is the branch, written by the /validate skill at its Definition of
# Done (or as an explicit, justified skip for changes touching no UI surface).
#
# Exit 0 = allow the push; exit 1 = block and print guidance to stderr.

zero="0000000000000000000000000000000000000000"
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
receipt="$repo_root/.claude/.validate-receipt"

# Collect the local branches being pushed, skipping deletions and non-branch refs.
# The `|| [ -n "$local_ref" ]` guard processes a final line that lacks a trailing
# newline — the pre-push wrapper captures stdin with `$(cat)`, which strips it.
branches=""
while read -r local_ref local_sha remote_ref remote_sha || [ -n "$local_ref" ]; do
  [ -z "$local_ref" ] && continue
  [ "$local_sha" = "$zero" ] && continue # deletion — nothing to validate
  case "$local_ref" in
    refs/heads/*) branches="$branches ${local_ref#refs/heads/}" ;;
    *) ;; # tags and other refs are not gated
  esac
done

# No branch refs in this push (e.g. tags only) — allow.
[ -z "$(printf '%s' "$branches" | tr -d '[:space:]')" ] && exit 0

receipt_branch=""
if [ -f "$receipt" ]; then
  receipt_branch="$(head -n1 "$receipt" 2>/dev/null | cut -d'|' -f1 | tr -d '[:space:]')"
fi

for b in $branches; do
  if [ "$receipt_branch" != "$b" ]; then
    cat >&2 <<EOF
BLOCKED: pushing "$b" requires a validation receipt naming that branch.

The receipt at .claude/.validate-receipt is missing or names a different branch
(found: "${receipt_branch:-none}").

Answer honestly: does this change touch ANYTHING a user can see or interact with
— a page it reaches, a button, a rendered data point, a visible state?

- If YES: you have NOT validated. Run /validate, capture a screenshot per
  affected state, post the flow to the Linear card. /validate writes the receipt
  when its Definition of Done is met.
- If genuinely NO UI surface is touched: record the justification —
    mkdir -p "$repo_root/.claude" && \\
      echo "$b | skipped | <one-line reason: no UI surface touched>" \\
      > "$repo_root/.claude/.validate-receipt"

Do not retry the push until the receipt names "$b".
EOF
    exit 1
  fi
done

exit 0
