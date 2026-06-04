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

	// Codex installed → skill is wired (symlinked) so Codex can discover it.
	codex := t.TempDir()
	t.Setenv("CODEX_HOME", codex)
	wireCodexSkillsGlobal(skills)
	link := filepath.Join(codex, "skills", "validate", "SKILL.md")
	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("expected ~/.codex/skills/validate/SKILL.md symlink: %v", err)
	}
	if dest != skillFile {
		t.Fatalf("symlink points to %q, want %q", dest, skillFile)
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
