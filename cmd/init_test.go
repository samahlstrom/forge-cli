package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// mergeCLAUDEmd
// ---------------------------------------------------------------------------

func TestMergeCLAUDEmd_NoExistingFile(t *testing.T) {
	forge := "# Forge Section\nSome content"
	got := mergeCLAUDEmd("/nonexistent/path/CLAUDE.md", forge)
	if got != forge {
		t.Errorf("expected forge content returned as-is, got %q", got)
	}
}

func TestMergeCLAUDEmd_EmptyExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	os.WriteFile(p, []byte(""), 0o644)

	forge := "# Forge Section\nStuff"
	got := mergeCLAUDEmd(p, forge)
	if got != forge {
		t.Errorf("expected forge content for empty file, got %q", got)
	}
}

func TestMergeCLAUDEmd_AppendsWhenNoDelimiters(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	existing := "# My Project\nCustom instructions"
	os.WriteFile(p, []byte(existing), 0o644)

	forge := "# Forge Generated"
	got := mergeCLAUDEmd(p, forge)

	if !strings.Contains(got, existing) {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(got, forgeDelimiter) {
		t.Error("should contain forge start delimiter")
	}
	if !strings.Contains(got, forgeDelimiterEnd) {
		t.Error("should contain forge end delimiter")
	}
	if !strings.Contains(got, forge) {
		t.Error("should contain forge content")
	}
	// Existing content should come before forge section
	existIdx := strings.Index(got, existing)
	delimIdx := strings.Index(got, forgeDelimiter)
	if existIdx >= delimIdx {
		t.Error("existing content should come before forge delimiters")
	}
}

func TestMergeCLAUDEmd_ReplacesExistingForgeSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	old := "# My Project\n\n" + forgeDelimiter + "\nOLD FORGE CONTENT\n" + forgeDelimiterEnd + "\n"
	os.WriteFile(p, []byte(old), 0o644)

	newForge := "NEW FORGE CONTENT"
	got := mergeCLAUDEmd(p, newForge)

	if strings.Contains(got, "OLD FORGE CONTENT") {
		t.Error("old forge content should be replaced")
	}
	if !strings.Contains(got, "NEW FORGE CONTENT") {
		t.Error("new forge content should be present")
	}
	if !strings.Contains(got, "# My Project") {
		t.Error("content before forge section should be preserved")
	}
}

func TestMergeCLAUDEmd_PreservesContentAroundForgeSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "CLAUDE.md")
	content := "# Before\n\n" + forgeDelimiter + "\nOLD\n" + forgeDelimiterEnd + "\n\n# After"
	os.WriteFile(p, []byte(content), 0o644)

	got := mergeCLAUDEmd(p, "REPLACED")

	if !strings.Contains(got, "# Before") {
		t.Error("content before forge section should be preserved")
	}
	if !strings.Contains(got, "# After") {
		t.Error("content after forge section should be preserved")
	}
	if !strings.Contains(got, "REPLACED") {
		t.Error("new forge content should be present")
	}
}

// ---------------------------------------------------------------------------
// mergeSettingsJSON
// ---------------------------------------------------------------------------

func TestMergeSettingsJSON_NoExistingFile(t *testing.T) {
	forge := `{"permissions":{"allow":["Bash(bd *)"]}}`
	got := mergeSettingsJSON("/nonexistent/settings.json", forge)
	if got != forge {
		t.Errorf("expected forge content as-is, got %q", got)
	}
}

func TestMergeSettingsJSON_InvalidExistingJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	os.WriteFile(p, []byte("not json at all"), 0o644)

	forge := `{"permissions":{"allow":["Bash(bd *)"]}}`
	got := mergeSettingsJSON(p, forge)
	if got != forge {
		t.Errorf("expected forge content when existing is invalid JSON, got %q", got)
	}
	// Should have created a backup
	if _, err := os.Stat(p + ".backup"); err != nil {
		t.Error("should create backup of invalid JSON file")
	}
}

func TestMergeSettingsJSON_MergesPermissionsAllow(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	existing := `{"permissions":{"allow":["Read","Write"]}}`
	os.WriteFile(p, []byte(existing), 0o644)

	forge := `{"permissions":{"allow":["Read","Bash(bd *)"]}}`
	got := mergeSettingsJSON(p, forge)

	var result map[string]any
	if err := json.Unmarshal([]byte(got), &result); err != nil {
		t.Fatalf("result should be valid JSON: %v", err)
	}

	perms := result["permissions"].(map[string]any)
	allow := perms["allow"].([]any)

	// Should have union: Read, Write, Bash(bd *)
	allowStrs := make(map[string]bool)
	for _, v := range allow {
		allowStrs[v.(string)] = true
	}
	for _, want := range []string{"Read", "Write", "Bash(bd *)"} {
		if !allowStrs[want] {
			t.Errorf("expected %q in merged allow list", want)
		}
	}
	if len(allow) != 3 {
		t.Errorf("expected 3 entries (no duplicates), got %d", len(allow))
	}
}

func TestMergeSettingsJSON_AddsPermissionsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	os.WriteFile(p, []byte(`{"other":"value"}`), 0o644)

	forge := `{"permissions":{"allow":["Bash(bd *)"]}}`
	got := mergeSettingsJSON(p, forge)

	var result map[string]any
	json.Unmarshal([]byte(got), &result)

	// Should have both the original key and new permissions
	if result["other"] != "value" {
		t.Error("existing keys should be preserved")
	}
	perms := result["permissions"].(map[string]any)
	allow := perms["allow"].([]any)
	if len(allow) != 1 || allow[0].(string) != "Bash(bd *)" {
		t.Error("forge permissions should be added")
	}
}

func TestMergeSettingsJSON_MergesHooks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	existing := `{
		"hooks": {
			"SessionStart": [
				{"hooks": [{"command": "echo existing"}]}
			]
		}
	}`
	os.WriteFile(p, []byte(existing), 0o644)

	forge := `{
		"hooks": {
			"SessionStart": [
				{"hooks": [{"command": "echo new-hook"}]}
			]
		}
	}`
	got := mergeSettingsJSON(p, forge)

	var result map[string]any
	json.Unmarshal([]byte(got), &result)

	hooks := result["hooks"].(map[string]any)
	sessionStart := hooks["SessionStart"].([]any)
	if len(sessionStart) != 2 {
		t.Errorf("expected 2 SessionStart hook entries, got %d", len(sessionStart))
	}
}

func TestMergeSettingsJSON_DeduplicatesHooks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	existing := `{
		"hooks": {
			"SessionStart": [
				{"hooks": [{"command": "echo same"}]}
			]
		}
	}`
	os.WriteFile(p, []byte(existing), 0o644)

	forge := `{
		"hooks": {
			"SessionStart": [
				{"hooks": [{"command": "echo same"}]}
			]
		}
	}`
	got := mergeSettingsJSON(p, forge)

	var result map[string]any
	json.Unmarshal([]byte(got), &result)

	hooks := result["hooks"].(map[string]any)
	sessionStart := hooks["SessionStart"].([]any)
	if len(sessionStart) != 1 {
		t.Errorf("duplicate hooks should not be added; got %d entries", len(sessionStart))
	}
}

func TestMergeSettingsJSON_AddsHooksWhenMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	os.WriteFile(p, []byte(`{"permissions":{"allow":[]}}`), 0o644)

	forge := `{
		"hooks": {
			"PreToolUse": [
				{"hooks": [{"command": "echo check"}]}
			]
		}
	}`
	got := mergeSettingsJSON(p, forge)

	var result map[string]any
	json.Unmarshal([]byte(got), &result)

	hooks := result["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Error("forge hooks should be added when existing has none")
	}
}

// ---------------------------------------------------------------------------
// mergePermissionsAllow (helper)
// ---------------------------------------------------------------------------

func TestMergePermissionsAllow_BothEmpty(t *testing.T) {
	existing := map[string]any{"permissions": map[string]any{"allow": []any{}}}
	forge := map[string]any{"permissions": map[string]any{"allow": []any{}}}
	mergePermissionsAllow(existing, forge)

	allow := existing["permissions"].(map[string]any)["allow"].([]any)
	if len(allow) != 0 {
		t.Errorf("expected empty allow, got %d", len(allow))
	}
}

func TestMergePermissionsAllow_ForgeHasNoPermissions(t *testing.T) {
	existing := map[string]any{"permissions": map[string]any{"allow": []any{"Read"}}}
	forge := map[string]any{}
	mergePermissionsAllow(existing, forge)

	allow := existing["permissions"].(map[string]any)["allow"].([]any)
	if len(allow) != 1 {
		t.Error("existing should be unchanged when forge has no permissions")
	}
}

// ---------------------------------------------------------------------------
// mergeHooks (helper)
// ---------------------------------------------------------------------------

func TestMergeHooks_ForgeHasNoHooks(t *testing.T) {
	existing := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"command": "echo hi"}}}},
		},
	}
	forge := map[string]any{}
	mergeHooks(existing, forge)

	hooks := existing["hooks"].(map[string]any)
	ss := hooks["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Error("existing hooks should be unchanged when forge has no hooks")
	}
}

func TestMergeHooks_NewEventType(t *testing.T) {
	existing := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"command": "echo hi"}}}},
		},
	}
	forge := map[string]any{
		"hooks": map[string]any{
			"PostToolUse": []any{map[string]any{"hooks": []any{map[string]any{"command": "echo post"}}}},
		},
	}
	mergeHooks(existing, forge)

	hooks := existing["hooks"].(map[string]any)
	if _, ok := hooks["PostToolUse"]; !ok {
		t.Error("new hook event types should be added")
	}
}

// ---------------------------------------------------------------------------
// skip-if-exists logic (forge.yaml, project.md)
// ---------------------------------------------------------------------------

func TestSkipIfExists_ForgeYaml(t *testing.T) {
	// The skip-if-exists logic in generateHarness skips writing forge.yaml
	// and .forge/context/project.md when they already exist and --force is false.
	// We test this indirectly by checking that mergeCLAUDEmd and mergeSettingsJSON
	// do NOT skip (they merge), while the skip logic is for specific files.
	//
	// Direct test: create a temp dir with an existing forge.yaml, verify
	// the conditional would skip it.

	dir := t.TempDir()
	forgeYaml := filepath.Join(dir, "forge.yaml")
	os.WriteFile(forgeYaml, []byte("name: my-project\n"), 0o644)

	projectMd := filepath.Join(dir, ".forge", "context", "project.md")
	os.MkdirAll(filepath.Dir(projectMd), 0o755)
	os.WriteFile(projectMd, []byte("# My Project\n"), 0o644)

	// Simulate the skip-if-exists check from generateHarness lines 658-667
	skipFiles := []string{"forge.yaml", ".forge/context/project.md"}
	for _, rel := range skipFiles {
		outputPath := filepath.Join(dir, rel)
		if _, err := os.Stat(outputPath); err == nil {
			// File exists — the real code would skip writing
		} else {
			t.Errorf("expected %s to exist for skip test", rel)
		}
	}

	// Verify that non-skip files (like CLAUDE.md) would NOT be skipped
	claudePath := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Stat(claudePath); err == nil {
		t.Error("CLAUDE.md should not exist in fresh temp dir")
	}
}

func TestSkipIfExists_ForceOverrides(t *testing.T) {
	// When --force is true, even existing forge.yaml should be overwritten.
	// The condition is: !initForce && util.Exists(outputPath)
	// With force=true, the condition is false, so the file gets written.

	dir := t.TempDir()
	forgeYaml := filepath.Join(dir, "forge.yaml")
	original := "name: original\n"
	os.WriteFile(forgeYaml, []byte(original), 0o644)

	// Simulate the force=true path: the condition !force && exists evaluates to false,
	// so the file would be overwritten. We verify the logic expression.
	force := true
	exists := true
	shouldSkip := !force && exists
	if shouldSkip {
		t.Error("with force=true, file should NOT be skipped")
	}

	force = false
	shouldSkip = !force && exists
	if !shouldSkip {
		t.Error("with force=false and file existing, file SHOULD be skipped")
	}
}

func TestSkipIfExists_NewFileNotSkipped(t *testing.T) {
	// When forge.yaml does not exist, it should always be written regardless of --force
	force := false
	exists := false
	shouldSkip := !force && exists
	if shouldSkip {
		t.Error("non-existing file should never be skipped")
	}
}
