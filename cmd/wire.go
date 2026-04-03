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
	skillsDir := filepath.Join(".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		targetFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")

		// Only touch symlinks
		info, err := os.Lstat(targetFile)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		// Check if the symlink target exists
		if _, err := os.Stat(targetFile); err != nil {
			// Broken symlink — remove it and its directory
			os.Remove(targetFile)
			os.Remove(filepath.Join(skillsDir, entry.Name()))
			ui.Log.Step(fmt.Sprintf("Removed stale link: %s", entry.Name()))
		}
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
