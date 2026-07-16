package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newGlobalToolkit extends newToolkit (temp FORGE_HOME, no git repo so
// commitAndPush no-ops) with an isolated HOME and CODEX_HOME plus the named
// canonical skills. Every test in this file runs against temp roots only —
// nothing here may touch the real ~/.claude, ~/.agents, ~/.codex or ~/.forge.
func newGlobalToolkit(t *testing.T, names ...string) (home string, forge string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	forge = newToolkit(t)

	for _, name := range names {
		dir := filepath.Join(forge, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: Canonical " + name + " skill. Second sentence.\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home, forge
}

// canonicalDir is the toolkit directory a link must resolve to.
func canonicalDir(forge, name string) string {
	return filepath.Join(forge, "skills", name)
}

// TestSyncGlobalSkillsLinksBothBackendsToCanonical is the core parity contract:
// Claude (~/.claude/skills) and Codex (~/.agents/skills) must each resolve to the
// SAME canonical toolkit directory. Directory symlinks (not SKILL.md file links)
// so a skill's sibling resources travel with it.
//
// The roots are spelled out literally rather than taken from globalSkillRoots():
// deriving them from the code under test would make this pass even if a backend
// were dropped entirely.
func TestSyncGlobalSkillsLinksBothBackendsToCanonical(t *testing.T) {
	home, forge := newGlobalToolkit(t, "forge", "validate")

	syncGlobalSkills(false, false)

	claudeRoot := filepath.Join(home, ".claude", "skills") // Claude Code reads this
	codexRoot := filepath.Join(home, ".agents", "skills")  // Codex reads this
	for _, root := range []string{claudeRoot, codexRoot} {
		for _, name := range []string{"forge", "validate"} {
			link := filepath.Join(root, name)
			dest, err := os.Readlink(link)
			if err != nil {
				t.Fatalf("%s is not a symlink: %v", link, err)
			}
			if want := canonicalDir(forge, name); dest != want {
				t.Fatalf("%s -> %q, want canonical %q", link, dest, want)
			}
			// The link must actually resolve to readable canonical content.
			data, err := os.ReadFile(filepath.Join(link, "SKILL.md"))
			if err != nil || !strings.Contains(string(data), "Canonical "+name) {
				t.Fatalf("%s does not resolve to canonical content: %v", link, err)
			}
		}
	}
}

// TestSyncGlobalSkillsIsIdempotent proves repeated syncs are byte-stable: no
// duplicate entries, no relink churn, identical link sets every run.
func TestSyncGlobalSkillsIsIdempotent(t *testing.T) {
	home, _ := newGlobalToolkit(t, "forge", "validate")

	syncGlobalSkills(false, false)
	first := snapshotRoots(t, home)
	syncGlobalSkills(false, false)
	syncGlobalSkills(false, false)
	second := snapshotRoots(t, home)

	if first != second {
		t.Fatalf("sync is not byte-stable across reruns:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	// Exactly one entry per skill per root — no duplicates.
	for _, root := range globalSkillRoots(home) {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("%s has %d entries, want exactly 2 (no duplicates): %v", root, len(entries), entries)
		}
	}
}

// snapshotRoots renders every global root entry and its link target, so any
// churn between runs shows up as a diff.
func snapshotRoots(t *testing.T, home string) string {
	t.Helper()
	var b strings.Builder
	for _, root := range globalSkillRoots(home) {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			p := filepath.Join(root, e.Name())
			dest, err := os.Readlink(p)
			if err != nil {
				dest = "<not-a-link>"
			}
			b.WriteString(p + " -> " + dest + "\n")
		}
	}
	return b.String()
}

// TestSyncGlobalSkillsRepairsEmptyPlaceholder covers the real machine state:
// ~/.agents/skills held empty <name>/ placeholder dirs that shadowed canonical
// skills. An empty dir holds no user content, so it is safe to replace.
func TestSyncGlobalSkillsRepairsEmptyPlaceholder(t *testing.T) {
	home, forge := newGlobalToolkit(t, "forge")
	placeholder := filepath.Join(home, ".agents", "skills", "forge")
	if err := os.MkdirAll(placeholder, 0o755); err != nil {
		t.Fatal(err)
	}

	syncGlobalSkills(false, false)

	dest, err := os.Readlink(placeholder)
	if err != nil {
		t.Fatalf("empty placeholder was not repaired into a symlink: %v", err)
	}
	if want := canonicalDir(forge, "forge"); dest != want {
		t.Fatalf("placeholder -> %q, want %q", dest, want)
	}
}

// TestSyncGlobalSkillsMigratesLegacyFileSymlinkLayout is THE upgrade path for
// every existing install: older builds wrote a real <name>/ dir holding SKILL.md
// as a FILE symlink into the toolkit. That dir is ours, not user content, so it
// must migrate to a directory link without needing --force — otherwise it looks
// "non-empty", gets skipped, and no existing machine ever converts.
func TestSyncGlobalSkillsMigratesLegacyFileSymlinkLayout(t *testing.T) {
	home, forge := newGlobalToolkit(t, "forge")
	canonical := canonicalDir(forge, "forge")

	for _, root := range globalSkillRoots(home) {
		legacyDir := filepath.Join(root, "forge")
		if err := os.MkdirAll(legacyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(canonical, "SKILL.md"), filepath.Join(legacyDir, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}

	syncGlobalSkills(false, false) // no --force: this is a routine sync

	for _, root := range globalSkillRoots(home) {
		link := filepath.Join(root, "forge")
		dest, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("legacy SKILL.md-link layout was not migrated in %s: %v", root, err)
		}
		if dest != canonical {
			t.Fatalf("%s -> %q, want canonical dir %q", link, dest, canonical)
		}
	}
}

// TestSyncGlobalSkillsRelinksIdenticalCopy: a copy whose SKILL.md is byte-identical
// to the canonical file is provably ours — replacing it with a link to canonical
// loses nothing, so a routine sync self-heals it without --force. This is the bulk
// of an existing install: copies made before the toolkit became canonical.
func TestSyncGlobalSkillsRelinksIdenticalCopy(t *testing.T) {
	home, forge := newGlobalToolkit(t, "forge")
	canonical := canonicalDir(forge, "forge")
	body, err := os.ReadFile(filepath.Join(canonical, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	copyDir := filepath.Join(home, ".claude", "skills", "forge")
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	syncGlobalSkills(false, false)

	dest, err := os.Readlink(copyDir)
	if err != nil || dest != canonical {
		t.Fatalf("identical copy was not relinked to canonical: %q, %v", dest, err)
	}
}

// TestSyncGlobalSkillsPreservesDriftedCopy: once a copy's content DIFFERS from
// canonical it may carry someone's edits, so it is preserved and left for the
// explicit --force path rather than silently overwritten.
func TestSyncGlobalSkillsPreservesDriftedCopy(t *testing.T) {
	home, _ := newGlobalToolkit(t, "forge")
	copyDir := filepath.Join(home, ".claude", "skills", "forge")
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	drifted := "---\nname: forge\ndescription: edited by hand\n---\n"
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}

	syncGlobalSkills(false, false)

	got, err := os.ReadFile(filepath.Join(copyDir, "SKILL.md"))
	if err != nil || string(got) != drifted {
		t.Fatalf("drifted copy was overwritten without --force: %q, %v", got, err)
	}
}

// TestSyncGlobalSkillsForceReplacesDriftedCopy: --force is the explicit path.
func TestSyncGlobalSkillsForceReplacesDriftedCopy(t *testing.T) {
	home, forge := newGlobalToolkit(t, "forge")
	copyDir := filepath.Join(home, ".claude", "skills", "forge")
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte("drifted"), 0o644); err != nil {
		t.Fatal(err)
	}

	syncGlobalSkills(true, false)

	if dest, err := os.Readlink(copyDir); err != nil || dest != canonicalDir(forge, "forge") {
		t.Fatalf("--force did not replace drifted copy: %q, %v", dest, err)
	}
}

// TestSyncGlobalSkillsPreservesForeignSymlinkedSkill: a <name>/SKILL.md link that
// points somewhere OTHER than the toolkit belongs to another installer. Only our
// own legacy layout may be migrated.
func TestSyncGlobalSkillsPreservesForeignSymlinkedSkill(t *testing.T) {
	home, _ := newGlobalToolkit(t, "forge")
	foreign := filepath.Join(t.TempDir(), "other-tool")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	foreignSkill := filepath.Join(foreign, "SKILL.md")
	if err := os.WriteFile(foreignSkill, []byte("---\nname: forge\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agents", "skills", "forge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreignSkill, filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	syncGlobalSkills(false, false)

	dest, err := os.Readlink(filepath.Join(dir, "SKILL.md"))
	if err != nil || dest != foreignSkill {
		t.Fatalf("another installer's skill was hijacked: %q, %v", dest, err)
	}
}

// TestSyncGlobalSkillsPreservesUserAuthoredSkill: a non-empty directory we did
// not create is user content. Without --force it must survive untouched.
func TestSyncGlobalSkillsPreservesUserAuthoredSkill(t *testing.T) {
	home, _ := newGlobalToolkit(t, "forge")
	mine := filepath.Join(home, ".agents", "skills", "forge")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: forge\ndescription: my own hand-written skill\n---\n"
	if err := os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	syncGlobalSkills(false, false)

	got, err := os.ReadFile(filepath.Join(mine, "SKILL.md"))
	if err != nil {
		t.Fatalf("user-authored skill was destroyed: %v", err)
	}
	if string(got) != body {
		t.Fatalf("user-authored skill was modified:\n%s", got)
	}
}

// TestSyncGlobalSkillsUnrelatedSkillsUntouched: skills that are not part of the
// toolkit (plugin installs, other tools) must never be pruned or relinked.
func TestSyncGlobalSkillsUnrelatedSkillsUntouched(t *testing.T) {
	home, _ := newGlobalToolkit(t, "forge")
	for _, root := range globalSkillRoots(home) {
		dir := filepath.Join(root, "plugin-only")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: plugin-only\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	syncGlobalSkills(false, false)

	for _, root := range globalSkillRoots(home) {
		if _, err := os.Stat(filepath.Join(root, "plugin-only", "SKILL.md")); err != nil {
			t.Fatalf("unrelated skill removed from %s: %v", root, err)
		}
	}
}

// TestRetireLegacyCodexCopiesRemovesForgeCopiesOnly proves the migration:
// Codex reads BOTH ~/.codex/skills and ~/.agents/skills and does not dedupe by
// name, so a leftover Forge copy loads as a second, drifting version of the same
// skill. Every Forge footprint goes; everything else stays.
func TestRetireLegacyCodexCopiesRemovesForgeCopiesOnly(t *testing.T) {
	home, _ := newGlobalToolkit(t, "validate", "linear", "forge")
	codexSkills := filepath.Join(home, ".codex", "skills")

	write := func(dir, body string, marker bool) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if marker {
			if err := os.WriteFile(filepath.Join(dir, forgeManagedMarker), []byte("forge\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	// 1. marked copy from a newer Forge build.
	write(filepath.Join(codexSkills, "validate"), "---\nname: validate\ndescription: stale\n---\n", true)
	// 2. unmarked copy from an older Forge build (marker predates it).
	write(filepath.Join(codexSkills, "linear"), "---\nname: linear\ndescription: stale\n---\n", false)
	// 3. Forge's own in-place archive — still discovered by Codex under
	//    frontmatter name "forge", so it duplicates the canonical skill.
	write(filepath.Join(codexSkills, "forge.stale.20260708140626"), "---\nname: forge\ndescription: archived\n---\n", false)
	// 4. a user/plugin skill Forge never managed.
	write(filepath.Join(codexSkills, "plugin-only"), "---\nname: plugin-only\ndescription: mine\n---\n", false)
	// 5. Codex's own bundled system skills.
	write(filepath.Join(codexSkills, ".system", "imagegen"), "---\nname: imagegen\n---\n", false)

	retireLegacyCodexCopies()

	for _, gone := range []string{"validate", "linear", "forge.stale.20260708140626"} {
		if _, err := os.Lstat(filepath.Join(codexSkills, gone)); !os.IsNotExist(err) {
			t.Fatalf("legacy Forge copy %q still discoverable by Codex: %v", gone, err)
		}
	}
	for _, kept := range []string{"plugin-only", ".system/imagegen"} {
		if _, err := os.Stat(filepath.Join(codexSkills, kept, "SKILL.md")); err != nil {
			t.Fatalf("non-Forge entry %q was destroyed: %v", kept, err)
		}
	}
}

// TestEnsureToolkitClaudeMDImportCreatesPortableImport encodes Sam's invariant —
// Claude and Codex ingest IDENTICAL instructions — and the Windows portability
// exception. CLAUDE.md is a one-line "@AGENTS.md" import, NEVER a git symlink:
// the toolkit is a git repo that syncs to Sam's Windows machine, where a symlink
// degrades to a broken text file (see PR #12 / 2568e66).
func TestEnsureToolkitClaudeMDImportCreatesPortableImport(t *testing.T) {
	_, forge := newGlobalToolkit(t, "forge")
	if err := os.WriteFile(filepath.Join(forge, "AGENTS.md"), []byte("# Forge Toolkit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(forge, "CLAUDE.md")

	ensureToolkitClaudeMDImport()

	info, err := os.Lstat(claudeMD)
	if err != nil {
		t.Fatalf("toolkit CLAUDE.md was not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("toolkit CLAUDE.md is a symlink — breaks on Sam's Windows checkout (PR #12)")
	}
	if got, _ := os.ReadFile(claudeMD); string(got) != "@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md = %q, want the portable one-line import %q", got, "@AGENTS.md\n")
	}
}

// TestEnsureToolkitClaudeMDImportReplacesLegacySymlink is the migration that
// matters: a CLAUDE.md -> AGENTS.md symlink is the pre-PR#12 layout, and it is
// exactly what breaks on Sam's Windows checkout (a committed symlink degrades to
// a broken text file). It must be converted to the portable import.
//
// os.ReadFile FOLLOWS a symlink, so a content check alone reads AGENTS.md's text,
// decides it isn't the managed one-liner, and silently leaves the symlink in place.
func TestEnsureToolkitClaudeMDImportReplacesLegacySymlink(t *testing.T) {
	_, forge := newGlobalToolkit(t, "forge")
	if err := os.WriteFile(filepath.Join(forge, "AGENTS.md"), []byte("# Forge Toolkit\nlots of content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(forge, "CLAUDE.md")
	if err := os.Symlink("AGENTS.md", claudeMD); err != nil {
		t.Fatal(err)
	}

	ensureToolkitClaudeMDImport()

	info, err := os.Lstat(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("legacy CLAUDE.md symlink was not converted — still breaks on Windows")
	}
	if got, _ := os.ReadFile(claudeMD); string(got) != toolkitClaudeMDImport {
		t.Fatalf("CLAUDE.md = %q, want %q", got, toolkitClaudeMDImport)
	}
	// The canonical file must survive the conversion untouched.
	if got, _ := os.ReadFile(filepath.Join(forge, "AGENTS.md")); !strings.Contains(string(got), "lots of content") {
		t.Fatalf("AGENTS.md was clobbered through the link: %q", got)
	}
}

// TestEnsureToolkitClaudeMDImportLeavesForeignSymlink: only a link to the
// toolkit's own AGENTS.md is ours to repair.
func TestEnsureToolkitClaudeMDImportLeavesForeignSymlink(t *testing.T) {
	_, forge := newGlobalToolkit(t, "forge")
	claudeMD := filepath.Join(forge, "CLAUDE.md")
	if err := os.Symlink("/somewhere/else/NOTES.md", claudeMD); err != nil {
		t.Fatal(err)
	}

	ensureToolkitClaudeMDImport()

	dest, err := os.Readlink(claudeMD)
	if err != nil || dest != "/somewhere/else/NOTES.md" {
		t.Fatalf("clobbered a link we do not own: %q, %v", dest, err)
	}
}

// TestEnsureToolkitClaudeMDImportIsIdempotent: repeated runs are byte-stable.
func TestEnsureToolkitClaudeMDImportIsIdempotent(t *testing.T) {
	_, forge := newGlobalToolkit(t, "forge")
	claudeMD := filepath.Join(forge, "CLAUDE.md")

	ensureToolkitClaudeMDImport()
	first, _ := os.ReadFile(claudeMD)
	ensureToolkitClaudeMDImport()
	second, _ := os.ReadFile(claudeMD)

	if string(first) != string(second) {
		t.Fatalf("not idempotent: %q then %q", first, second)
	}
}

// TestEnsureToolkitClaudeMDImportPreservesCustomContent: only a missing file or
// the exact known managed wrapper is repaired. Anything the user wrote stays.
func TestEnsureToolkitClaudeMDImportPreservesCustomContent(t *testing.T) {
	_, forge := newGlobalToolkit(t, "forge")
	claudeMD := filepath.Join(forge, "CLAUDE.md")
	custom := "# my own notes\n\n@AGENTS.md\n\nmore of my content\n"
	if err := os.WriteFile(claudeMD, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	ensureToolkitClaudeMDImport()

	got, _ := os.ReadFile(claudeMD)
	if string(got) != custom {
		t.Fatalf("clobbered user-authored CLAUDE.md:\ngot:  %q\nwant: %q", got, custom)
	}
}

// TestSkillAddPublishesToBothBackends: creating a skill runs the same single
// sync entry point, so a brand-new skill is immediately visible to Claude and
// Codex without a separate command.
func TestSkillAddPublishesToBothBackends(t *testing.T) {
	home, forge := newGlobalToolkit(t, "existing")
	// A global install is what opts the user in; both roots already exist.
	for _, root := range globalSkillRoots(home) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(forge, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { skillBody = "" })
	skillBody = "---\nname: brand-new\ndescription: Canonical brand-new skill.\n---\n"
	if err := runSkillAdd(nil, []string{"brand-new"}); err != nil {
		t.Fatalf("runSkillAdd: %v", err)
	}

	for _, root := range globalSkillRoots(home) {
		link := filepath.Join(root, "brand-new")
		dest, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("new skill not published to %s: %v", root, err)
		}
		if want := canonicalDir(forge, "brand-new"); dest != want {
			t.Fatalf("%s -> %q, want %q", link, dest, want)
		}
	}
}

// TestSyncGlobalSkillsSkipsWhenNotGloballyInstalled: never create global roots
// for a user who never opted into a global install.
func TestWireAllSkillsGlobalNoopWithoutGlobalInstall(t *testing.T) {
	home, _ := newGlobalToolkit(t, "forge")

	wireAllSkillsGlobal()

	if _, err := os.Stat(filepath.Join(home, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatalf("created ~/.agents/skills without a global install: %v", err)
	}
}

// TestSyncGlobalSkillsPrunesRemovedSkill: a link whose canonical dir is gone is
// removed, so a deleted skill stops resolving for both backends.
func TestSyncGlobalSkillsPrunesRemovedSkill(t *testing.T) {
	home, forge := newGlobalToolkit(t, "forge", "doomed")
	syncGlobalSkills(false, false)
	if err := os.RemoveAll(canonicalDir(forge, "doomed")); err != nil {
		t.Fatal(err)
	}

	syncGlobalSkills(false, false)

	for _, root := range globalSkillRoots(home) {
		if _, err := os.Lstat(filepath.Join(root, "doomed")); !os.IsNotExist(err) {
			t.Fatalf("broken link to removed skill survived in %s: %v", root, err)
		}
		if _, err := os.Readlink(filepath.Join(root, "forge")); err != nil {
			t.Fatalf("pruning removed a live skill from %s: %v", root, err)
		}
	}
}
