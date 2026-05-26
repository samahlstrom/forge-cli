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

var globalFlag bool
var forceFlag bool

func init() {
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize forge skills in the current project (or globally)",
		Long: `Symlinks your toolkit's skills into .claude/skills/ so they're
available as slash commands in Claude Code.

Use --global to install skills into ~/.claude/skills/ instead, making
them available in every Claude Code session (CLI, Desktop, VS Code,
JetBrains).

Use --force to overwrite existing non-symlink skill directories
(useful when ~/.claude/skills/ contains stale copies from earlier
installs).

Skills are symlinked, not copied — running 'forge sync' updates them
everywhere automatically.`,
		RunE: runInit,
	}
	initCmd.Flags().BoolVarP(&globalFlag, "global", "g", false, "Install skills globally into ~/.claude/ (available in all projects and interfaces)")
	initCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Overwrite existing non-symlink skill directories")
	rootCmd.AddCommand(initCmd)
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

	if globalFlag {
		return runInitGlobal(skills)
	}
	return runInitLocal(skills)
}

func runInitGlobal(skills []resolve.SkillInfo) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not find home directory: %w", err)
	}

	claudeDir := filepath.Join(home, ".claude")
	skillsDir := filepath.Join(claudeDir, "skills")

	ui.Intro("Installing forge globally into ~/.claude/")

	installed, _ := wireSkillsInto(skillsDir, skills, forceFlag, true)
	pruneStaleSkillsIn(skillsDir)

	fmt.Println()
	if installed > 0 {
		ui.Log.Info(fmt.Sprintf("%d skill(s) installed globally — available in all Claude Code sessions.", installed))
	}

	// Inject forge section into ~/.claude/CLAUDE.md
	claudeMD := filepath.Join(claudeDir, "CLAUDE.md")
	if err := ensureClaudeMDSectionAt(claudeMD, skills); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update ~/.claude/CLAUDE.md: %v", err))
	}

	return nil
}

func runInitLocal(skills []resolve.SkillInfo) error {
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

	// Symlink global agents.md into project root
	ensureAgentsMDLink()

	// Inject forge section into CLAUDE.md
	if err := ensureClaudeMDSectionAt("CLAUDE.md", skills); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update CLAUDE.md: %v", err))
	}

	return nil
}

// ensureAgentsMDLink symlinks ~/.forge/agents.md → ./agents.md if not already present.
func ensureAgentsMDLink() {
	globalAgentsMD := filepath.Join(resolve.ForgeHome(), "agents.md")
	localAgentsMD := "agents.md"

	if _, err := os.Stat(globalAgentsMD); err != nil {
		return // global agents.md doesn't exist yet — forge sync will create it
	}
	if dest, err := os.Readlink(localAgentsMD); err == nil && dest == globalAgentsMD {
		ui.Log.Step("agents.md (already linked)")
		return
	}
	if info, err := os.Lstat(localAgentsMD); err == nil && info.Mode()&os.ModeSymlink == 0 {
		ui.Log.Step("agents.md (project-specific, skipped)")
		return
	}
	os.Remove(localAgentsMD)
	if err := os.Symlink(globalAgentsMD, localAgentsMD); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not link agents.md: %v", err))
		return
	}
	ui.Log.Success(fmt.Sprintf("agents.md → %s", ui.Dim(globalAgentsMD)))
}

// ensureClaudeMDSectionAt adds or updates a forge section in the given CLAUDE.md file.
// The section is kept minimal — just an @agents.md import that auto-loads the skill manifest.
func ensureClaudeMDSectionAt(claudeMD string, _ []resolve.SkillInfo) error {
	// Minimal forge section: just the @agents.md import
	forgeSection := forgeMarkerBegin + "\n@agents.md\n" + forgeMarkerEnd

	// Read existing CLAUDE.md or create one
	existing := ""
	if data, err := os.ReadFile(claudeMD); err == nil {
		existing = string(data)
	}

	// Replace existing section
	if strings.Contains(existing, forgeMarkerBegin) {
		beginIdx := strings.Index(existing, forgeMarkerBegin)
		endIdx := strings.Index(existing, forgeMarkerEnd)
		if endIdx > beginIdx {
			updated := existing[:beginIdx] + forgeSection + existing[endIdx+len(forgeMarkerEnd):]
			if err := os.WriteFile(claudeMD, []byte(updated), 0o644); err != nil {
				return err
			}
			ui.Log.Success("Updated CLAUDE.md (@agents.md import)")
			return nil
		}
	}

	// Prepend — @agents.md must load first so all agents get the toolkit manifest
	content := forgeSection + "\n"
	if existing != "" {
		content += "\n" + existing
	}

	if err := os.WriteFile(claudeMD, []byte(content), 0o644); err != nil {
		return err
	}

	if existing == "" {
		ui.Log.Success("Created CLAUDE.md with @agents.md import")
	} else {
		ui.Log.Success("Prepended @agents.md import to CLAUDE.md")
	}
	return nil
}
