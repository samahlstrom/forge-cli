package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

const forgeMarkerBegin = "<!-- BEGIN FORGE INTEGRATION -->"
const forgeMarkerEnd = "<!-- END FORGE INTEGRATION -->"

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

	// Gitignore symlinked skills so they don't get committed
	updateSkillsGitignore()

	// Inject forge section into CLAUDE.md
	if err := ensureClaudeMDSection(skills); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update CLAUDE.md: %v", err))
	}

	return nil
}

// ensureClaudeMDSection adds or updates a forge section in the project's CLAUDE.md.
func ensureClaudeMDSection(skills []resolve.SkillInfo) error {
	claudeMD := "CLAUDE.md"

	// Build the forge section
	var sb strings.Builder
	sb.WriteString(forgeMarkerBegin + "\n")
	sb.WriteString("## Forge Toolkit\n\n")
	sb.WriteString("This project uses [forge](https://github.com/samahlstrom/forge-cli) — a portable AI agent toolkit.\n")
	sb.WriteString("Your personal toolkit lives at `~/.forge/` and is synced via `forge sync`.\n\n")
	sb.WriteString("### CLI Commands\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("forge list              # See all skills and agents\n")
	sb.WriteString("forge skill add <name>  # Create a new skill\n")
	sb.WriteString("forge skill remove <name> # Remove a skill\n")
	sb.WriteString("forge agent add <name>  # Create a new agent\n")
	sb.WriteString("forge agent remove <name> # Remove an agent\n")
	sb.WriteString("forge sync              # Pull/push toolkit changes\n")
	sb.WriteString("forge get <repo> <name> # Pull a skill from any repo\n")
	sb.WriteString("```\n\n")

	if len(skills) > 0 {
		sb.WriteString("### Available Skills\n\n")
		for _, s := range skills {
			sb.WriteString(fmt.Sprintf("- `/%s`\n", s.Name))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(forgeMarkerEnd)
	forgeSection := sb.String()

	// Read existing CLAUDE.md or create one
	existing := ""
	if data, err := os.ReadFile(claudeMD); err == nil {
		existing = string(data)
	}

	// Replace existing section or append
	if strings.Contains(existing, forgeMarkerBegin) {
		beginIdx := strings.Index(existing, forgeMarkerBegin)
		endIdx := strings.Index(existing, forgeMarkerEnd)
		if endIdx > beginIdx {
			updated := existing[:beginIdx] + forgeSection + existing[endIdx+len(forgeMarkerEnd):]
			if err := os.WriteFile(claudeMD, []byte(updated), 0o644); err != nil {
				return err
			}
			ui.Log.Success("Updated forge section in CLAUDE.md")
			return nil
		}
	}

	// Append to existing or create new
	content := existing
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	content += forgeSection + "\n"

	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		return err
	}

	if existing == "" {
		ui.Log.Success("Created CLAUDE.md with forge section")
	} else {
		ui.Log.Success("Added forge section to CLAUDE.md")
	}
	return nil
}
