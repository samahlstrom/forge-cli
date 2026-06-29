package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samahlstrom/forge-cli/internal/resolve"
)

func TestWriteForgeSection(t *testing.T) {
	dir := t.TempDir()

	// Create from scratch.
	p := filepath.Join(dir, "CLAUDE.md")
	if err := writeForgeSection(p, "@agents.md", "CLAUDE.md"); err != nil {
		t.Fatalf("create: %v", err)
	}
	got := readFile(t, p)
	if !strings.Contains(got, forgeMarkerBegin) || !strings.Contains(got, "@agents.md") {
		t.Fatalf("created file missing forge block:\n%s", got)
	}

	// Prepend to existing non-forge content (preserve it).
	p2 := filepath.Join(dir, "EXISTING.md")
	if err := os.WriteFile(p2, []byte("# Project rules\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeForgeSection(p2, "@agents.md", "CLAUDE.md"); err != nil {
		t.Fatalf("prepend: %v", err)
	}
	got = readFile(t, p2)
	if !strings.Contains(got, "keep me") {
		t.Fatalf("prepend clobbered existing content:\n%s", got)
	}
	if strings.Index(got, forgeMarkerBegin) > strings.Index(got, "keep me") {
		t.Fatalf("forge block should be prepended before existing content:\n%s", got)
	}

	// Replace in place — must not duplicate the block.
	if err := writeForgeSection(p2, "@agents.md", "CLAUDE.md"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got = readFile(t, p2)
	if n := strings.Count(got, forgeMarkerBegin); n != 1 {
		t.Fatalf("expected exactly 1 forge block after replace, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "keep me") {
		t.Fatalf("replace clobbered existing content:\n%s", got)
	}
}

func TestGlobalForgeImportIsAbsoluteToolkitPath(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)

	want := "@" + filepath.Join(forge, "CLAUDE.md")
	if got := globalForgeImport(); got != want {
		t.Fatalf("global import = %q, want absolute toolkit path %q", got, want)
	}
	// The bug: a relative @AGENTS.md in ~/.claude/CLAUDE.md resolves to the user's
	// own ~/.claude/AGENTS.md, so the toolkit never loads. Guard against regressing.
	if got := globalForgeImport(); got == "@AGENTS.md" || got == "@agents.md" {
		t.Fatalf("global import is relative %q — resolves to ~/.claude/AGENTS.md, not the toolkit", got)
	}
}

func TestEnsureClaudeMDSectionInjectsImportInsideMarkersPreservingUserContent(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)

	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	user := "# How to talk to me\n\nKeep this exactly, byte for byte.\n"
	if err := os.WriteFile(claudeMD, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}

	imp := globalForgeImport()
	if err := ensureClaudeMDSectionAt(claudeMD, imp); err != nil {
		t.Fatalf("inject: %v", err)
	}
	got := readFile(t, claudeMD)

	// The absolute import lives INSIDE the forge markers.
	begin := strings.Index(got, forgeMarkerBegin)
	end := strings.Index(got, forgeMarkerEnd)
	if begin < 0 || end < 0 || begin > end {
		t.Fatalf("forge markers missing/disordered:\n%s", got)
	}
	if block := got[begin : end+len(forgeMarkerEnd)]; !strings.Contains(block, imp) {
		t.Fatalf("absolute import %q not inside forge markers:\n%s", imp, got)
	}

	// User content OUTSIDE the markers is preserved byte-for-byte.
	outside := got[:begin] + got[end+len(forgeMarkerEnd):]
	if !strings.Contains(outside, user) {
		t.Fatalf("user content not preserved byte-for-byte:\noutside=%q", outside)
	}
}

func TestEnsureCodexAgentsMDEmbedsLiteralManifest(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	const manifest = "# Forge Toolkit\n\n## Skills\n- /validate — example\n"
	if err := os.WriteFile(filepath.Join(forge, "agents.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsMD := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := ensureCodexAgentsMDAt(agentsMD); err != nil {
		t.Fatalf("embed: %v", err)
	}
	got := readFile(t, agentsMD)
	if !strings.Contains(got, "/validate — example") {
		t.Fatalf("Codex AGENTS.md missing literal manifest (Codex can't follow @import):\n%s", got)
	}
	if strings.Contains(got, "@agents.md") {
		t.Fatalf("Codex AGENTS.md must embed content, not an @import:\n%s", got)
	}
}

func TestEnsureCodexAgentsMDSkipsSymlink(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	if err := os.WriteFile(filepath.Join(forge, "agents.md"), []byte("MANIFEST\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A symlinked AGENTS.md (e.g. ./agents.md → toolkit) must NOT be written
	// through — that would clobber the toolkit's own agents.md.
	dir := t.TempDir()
	target := filepath.Join(dir, "toolkit-source.md")
	if err := os.WriteFile(target, []byte("ORIGINAL\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := ensureCodexAgentsMDAt(link); err != nil {
		t.Fatalf("symlink case: %v", err)
	}
	if got := readFile(t, target); got != "ORIGINAL\n" {
		t.Fatalf("symlink target was written through; want unchanged, got:\n%s", got)
	}
}

func TestWireCodexSkillsGlobal(t *testing.T) {
	// A toolkit skill on disk.
	toolkit := t.TempDir()
	skillDir := filepath.Join(toolkit, "validate")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: validate\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := []resolve.SkillInfo{{Name: "validate", Path: skillFile}}

	// Codex installed → skill is copied as REAL files (Codex doesn't follow
	// symlinks), with a forge-managed marker.
	codex := t.TempDir()
	t.Setenv("CODEX_HOME", codex)
	wireCodexSkillsGlobal(skills)
	copied := filepath.Join(codex, "skills", "validate", "SKILL.md")
	info, err := os.Lstat(copied)
	if err != nil {
		t.Fatalf("expected ~/.codex/skills/validate/SKILL.md: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("Codex skill must be a real file, not a symlink")
	}
	if got := readFile(t, copied); !strings.Contains(got, "name: validate") {
		t.Fatalf("copied SKILL.md missing content:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(codex, "skills", "validate", forgeManagedMarker)); err != nil {
		t.Fatalf("expected forge-managed marker: %v", err)
	}

	// A user-authored skill of the same name (no marker) must not be clobbered.
	userSkill := filepath.Join(codex, "skills", "mine")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	wireCodexSkillsGlobal([]resolve.SkillInfo{{Name: "mine", Path: skillFile}})
	if got := readFile(t, filepath.Join(userSkill, "SKILL.md")); got != "keep me" {
		t.Fatalf("clobbered a user-authored skill: %q", got)
	}

	// Codex not installed (config dir absent) → no-op, no skills dir created.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("CODEX_HOME", missing)
	wireCodexSkillsGlobal(skills)
	if _, err := os.Stat(filepath.Join(missing, "skills")); !os.IsNotExist(err) {
		t.Fatalf("should not create skills dir when Codex isn't installed")
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(data)
}

// After the toolkit rename agents.md -> AGENTS.md, the installer must still read
// the manifest (case-robust); on case-sensitive filesystems the old lowercase
// literal would miss it entirely.
func TestForgeManifestBodyReadsUppercaseAGENTS(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	if err := os.WriteFile(filepath.Join(forge, "AGENTS.md"), []byte("# Forge Toolkit\nUPPERCASE MANIFEST\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, ok := forgeManifestBody()
	if !ok {
		t.Fatal("forgeManifestBody: want ok reading AGENTS.md, got false")
	}
	if !strings.Contains(body, "UPPERCASE MANIFEST") {
		t.Fatalf("forgeManifestBody did not read AGENTS.md content:\n%s", body)
	}
}

// ensureClaudeMDSectionAt must inject the UPPERCASE "@AGENTS.md" import so it
// resolves on case-sensitive filesystems (Linux/CI), and must preserve existing
// content. (Lowercase "@agents.md" would dangle where the file is AGENTS.md.)
func TestEnsureClaudeMDSectionInjectsUppercaseImport(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte("# Project rules\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureClaudeMDSectionAt(claude, "@AGENTS.md"); err != nil {
		t.Fatalf("inject: %v", err)
	}
	got := readFile(t, claude)
	if !strings.Contains(got, "@AGENTS.md") || !strings.Contains(got, forgeMarkerBegin) {
		t.Fatalf("CLAUDE.md should get the uppercase @AGENTS.md import:\n%s", got)
	}
	if strings.Contains(got, "@agents.md") {
		t.Fatalf("CLAUDE.md must not inject lowercase @agents.md (dangles on case-sensitive FS):\n%s", got)
	}
	if !strings.Contains(got, "keep me") {
		t.Fatalf("injection clobbered existing content:\n%s", got)
	}
}
