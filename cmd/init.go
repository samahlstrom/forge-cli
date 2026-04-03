package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Initialize forge skills in the current project",
		Long: `Symlinks your toolkit's skills into .claude/skills/ so they're
available as slash commands in Claude Code.

Skills are symlinked, not copied — running 'forge sync' updates them
everywhere automatically.`,
		RunE: runInit,
	})
}

func runInit(_ *cobra.Command, _ []string) error {
	if !resolve.IsSetup() {
		return fmt.Errorf("toolkit not installed — run 'forge setup' first")
	}

	skills := resolve.ListSkills()
	if len(skills) == 0 {
		ui.Log.Warn("No skills found in your toolkit.")
		return nil
	}

	ui.Intro("Initializing forge in current project")

	installed := 0
	for _, skill := range skills {
		targetDir := filepath.Join(".claude", "skills", skill.Name)
		targetFile := filepath.Join(targetDir, "SKILL.md")

		// Check if already correctly symlinked
		if dest, err := os.Readlink(targetFile); err == nil && dest == skill.Path {
			ui.Log.Step(fmt.Sprintf("%s (already linked)", skill.Name))
			installed++
			continue
		}

		// Don't overwrite project-specific (non-symlink) skills
		if info, err := os.Lstat(targetFile); err == nil && info.Mode()&os.ModeSymlink == 0 {
			ui.Log.Step(fmt.Sprintf("%s (project-specific, skipped)", skill.Name))
			installed++
			continue
		}

		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			ui.Log.Error(fmt.Sprintf("failed to create %s: %v", targetDir, err))
			continue
		}

		// Remove existing symlink before creating new one
		os.Remove(targetFile)

		if err := os.Symlink(skill.Path, targetFile); err != nil {
			ui.Log.Error(fmt.Sprintf("failed to symlink %s: %v", skill.Name, err))
			continue
		}

		ui.Log.Success(fmt.Sprintf("%s → %s", skill.Name, ui.Dim(skill.Path)))
		installed++
	}

	fmt.Println()
	if installed > 0 {
		ui.Log.Info(fmt.Sprintf("%d skill(s) available as slash commands in Claude Code.", installed))
	}
	return nil
}
