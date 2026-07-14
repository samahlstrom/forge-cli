package resolve

import (
	"os"
	"path/filepath"
	"strings"
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

func TestUpsertHookCreatesManifestWhenAbsent(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge) // no manifest yet

	h := HookInfo{Name: "my-gate", Kind: "git-hook", GitHook: "pre-push", Script: "my-gate.sh", Scope: "repo", Default: true}
	if err := UpsertHook(h); err != nil {
		t.Fatalf("UpsertHook into absent manifest: %v", err)
	}

	hooks := ListHooks()
	if len(hooks) != 1 || hooks[0].Name != "my-gate" || hooks[0].GitHook != "pre-push" {
		t.Fatalf("manifest not created with the hook: %+v", hooks)
	}
}

func TestUpsertHookAppendsThenReplacesByName(t *testing.T) {
	writeManifest(t, `{"hooks":[{"name":"a","kind":"git-hook","gitHook":"pre-push","script":"a.sh","scope":"repo","default":true}]}`)

	// Append a second, different hook.
	if err := UpsertHook(HookInfo{Name: "b", Kind: "claude-settings-hook", Event: "PreToolUse", Matcher: "Bash", Script: "b.sh", Scope: "repo"}); err != nil {
		t.Fatal(err)
	}
	if hooks := ListHooks(); len(hooks) != 2 {
		t.Fatalf("expected 2 hooks after append, got %d", len(hooks))
	}

	// Upsert "a" again with a new script — replace in place, no duplicate.
	if err := UpsertHook(HookInfo{Name: "a", Kind: "git-hook", GitHook: "pre-commit", Script: "a2.sh", Scope: "repo", Default: false}); err != nil {
		t.Fatal(err)
	}
	hooks := ListHooks()
	if len(hooks) != 2 {
		t.Fatalf("replace must not duplicate; got %d hooks", len(hooks))
	}
	for _, h := range hooks {
		if h.Name == "a" && (h.GitHook != "pre-commit" || h.Script != "a2.sh") {
			t.Fatalf("hook a not replaced: %+v", h)
		}
	}
}

func TestUpsertHookMalformedManifestErrorsWithoutClobber(t *testing.T) {
	writeManifest(t, `{ not valid json`)
	if err := UpsertHook(HookInfo{Name: "x", Kind: "git-hook", GitHook: "pre-push", Script: "x.sh", Scope: "repo"}); err == nil {
		t.Fatal("UpsertHook must error on a malformed manifest rather than clobber it")
	}
	// The malformed file is left as-is.
	data, _ := os.ReadFile(filepath.Join(HooksDir(), "manifest.json"))
	if !strings.Contains(string(data), "not valid json") {
		t.Fatalf("malformed manifest must be left untouched, got: %s", data)
	}
}

func TestSaveManifestOmitsEmptyOptionalFields(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	if err := UpsertHook(HookInfo{Name: "g", Kind: "git-hook", GitHook: "pre-push", Script: "g.sh", Scope: "repo", Default: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(HooksDir(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, absent := range []string{`"event"`, `"matcher"`, `"note"`} {
		if strings.Contains(s, absent) {
			t.Fatalf("git-hook entry should omit %s; manifest:\n%s", absent, s)
		}
	}
	if !strings.Contains(s, `"gitHook"`) {
		t.Fatalf("git-hook entry must keep gitHook; manifest:\n%s", s)
	}
}

func TestRemoveHookFromManifest(t *testing.T) {
	writeManifest(t, `{"hooks":[
	  {"name":"a","kind":"git-hook","gitHook":"pre-push","script":"a.sh","scope":"repo","default":true},
	  {"name":"b","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Bash","script":"b.sh","scope":"repo"}
	]}`)

	removed, ok, err := RemoveHookFromManifest("a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || removed.Script != "a.sh" {
		t.Fatalf("expected to remove a (script a.sh), got ok=%v removed=%+v", ok, removed)
	}
	hooks := ListHooks()
	if len(hooks) != 1 || hooks[0].Name != "b" {
		t.Fatalf("only b should remain: %+v", hooks)
	}

	// Removing an absent hook reports not-found, no error.
	if _, ok, err := RemoveHookFromManifest("missing"); err != nil || ok {
		t.Fatalf("removing absent hook: ok=%v err=%v", ok, err)
	}
}
