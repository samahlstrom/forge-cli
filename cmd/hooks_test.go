package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samahlstrom/forge-cli/internal/resolve"
)

// ---- Claude settings.json deep-merge ----

func readJSON(t *testing.T, p string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON after merge: %v\n%s", err, data)
	}
	return m
}

// preToolUseCommands returns every command string registered under the
// PreToolUse entry with the given matcher.
func preToolUseCommands(t *testing.T, settings map[string]interface{}, matcher string) []string {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]interface{})
	list, _ := hooks["PreToolUse"].([]interface{})
	var cmds []string
	for _, item := range list {
		m, _ := item.(map[string]interface{})
		if s, _ := m["matcher"].(string); s != matcher {
			continue
		}
		inner, _ := m["hooks"].([]interface{})
		for _, h := range inner {
			hm, _ := h.(map[string]interface{})
			if c, _ := hm["command"].(string); c != "" {
				cmds = append(cmds, c)
			}
		}
	}
	return cmds
}

func TestMergeSettingsHookCreatesFileWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".claude", "settings.json")

	if err := mergeClaudeSettingsHook(p, "PreToolUse", "Bash", "/abs/validate-gate.sh"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	got := preToolUseCommands(t, readJSON(t, p), "Bash")
	if len(got) != 1 || got[0] != "/abs/validate-gate.sh" {
		t.Fatalf("expected our command registered, got %v", got)
	}
}

func TestMergeSettingsHookPreservesPermissionsAndOtherMatchers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	existing := `{
  "permissions": {"allow": ["Read", "Bash(git *)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Edit", "hooks": [{"type": "command", "command": "/keep/me.sh"}]}
    ]
  },
  "model": "opus"
}`
	if err := os.WriteFile(p, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mergeClaudeSettingsHook(p, "PreToolUse", "Bash", "/abs/validate-gate.sh"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	settings := readJSON(t, p)

	// Unrelated top-level keys preserved.
	if _, ok := settings["permissions"]; !ok {
		t.Fatalf("permissions lost after merge")
	}
	if m, _ := settings["model"].(string); m != "opus" {
		t.Fatalf("unknown top-level key 'model' lost, got %q", m)
	}

	// Existing Edit matcher preserved untouched.
	edit := preToolUseCommands(t, settings, "Edit")
	if len(edit) != 1 || edit[0] != "/keep/me.sh" {
		t.Fatalf("existing Edit matcher clobbered, got %v", edit)
	}

	// Ours appended under a new Bash matcher.
	bash := preToolUseCommands(t, settings, "Bash")
	if len(bash) != 1 || bash[0] != "/abs/validate-gate.sh" {
		t.Fatalf("our Bash matcher not appended, got %v", bash)
	}
}

func TestMergeSettingsHookIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	for i := 0; i < 3; i++ {
		if err := mergeClaudeSettingsHook(p, "PreToolUse", "Bash", "/abs/validate-gate.sh"); err != nil {
			t.Fatalf("merge %d: %v", i, err)
		}
	}
	got := preToolUseCommands(t, readJSON(t, p), "Bash")
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 command after 3 merges (no dup), got %d: %v", len(got), got)
	}
}

func TestMergeSettingsHookRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(`{ broken `), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mergeClaudeSettingsHook(p, "PreToolUse", "Bash", "/abs/x.sh"); err == nil {
		t.Fatalf("expected error on invalid existing settings.json, got nil")
	}
}

// ---- git pre-push hook install ----

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func gitConfig(t *testing.T, dir, key string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "config", "--get", key).Output()
	return strings.TrimSpace(string(out))
}

func TestInstallGitHookCleanRepo(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	hook := resolve.HookInfo{Name: "pre-push-validate", Kind: "git-hook", GitHook: "pre-push", Script: "pre-push-validate.sh"}
	if err := installGitHook(dir, hook); err != nil {
		t.Fatalf("install: %v", err)
	}

	hookFile := filepath.Join(dir, ".githooks", "pre-push")
	info, err := os.Stat(hookFile)
	if err != nil {
		t.Fatalf("expected %s: %v", hookFile, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("pre-push hook must be executable, mode = %v", info.Mode())
	}
	body, _ := os.ReadFile(hookFile)
	if !strings.Contains(string(body), gitHookSentinel) {
		t.Fatalf("installed hook missing forge sentinel:\n%s", body)
	}
	// core.hooksPath set to the relative committed dir so it travels with worktrees.
	if hp := gitConfig(t, dir, "core.hooksPath"); hp != ".githooks" {
		t.Fatalf("core.hooksPath = %q, want .githooks", hp)
	}
}

func TestInstallGitHookPreservesExistingNonForgeHook(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	// Pre-existing, non-forge pre-push in the committed .githooks dir.
	hooksDir := filepath.Join(dir, ".githooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "#!/usr/bin/env bash\necho user-hook\n"
	prePush := filepath.Join(hooksDir, "pre-push")
	if err := os.WriteFile(prePush, []byte(existing), 0o755); err != nil {
		t.Fatal(err)
	}

	hook := resolve.HookInfo{Name: "pre-push-validate", Kind: "git-hook", GitHook: "pre-push", Script: "pre-push-validate.sh"}
	if err := installGitHook(dir, hook); err != nil {
		t.Fatalf("install: %v", err)
	}

	// The original is preserved as pre-push.local and chained.
	local, err := os.ReadFile(filepath.Join(hooksDir, "pre-push.local"))
	if err != nil {
		t.Fatalf("existing hook should be preserved as pre-push.local: %v", err)
	}
	if string(local) != existing {
		t.Fatalf("preserved hook content changed:\n%s", local)
	}
	body, _ := os.ReadFile(prePush)
	if !strings.Contains(string(body), gitHookSentinel) {
		t.Fatalf("forge hook should now own pre-push:\n%s", body)
	}
	if !strings.Contains(string(body), "pre-push.local") {
		t.Fatalf("forge hook must chain the preserved pre-push.local:\n%s", body)
	}
}

func TestInstallGitHookRespectsExistingHooksPath(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	// Repo already uses a custom relative hooks dir.
	cmd := exec.Command("git", "-C", dir, "config", "core.hooksPath", "myhooks")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set hooksPath: %v\n%s", err, out)
	}

	hook := resolve.HookInfo{Name: "pre-push-validate", Kind: "git-hook", GitHook: "pre-push", Script: "pre-push-validate.sh"}
	if err := installGitHook(dir, hook); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Installed INTO the existing hooks dir; the setting is NOT overridden.
	if _, err := os.Stat(filepath.Join(dir, "myhooks", "pre-push")); err != nil {
		t.Fatalf("hook should install into existing core.hooksPath dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".githooks", "pre-push")); err == nil {
		t.Fatalf("must not create .githooks when core.hooksPath is already set")
	}
	if hp := gitConfig(t, dir, "core.hooksPath"); hp != "myhooks" {
		t.Fatalf("core.hooksPath overridden to %q, want myhooks", hp)
	}
}

func TestInstallGitHookIdempotent(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	hook := resolve.HookInfo{Name: "pre-push-validate", Kind: "git-hook", GitHook: "pre-push", Script: "pre-push-validate.sh"}

	if err := installGitHook(dir, hook); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	if err := installGitHook(dir, hook); err != nil {
		t.Fatalf("install 2: %v", err)
	}

	// No spurious pre-push.local created when re-running over our own hook.
	if _, err := os.Stat(filepath.Join(dir, ".githooks", "pre-push.local")); err == nil {
		t.Fatalf("idempotent re-run must not preserve our own hook as .local")
	}
}

// ---- installRepoHooks (manifest walk) ----

// setupToolkitWithManifest writes a manifest + the gate scripts into a temp
// FORGE_HOME and returns the repo dir to install into.
func setupToolkitWithManifest(t *testing.T, manifest string) string {
	t.Helper()
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	hooksDir := filepath.Join(forge, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"pre-push-validate.sh", "validate-gate.sh"} {
		if err := os.WriteFile(filepath.Join(hooksDir, s), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := t.TempDir()
	gitInit(t, repo)
	return repo
}

func TestInstallRepoHooksHonorsDefaultFalse(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"pre-push-validate","kind":"git-hook","gitHook":"pre-push","script":"pre-push-validate.sh","scope":"repo","default":true},
  {"name":"validate-gate","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Bash","script":"validate-gate.sh","scope":"repo","default":false}
]}`
	repo := setupToolkitWithManifest(t, manifest)

	installRepoHooks(repo, nil) // no opt-ins

	// default:true git hook installed.
	if _, err := os.Stat(filepath.Join(repo, ".githooks", "pre-push")); err != nil {
		t.Fatalf("default git hook should be installed: %v", err)
	}
	// default:false settings hook NOT installed.
	if _, err := os.Stat(filepath.Join(repo, ".claude", "settings.json")); err == nil {
		t.Fatalf("default:false settings hook must NOT be auto-installed")
	}
}

func TestInstallRepoHooksInstallsOptInHook(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"validate-gate","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Bash","script":"validate-gate.sh","scope":"repo","default":false}
]}`
	repo := setupToolkitWithManifest(t, manifest)

	installRepoHooks(repo, map[string]bool{"validate-gate": true})

	settingsPath := filepath.Join(repo, ".claude", "settings.json")
	got := preToolUseCommands(t, readJSON(t, settingsPath), "Bash")
	if len(got) != 1 {
		t.Fatalf("opted-in settings hook should be merged, got %v", got)
	}
	// And it must reference the absolute toolkit gate path.
	if !strings.HasSuffix(got[0], filepath.Join("hooks", "validate-gate.sh")) {
		t.Fatalf("command should point at toolkit gate, got %q", got[0])
	}
	// The toolkit gate script must be made executable.
	info, err := os.Stat(resolve.HookScriptPath("validate-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("gate script must be made executable, mode = %v", info.Mode())
	}
}

func TestInstallRepoHooksSkipsUnknownKind(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"weird","kind":"mystery","script":"x.sh","scope":"repo","default":true}
]}`
	repo := setupToolkitWithManifest(t, manifest)
	// Must not panic or error out; simply skips the unknown kind.
	installRepoHooks(repo, nil)
	if _, err := os.Stat(filepath.Join(repo, ".githooks", "pre-push")); err == nil {
		t.Fatalf("unknown kind should install nothing")
	}
}

// ---- installGlobalHooks (global-scoped claude-settings hooks) ----

func TestInstallGlobalHooksInstallsGlobalSettingsHook(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"ponytail-preload","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Edit|Write|MultiEdit|NotebookEdit","script":"validate-gate.sh","scope":"global","default":true}
]}`
	setupToolkitWithManifest(t, manifest) // sets FORGE_HOME + writes the scripts
	claudeDir := t.TempDir()

	installGlobalHooks(claudeDir, nil)

	settingsPath := filepath.Join(claudeDir, "settings.json")
	got := preToolUseCommands(t, readJSON(t, settingsPath), "Edit|Write|MultiEdit|NotebookEdit")
	if len(got) != 1 {
		t.Fatalf("global settings hook should be merged, got %v", got)
	}
	if !strings.HasSuffix(got[0], filepath.Join("hooks", "validate-gate.sh")) {
		t.Fatalf("command should point at the toolkit script, got %q", got[0])
	}
}

func TestInstallGlobalHooksSkipsRepoScopedHook(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"validate-gate","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Bash","script":"validate-gate.sh","scope":"repo","default":true}
]}`
	setupToolkitWithManifest(t, manifest)
	claudeDir := t.TempDir()

	installGlobalHooks(claudeDir, nil)

	if _, err := os.Stat(filepath.Join(claudeDir, "settings.json")); err == nil {
		t.Fatalf("repo-scoped hook must NOT be installed globally")
	}
}

func TestInstallGlobalHooksHonorsDefaultFalse(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"opt-in-global","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Edit","script":"validate-gate.sh","scope":"global","default":false}
]}`
	setupToolkitWithManifest(t, manifest)
	claudeDir := t.TempDir()

	installGlobalHooks(claudeDir, nil) // no opt-ins
	if _, err := os.Stat(filepath.Join(claudeDir, "settings.json")); err == nil {
		t.Fatalf("default:false global hook must NOT be auto-installed")
	}

	installGlobalHooks(claudeDir, map[string]bool{"opt-in-global": true})
	got := preToolUseCommands(t, readJSON(t, filepath.Join(claudeDir, "settings.json")), "Edit")
	if len(got) != 1 {
		t.Fatalf("opted-in global hook should be merged, got %v", got)
	}
}

func TestInstallRepoHooksSkipsGlobalScopedHook(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"ponytail-preload","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Edit|Write|MultiEdit|NotebookEdit","script":"validate-gate.sh","scope":"global","default":true}
]}`
	repo := setupToolkitWithManifest(t, manifest)

	installRepoHooks(repo, nil)

	if _, err := os.Stat(filepath.Join(repo, ".claude", "settings.json")); err == nil {
		t.Fatalf("global-scoped hook must NOT be installed per-repo")
	}
}

// ---- default-global scope for claude-settings hooks ----
// A claude-settings hook with NO scope (or scope:global) installs globally; only
// an explicit scope:repo keeps it per-repo. git-hooks are per-repo by nature.

func TestInstallGlobalHooksDefaultsNoScopeToGlobal(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"noscope","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Edit","script":"validate-gate.sh","default":true}
]}`
	setupToolkitWithManifest(t, manifest)
	claudeDir := t.TempDir()

	installGlobalHooks(claudeDir, nil)

	got := preToolUseCommands(t, readJSON(t, filepath.Join(claudeDir, "settings.json")), "Edit")
	if len(got) != 1 {
		t.Fatalf("no-scope claude-settings hook should default to global, got %v", got)
	}
}

func TestInstallRepoHooksSkipsNoScopeSettingsHook(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"noscope","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Edit","script":"validate-gate.sh","default":true}
]}`
	repo := setupToolkitWithManifest(t, manifest)

	installRepoHooks(repo, nil)

	if _, err := os.Stat(filepath.Join(repo, ".claude", "settings.json")); err == nil {
		t.Fatalf("no-scope claude-settings hook is global by default and must NOT install per-repo")
	}
}

func TestInstallRepoHooksInstallsNoScopeGitHook(t *testing.T) {
	manifest := `{"hooks":[
  {"name":"pre-push-validate","kind":"git-hook","gitHook":"pre-push","script":"pre-push-validate.sh","default":true}
]}`
	repo := setupToolkitWithManifest(t, manifest)

	installRepoHooks(repo, nil)

	if _, err := os.Stat(filepath.Join(repo, ".githooks", "pre-push")); err != nil {
		t.Fatalf("no-scope git-hook must still install per-repo (git hooks are per-repo by nature): %v", err)
	}
}

// ---- install-local-hook (settings.local.json target) ----

const localHookManifest = `{"hooks":[
  {"name":"validate-gate","kind":"claude-settings-hook","event":"PreToolUse","matcher":"Bash","script":"validate-gate.sh","scope":"repo","default":false},
  {"name":"pre-push-validate","kind":"git-hook","gitHook":"pre-push","script":"pre-push-validate.sh","scope":"repo","default":true}
]}`

func TestInstallLocalHookWritesLocalNotCommitted(t *testing.T) {
	repo := setupToolkitWithManifest(t, localHookManifest)

	if err := installLocalHook(repo, "validate-gate"); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Writes the gitignored LOCAL file...
	localPath := filepath.Join(repo, ".claude", "settings.local.json")
	got := preToolUseCommands(t, readJSON(t, localPath), "Bash")
	if len(got) != 1 || !strings.HasSuffix(got[0], filepath.Join("hooks", "validate-gate.sh")) {
		t.Fatalf("local hook should point at toolkit gate, got %v", got)
	}
	// ...and NEVER the committed settings.json.
	if _, err := os.Stat(filepath.Join(repo, ".claude", "settings.json")); err == nil {
		t.Fatalf("committed settings.json must be untouched by a local install")
	}
}

func TestInstallLocalHookPreservesExistingKeys(t *testing.T) {
	repo := setupToolkitWithManifest(t, localHookManifest)
	localPath := filepath.Join(repo, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "permissions": {"allow": ["Read"]},
  "hooks": {"PreToolUse": [{"matcher": "Edit", "hooks": [{"type":"command","command":"/keep/me.sh"}]}]}
}`
	if err := os.WriteFile(localPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installLocalHook(repo, "validate-gate"); err != nil {
		t.Fatalf("install: %v", err)
	}

	settings := readJSON(t, localPath)
	if _, ok := settings["permissions"]; !ok {
		t.Fatalf("permissions lost after local merge")
	}
	if edit := preToolUseCommands(t, settings, "Edit"); len(edit) != 1 || edit[0] != "/keep/me.sh" {
		t.Fatalf("existing Edit matcher clobbered, got %v", edit)
	}
	if bash := preToolUseCommands(t, settings, "Bash"); len(bash) != 1 {
		t.Fatalf("our Bash matcher not appended, got %v", bash)
	}
}

func TestInstallLocalHookIdempotent(t *testing.T) {
	repo := setupToolkitWithManifest(t, localHookManifest)
	for i := 0; i < 3; i++ {
		if err := installLocalHook(repo, "validate-gate"); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
	}
	localPath := filepath.Join(repo, ".claude", "settings.local.json")
	if got := preToolUseCommands(t, readJSON(t, localPath), "Bash"); len(got) != 1 {
		t.Fatalf("expected exactly 1 command after 3 installs (no dup), got %d: %v", len(got), got)
	}
}

func TestInstallLocalHookUnknownName(t *testing.T) {
	repo := setupToolkitWithManifest(t, localHookManifest)
	if err := installLocalHook(repo, "nope"); err == nil {
		t.Fatalf("expected error for a hook name absent from the manifest")
	}
}

func TestInstallLocalHookRejectsNonSettingsKind(t *testing.T) {
	repo := setupToolkitWithManifest(t, localHookManifest)
	// pre-push-validate is a git-hook, not a claude-settings-hook.
	if err := installLocalHook(repo, "pre-push-validate"); err == nil {
		t.Fatalf("expected error: only claude-settings-hook installs into settings.local.json")
	}
}

func TestIsGitIgnored(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	// Disable any machine-global excludes (e.g. ~/.config/git/ignore) so the test
	// is hermetic — only the repo's own .gitignore should decide the result here.
	if out, err := exec.Command("git", "-C", dir, "config", "core.excludesFile", "/dev/null").CombinedOutput(); err != nil {
		t.Fatalf("disable global excludes: %v\n%s", err, out)
	}
	rel := filepath.Join(".claude", "settings.local.json")

	if isGitIgnored(dir, rel) {
		t.Fatalf("path must not be reported ignored before a .gitignore rule exists")
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".claude/settings.local.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isGitIgnored(dir, rel) {
		t.Fatalf("path must be reported ignored once .gitignore covers it")
	}
}
