package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samahlstrom/forge-cli/internal/resolve"
)

// newToolkit creates a temp FORGE_HOME that looks set up (has agents/ so
// resolve.IsSetup() is true) and is NOT a git repo, so commitAndPush no-ops.
func newToolkit(t *testing.T) string {
	t.Helper()
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	for _, d := range []string{"agents", "skills", "hooks"} {
		if err := os.MkdirAll(filepath.Join(forge, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return forge
}

// resetHookFlags clears the package-level hook flag vars between direct RunE calls.
func resetHookFlags() {
	hookFile, hookScaffold, hookGitHook, hookEvent, hookMatcher, hookDefault, hookScope = "", false, "", "", "", false, "repo"
}

func TestHookAddUploadsExistingScriptAndRegisters(t *testing.T) {
	forge := newToolkit(t)

	// A script the user already wrote, mode 0644 (not executable).
	src := filepath.Join(t.TempDir(), "my-gate.sh")
	if err := os.WriteFile(src, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resetHookFlags()
	hookFile = src
	hookGitHook = "pre-push"

	if err := runHookAdd(nil, []string{"my-gate"}); err != nil {
		t.Fatalf("runHookAdd: %v", err)
	}

	// Script copied into the toolkit and made executable.
	dst := filepath.Join(forge, "hooks", "my-gate.sh")
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("uploaded script missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("uploaded hook must be executable, got %v", info.Mode().Perm())
	}

	// Registered in the manifest as a default:false git-hook.
	hooks := resolve.ListHooks()
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook in manifest, got %d", len(hooks))
	}
	h := hooks[0]
	if h.Name != "my-gate" || h.Kind != "git-hook" || h.GitHook != "pre-push" || h.Script != "my-gate.sh" || h.Default {
		t.Fatalf("manifest entry wrong: %+v", h)
	}
}

func TestHookAddScaffoldCreatesExecutableScript(t *testing.T) {
	forge := newToolkit(t)

	resetHookFlags()
	hookScaffold = true
	hookGitHook = "pre-push"
	hookDefault = true

	if err := runHookAdd(nil, []string{"blank-gate"}); err != nil {
		t.Fatalf("runHookAdd scaffold: %v", err)
	}

	dst := filepath.Join(forge, "hooks", "blank-gate.sh")
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("scaffolded script missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("scaffolded hook must be executable, got %v", info.Mode().Perm())
	}
	if h := resolve.ListHooks(); len(h) != 1 || !h[0].Default {
		t.Fatalf("expected one default hook, got %+v", h)
	}
}

func TestHookAddClaudeSettingsHookNeedsMatcher(t *testing.T) {
	newToolkit(t)
	resetHookFlags()
	hookScaffold = true
	hookEvent = "PreToolUse" // no --matcher
	if err := runHookAdd(nil, []string{"x"}); err == nil {
		t.Fatal("expected error when --event has no --matcher")
	}
}

func TestHookAddRequiresKind(t *testing.T) {
	newToolkit(t)
	resetHookFlags()
	hookScaffold = true // kind flags omitted
	if err := runHookAdd(nil, []string{"x"}); err == nil {
		t.Fatal("expected error when neither --git-hook nor --event is given")
	}
}

func TestHookAddRequiresFileOrScaffold(t *testing.T) {
	newToolkit(t)
	resetHookFlags()
	hookGitHook = "pre-push" // no --file, no --scaffold
	if err := runHookAdd(nil, []string{"x"}); err == nil {
		t.Fatal("expected error when neither --file nor --scaffold is given")
	}
}

func TestHookRemoveDropsEntryAndScript(t *testing.T) {
	forge := newToolkit(t)

	resetHookFlags()
	hookScaffold = true
	hookGitHook = "pre-push"
	if err := runHookAdd(nil, []string{"gone"}); err != nil {
		t.Fatal(err)
	}
	if err := runHookRemove(nil, []string{"gone"}); err != nil {
		t.Fatalf("runHookRemove: %v", err)
	}

	if hooks := resolve.ListHooks(); len(hooks) != 0 {
		t.Fatalf("manifest should be empty after remove, got %+v", hooks)
	}
	if _, err := os.Stat(filepath.Join(forge, "hooks", "gone.sh")); !os.IsNotExist(err) {
		t.Fatalf("hook script should be deleted on remove")
	}
}

func TestHookRemoveMissingErrors(t *testing.T) {
	newToolkit(t)
	resetHookFlags()
	if err := runHookRemove(nil, []string{"nope"}); err == nil {
		t.Fatal("removing a non-existent hook should error")
	}
}
