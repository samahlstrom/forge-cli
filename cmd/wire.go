package cmd

import (
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

// pruneStaleSkillsIn removes broken symlinks from any skills directory.
func pruneStaleSkillsIn(skillsDir string) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		targetFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")

		info, err := os.Lstat(targetFile)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		if _, err := os.Stat(targetFile); err != nil {
			os.Remove(targetFile)
			os.Remove(filepath.Join(skillsDir, entry.Name()))
			ui.Log.Step(fmt.Sprintf("Removed stale link: %s", entry.Name()))
		}
	}
}

// wireSkillsInto installs skills as symlinks under skillsDir. Returns counts.
// Behavior per skill:
//   - already correctly linked → no-op
//   - SKILL.md is a symlink (even broken) or the dir itself is a symlink → ours, replace
//   - regular file/dir → skip unless force=true, then replace
func wireSkillsInto(skillsDir string, skills []resolve.SkillInfo, force, verbose bool) (int, int) {
	installed, skipped := 0, 0
	for _, skill := range skills {
		targetDir := filepath.Join(skillsDir, skill.Name)
		targetFile := filepath.Join(targetDir, "SKILL.md")

		if dest, err := os.Readlink(targetFile); err == nil && dest == skill.Path {
			if verbose {
				ui.Log.Step(fmt.Sprintf("%s (already linked)", skill.Name))
			}
			installed++
			continue
		}

		linkInfo, linkErr := os.Lstat(targetFile)
		dirInfo, dirErr := os.Lstat(targetDir)

		isOurs := false
		if linkErr == nil && linkInfo.Mode()&os.ModeSymlink != 0 {
			isOurs = true
		} else if linkErr != nil && dirErr == nil && dirInfo.Mode()&os.ModeSymlink != 0 {
			isOurs = true
		}

		if !isOurs && linkErr == nil && !force {
			if verbose {
				ui.Log.Step(fmt.Sprintf("%s (project-specific, skipped — use --force to overwrite)", skill.Name))
			}
			skipped++
			continue
		}

		if linkErr == nil {
			os.Remove(targetFile)
		}
		// If targetDir itself is a symlink (e.g. broken link from old install), remove it.
		if dirErr == nil && dirInfo.Mode()&os.ModeSymlink != 0 {
			os.Remove(targetDir)
		} else if force && dirErr == nil && dirInfo.IsDir() && !isOurs {
			os.RemoveAll(targetDir)
		}

		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			ui.Log.Error(fmt.Sprintf("failed to create %s: %v", targetDir, err))
			continue
		}
		if err := os.Symlink(skill.Path, targetFile); err != nil {
			ui.Log.Error(fmt.Sprintf("failed to symlink %s: %v", skill.Name, err))
			continue
		}
		if verbose {
			ui.Log.Success(fmt.Sprintf("%s → %s", skill.Name, ui.Dim(skill.Path)))
		}
		installed++
	}
	return installed, skipped
}

// wireAllSkillsGlobal re-syncs ~/.claude/skills/ from the toolkit. Only acts
// if ~/.claude/skills/ exists (i.e. user previously ran `forge init --global`).
// Never overwrites non-symlink skills (no --force here).
func wireAllSkillsGlobal() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	skillsDir := filepath.Join(home, ".claude", "skills")
	info, err := os.Stat(skillsDir)
	if err != nil || !info.IsDir() {
		return
	}
	skills := resolve.ListSkills()
	installed, _ := wireSkillsInto(skillsDir, skills, false, false)
	pruneStaleSkillsIn(skillsDir)
	if installed > 0 {
		ui.Log.Success(fmt.Sprintf("Re-synced %d skill(s) into ~/.claude/skills/", installed))
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
