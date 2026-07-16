package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
)

// isProjectInitialized returns true if the current project has .claude/skills/ directory.
func isProjectInitialized() bool {
	info, err := os.Stat(filepath.Join(".claude", "skills"))
	return err == nil && info.IsDir()
}

// wireSkill symlinks a single skill into the current project's .claude/skills/ if the project
// has been initialized with forge init. No-op if not initialized.
func wireSkill(name string) {
	if !isProjectInitialized() {
		return
	}

	skillPath := resolve.ResolveSkill(name)
	if skillPath == "" {
		return
	}

	targetDir := filepath.Join(".claude", "skills", name)
	targetFile := filepath.Join(targetDir, "SKILL.md")

	// Already correctly symlinked
	if dest, err := os.Readlink(targetFile); err == nil && dest == skillPath {
		return
	}

	// Don't overwrite project-specific (non-symlink) skills
	if info, err := os.Lstat(targetFile); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return
	}

	os.Remove(targetFile)

	if err := os.Symlink(skillPath, targetFile); err != nil {
		return
	}

	ui.Log.Step(fmt.Sprintf("Wired %s into project", name))
	updateSkillsGitignore()
}

// unwireSkill removes a skill's symlink from the current project's .claude/skills/.
// Only removes symlinks, not project-specific files.
func unwireSkill(name string) {
	if !isProjectInitialized() {
		return
	}

	targetFile := filepath.Join(".claude", "skills", name, "SKILL.md")
	targetDir := filepath.Join(".claude", "skills", name)

	// Only remove if it's a symlink
	if info, err := os.Lstat(targetFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		os.Remove(targetFile)
		os.Remove(targetDir)
		ui.Log.Step(fmt.Sprintf("Unwired %s from project", name))
	}
	updateSkillsGitignore()
}

// wireAllSkills symlinks all toolkit skills into the current project's .claude/skills/.
// No-op if the project hasn't been initialized with forge init.
func wireAllSkills() {
	if !isProjectInitialized() {
		return
	}

	skills := resolve.ListSkills()
	wired := 0
	for _, skill := range skills {
		targetDir := filepath.Join(".claude", "skills", skill.Name)
		targetFile := filepath.Join(targetDir, "SKILL.md")

		// Already correctly symlinked
		if dest, err := os.Readlink(targetFile); err == nil && dest == skill.Path {
			continue
		}

		// Don't overwrite project-specific (non-symlink) skills
		if info, err := os.Lstat(targetFile); err == nil && info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			continue
		}

		os.Remove(targetFile)

		if err := os.Symlink(skill.Path, targetFile); err != nil {
			continue
		}

		ui.Log.Step(fmt.Sprintf("Wired %s into project", skill.Name))
		wired++
	}

	if wired > 0 {
		ui.Log.Success(fmt.Sprintf("%d new skill(s) wired into project.", wired))
	}

	// Clean up stale symlinks pointing to removed skills
	pruneStaleSkills()

	// Update gitignore for symlinked skills
	updateSkillsGitignore()
}

// pruneStaleSkills removes symlinks in .claude/skills/ that point to nonexistent targets.
func pruneStaleSkills() {
	pruneStaleSkillsIn(filepath.Join(".claude", "skills"))
}

// pruneStaleSkillsIn removes broken symlinks from any skills directory: both a
// dangling <name> directory link (current layout) and a dangling <name>/SKILL.md
// file link (the layout older installs wrote). Only symlinks are touched, so
// user-authored skills are never pruned.
func pruneStaleSkillsIn(skillsDir string) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		target := filepath.Join(skillsDir, entry.Name())
		info, err := os.Lstat(target)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			target = filepath.Join(target, "SKILL.md")
			if info, err = os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
				continue
			}
		}
		// os.Stat follows the link: an error means it resolves to nothing.
		if _, err := os.Stat(target); err != nil {
			os.Remove(target)
			os.Remove(filepath.Join(skillsDir, entry.Name())) // no-op unless now empty
			ui.Log.Step(fmt.Sprintf("Removed stale link: %s", entry.Name()))
		}
	}
}

// wireSkillsInto links canonical toolkit skill directories under skillsDir.
// Returns counts. Each entry is a DIRECTORY symlink to the canonical dir, so a
// skill's sibling resources (scripts/, references/) resolve through it and the
// canonical copy stays the only content.
// Behavior per skill:
//   - already correctly linked → no-op
//   - any symlink (even broken/stale) → ours, relink
//   - a forge-produced copy (old SKILL.md link, or identical content) → migrate
//   - empty dir → placeholder, safe to replace
//   - non-empty dir → user content, skip unless force=true
func wireSkillsInto(skillsDir string, skills []resolve.SkillInfo, force, verbose bool) (int, int) {
	installed, skipped := 0, 0
	for _, skill := range skills {
		targetDir := filepath.Join(skillsDir, skill.Name)
		sourceDir := filepath.Dir(skill.Path)

		if dest, err := os.Readlink(targetDir); err == nil && dest == sourceDir {
			if verbose {
				ui.Log.Step(fmt.Sprintf("%s (already linked)", skill.Name))
			}
			installed++
			continue
		}

		if info, err := os.Lstat(targetDir); err == nil {
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				os.Remove(targetDir) // a stale link of ours — relink below
			case force || isReplaceableForgeCopy(targetDir, sourceDir):
				os.RemoveAll(targetDir)
			default:
				// os.Remove only succeeds on an empty dir, so it clears a
				// placeholder and refuses to touch real user content.
				if err := os.Remove(targetDir); err != nil {
					if verbose {
						ui.Log.Step(fmt.Sprintf("%s (user-authored, skipped — use --force to replace)", skill.Name))
					}
					skipped++
					continue
				}
			}
		}

		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			ui.Log.Error(fmt.Sprintf("failed to create %s: %v", skillsDir, err))
			continue
		}
		if err := os.Symlink(sourceDir, targetDir); err != nil {
			ui.Log.Error(fmt.Sprintf("failed to symlink %s: %v", skill.Name, err))
			continue
		}
		if verbose {
			ui.Log.Success(fmt.Sprintf("%s → %s", skill.Name, ui.Dim(sourceDir)))
		}
		installed++
	}
	return installed, skipped
}

// isReplaceableForgeCopy reports whether dir is a skill directory forge itself
// produced, so migrating it to a canonical link destroys nothing and needs no
// --force. Two provable signatures:
//
//   - <name>/SKILL.md is a symlink INTO the toolkit — the layout older builds
//     wrote. A SKILL.md link pointing elsewhere belongs to another installer.
//   - <name>/SKILL.md is byte-identical to the canonical file — a copy made
//     before the toolkit was the single source. Identical content means there is
//     nothing to lose.
//
// Anything else may carry someone's edits and is preserved for --force.
func isReplaceableForgeCopy(dir, sourceDir string) bool {
	skillMD := filepath.Join(dir, "SKILL.md")
	if dest, err := os.Readlink(skillMD); err == nil {
		rel, relErr := filepath.Rel(resolve.SkillsDir(), dest)
		return relErr == nil && !strings.HasPrefix(rel, "..")
	}
	got, err := os.ReadFile(skillMD)
	if err != nil {
		return false
	}
	want, err := os.ReadFile(filepath.Join(sourceDir, "SKILL.md"))
	return err == nil && bytes.Equal(got, want)
}

// globalSkillRoots are the skill directories the supported backends read, both
// pointed at the same canonical toolkit dirs so no copy can drift:
//   - Claude Code reads ~/.claude/skills
//   - Codex reads ~/.agents/skills (NOT ~/.claude/skills)
//
// Both follow directory symlinks — verified against codex-cli 0.144.1, which
// also supersedes the "Codex ignores symlinks" finding behind the old
// ~/.codex/skills copies (see retireLegacyCodexCopies).
func globalSkillRoots(home string) []string {
	return []string{
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
}

// syncGlobalSkills is THE entry point every skill create/install/sync path uses
// to publish the toolkit: it links canonical skill dirs into each backend's
// root, prunes links whose skill is gone, and retires legacy Codex copies.
// Idempotent — reruns are byte-stable.
func syncGlobalSkills(force, verbose bool) (int, int) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0
	}
	skills := resolve.ListSkills()
	installed, skipped := 0, 0
	for _, root := range globalSkillRoots(home) {
		i, s := wireSkillsInto(root, skills, force, verbose)
		pruneStaleSkillsIn(root)
		installed, skipped = installed+i, skipped+s
	}
	retireLegacyCodexCopies()
	return installed, skipped
}

// wireAllSkillsGlobal re-syncs the global skill roots from the toolkit. Only
// acts if ~/.claude/skills/ exists (i.e. the user opted in with `forge init
// --global`) — otherwise it would create global config for someone who never
// asked. Never overwrites user-authored skills (no --force here).
func wireAllSkillsGlobal() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	info, err := os.Stat(filepath.Join(home, ".claude", "skills"))
	if err != nil || !info.IsDir() {
		return
	}
	installed, skipped := syncGlobalSkills(false, false)
	if installed > 0 {
		ui.Log.Success(fmt.Sprintf("Re-synced %d skill link(s) for Claude Code and Codex", installed))
	}
	// Never skip silently: a preserved directory means that backend keeps loading
	// its own copy instead of the toolkit's, which is exactly the drift we're here
	// to stop. Say so, and name the way out.
	if skipped > 0 {
		ui.Log.Warn(fmt.Sprintf("%d skill(s) kept their own copy and still differ from the toolkit — run 'forge init --global --force' to replace them", skipped))
	}
}

// updateSkillsGitignore scans .claude/skills/ for symlinked entries and writes
// a .gitignore that ignores them. Project-specific (non-symlink) skills are NOT
// ignored, so they can be committed normally.
func updateSkillsGitignore() {
	skillsDir := filepath.Join(".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}

	var symlinked []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		targetFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		info, err := os.Lstat(targetFile)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			symlinked = append(symlinked, entry.Name())
		}
	}

	gitignorePath := filepath.Join(skillsDir, ".gitignore")

	if len(symlinked) == 0 {
		// No symlinks — remove gitignore if it's ours
		os.Remove(gitignorePath)
		return
	}

	sort.Strings(symlinked)

	var sb strings.Builder
	sb.WriteString("# Forge-managed symlinks (personal toolkit — don't commit)\n")
	sb.WriteString("# Run 'forge init' to recreate these on your machine\n")
	for _, name := range symlinked {
		sb.WriteString(name + "/\n")
	}

	os.WriteFile(gitignorePath, []byte(sb.String()), 0o644)
}
