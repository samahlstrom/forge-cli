package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

// writeManifest writes a hooks manifest into a temp FORGE_HOME and returns nothing.
func writeManifest(t *testing.T, body string) {
	t.Helper()
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	hooksDir := filepath.Join(forge, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "manifest.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHooksDirUnderForgeHome(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	want := filepath.Join(forge, "hooks")
	if got := HooksDir(); got != want {
		t.Fatalf("HooksDir() = %q, want %q", got, want)
	}
}

func TestHookScriptPathIsAbsoluteUnderHooksDir(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	want := filepath.Join(forge, "hooks", "pre-push-validate.sh")
	if got := HookScriptPath("pre-push-validate.sh"); got != want {
		t.Fatalf("HookScriptPath() = %q, want %q", got, want)
	}
}

func TestListHooksParsesManifest(t *testing.T) {
	writeManifest(t, `{
  "hooks": [
    {"name":"pre-push-validate","kind":"git-hook","gitHook":"pre-push","script":"pre-push-validate.sh","scope":"repo","default":true},
    {"name":"validate-gate","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Bash","script":"validate-gate.sh","scope":"repo","default":false,"note":"leaky"}
  ],
  "scripts": []
}`)

	hooks := ListHooks()
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d: %+v", len(hooks), hooks)
	}

	g := hooks[0]
	if g.Name != "pre-push-validate" || g.Kind != "git-hook" || g.GitHook != "pre-push" ||
		g.Script != "pre-push-validate.sh" || g.Scope != "repo" || !g.Default {
		t.Fatalf("git-hook entry parsed wrong: %+v", g)
	}

	c := hooks[1]
	if c.Name != "validate-gate" || c.Kind != "claude-settings-hook" || c.Event != "PreToolUse" ||
		c.Matcher != "Bash" || c.Script != "validate-gate.sh" || c.Default {
		t.Fatalf("claude-settings-hook entry parsed wrong: %+v", c)
	}
	if c.Default {
		t.Fatalf("validate-gate must default to false (opt-in only)")
	}
}

func TestListHooksMissingManifestReturnsNil(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge) // no hooks/manifest.json written
	if hooks := ListHooks(); hooks != nil {
		t.Fatalf("expected nil for absent manifest, got %+v", hooks)
	}
}

func TestListHooksMalformedJSONHandledGracefully(t *testing.T) {
	writeManifest(t, `{ this is not valid json `)
	// Must not panic; returns no hooks.
	if hooks := ListHooks(); len(hooks) != 0 {
		t.Fatalf("expected no hooks from malformed manifest, got %+v", hooks)
	}
	if _, err := LoadHooksManifest(); err == nil {
		t.Fatalf("LoadHooksManifest should report an error for malformed JSON")
	}
}

func TestListHooksMalformedEntryKeepsValidEntries(t *testing.T) {
	// One entry is missing its kind/script; the valid one must still parse.
	writeManifest(t, `{
  "hooks": [
    {"name":"incomplete"},
    {"name":"pre-push-validate","kind":"git-hook","gitHook":"pre-push","script":"pre-push-validate.sh","scope":"repo","default":true}
  ]
}`)
	hooks := ListHooks()
	if len(hooks) != 2 {
		t.Fatalf("expected both entries returned (installer skips incomplete ones), got %d", len(hooks))
	}
	if hooks[0].Kind != "" {
		t.Fatalf("incomplete entry should have empty Kind, got %q", hooks[0].Kind)
	}
}
