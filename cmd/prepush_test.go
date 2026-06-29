package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gateScript returns the path to the source pre-push-validate.sh, skipping the
// test if bash isn't available.
func gateScript(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	// Test runs with cwd = cmd/ package dir.
	p, err := filepath.Abs(filepath.Join("..", "library", "hooks", "pre-push-validate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("gate script not found at %s: %v", p, err)
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
