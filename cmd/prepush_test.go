package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// prepushGateScript is an inline copy of the pre-push validation gate. The gate
// itself ships from the personal toolkit (~/.forge/hooks/pre-push-validate.sh,
// in the forge-toolkit repo), not from this engine — so these tests carry their
// own fixture rather than reading a library/ file. Backticks in the original
// comments are rendered as single quotes so the script fits a Go raw string;
// the executable logic is unchanged.
const prepushGateScript = `#!/usr/bin/env bash
# pre-push-validate.sh — git pre-push gate. Blocks 'git push' of a branch whose
# validation receipt is missing or names a different branch. Installed to
# ~/.forge/hooks/ by 'forge setup'; invoked by the committed .githooks/pre-push
# wrapper that 'forge init' installs into each repo.
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
# The '|| [ -n "$local_ref" ]' guard processes a final line that lacks a trailing
# newline — the pre-push wrapper captures stdin with '$(cat)', which strips it.
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
`

// gateScript writes the inline gate fixture to a temp file and returns its path,
// skipping the test if bash isn't available.
func gateScript(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	p := filepath.Join(t.TempDir(), "pre-push-validate.sh")
	if err := os.WriteFile(p, []byte(prepushGateScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// runGate runs the gate in repoDir with the given stdin and returns exit code + stderr.
func runGate(t *testing.T, repoDir, stdin string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", gateScript(t))
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(stdin)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run gate: %v", err)
		}
	}
	return code, stderr.String()
}

func prepushRepo(t *testing.T, receipt string) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir)
	if receipt != "" {
		claude := filepath.Join(dir, ".claude")
		if err := os.MkdirAll(claude, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(claude, ".validate-receipt"), []byte(receipt), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const fakeSHA = "1111111111111111111111111111111111111111"

func TestGateAllowsMatchingReceipt(t *testing.T) {
	repo := prepushRepo(t, "feature-x | validated | ONE-1 | shots=2 | http://x | 2026-06-29T00:00:00Z\n")
	stdin := "refs/heads/feature-x " + fakeSHA + " refs/heads/feature-x 0000000000000000000000000000000000000000\n"
	code, errOut := runGate(t, repo, stdin)
	if code != 0 {
		t.Fatalf("matching receipt should allow push (exit 0), got %d\n%s", code, errOut)
	}
}

func TestGateBlocksMissingReceipt(t *testing.T) {
	repo := prepushRepo(t, "") // no receipt
	stdin := "refs/heads/feature-x " + fakeSHA + " refs/heads/feature-x 0000000000000000000000000000000000000000\n"
	code, errOut := runGate(t, repo, stdin)
	if code == 0 {
		t.Fatalf("missing receipt must block push (non-zero exit)")
	}
	if !strings.Contains(errOut, "feature-x") {
		t.Fatalf("guidance should name the blocked branch, got:\n%s", errOut)
	}
}

func TestGateBlocksMismatchedBranch(t *testing.T) {
	repo := prepushRepo(t, "other-branch | validated | ONE-1 | shots=1 | http://x | 2026-06-29T00:00:00Z\n")
	stdin := "refs/heads/feature-x " + fakeSHA + " refs/heads/feature-x 0000000000000000000000000000000000000000\n"
	code, _ := runGate(t, repo, stdin)
	if code == 0 {
		t.Fatalf("receipt for a different branch must block push")
	}
}

func TestGateAllowsTagsOnlyPush(t *testing.T) {
	repo := prepushRepo(t, "") // no receipt at all
	// A tag push carries no refs/heads/* — nothing to validate.
	stdin := "refs/tags/v1.0.0 " + fakeSHA + " refs/tags/v1.0.0 0000000000000000000000000000000000000000\n"
	code, errOut := runGate(t, repo, stdin)
	if code != 0 {
		t.Fatalf("tags-only push must pass (exit 0), got %d\n%s", code, errOut)
	}
}

func TestGateAllowsBranchDeletion(t *testing.T) {
	repo := prepushRepo(t, "") // no receipt
	// Deletion: local sha is all-zeros — nothing being validated.
	stdin := "(delete) 0000000000000000000000000000000000000000 refs/heads/feature-x " + fakeSHA + "\n"
	code, errOut := runGate(t, repo, stdin)
	if code != 0 {
		t.Fatalf("branch deletion must pass (exit 0), got %d\n%s", code, errOut)
	}
}

func TestGateBlocksWhenStdinHasNoTrailingNewline(t *testing.T) {
	// The pre-push wrapper captures stdin with `$(cat)`, which strips the
	// trailing newline before re-feeding the gate. The gate must still process
	// that final, newline-less ref line — otherwise it sees no branches and
	// wrongly allows the push.
	repo := prepushRepo(t, "") // no receipt
	stdin := "refs/heads/feature-x " + fakeSHA + " refs/heads/feature-x 0000000000000000000000000000000000000000"
	code, errOut := runGate(t, repo, stdin)
	if code == 0 {
		t.Fatalf("missing receipt must block even with no trailing newline on stdin")
	}
	if !strings.Contains(errOut, "feature-x") {
		t.Fatalf("guidance should name the blocked branch, got:\n%s", errOut)
	}
}

func TestGateScriptHasNoHardcodedUserPath(t *testing.T) {
	data, err := os.ReadFile(gateScript(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/Users/") {
		t.Fatalf("gate script must not contain a hardcoded /Users path")
	}
}
