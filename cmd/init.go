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

	// Inject forge section into ~/.claude/CLAUDE.md (Claude resolves @agents.md)
	claudeMD := filepath.Join(claudeDir, "CLAUDE.md")
	if err := ensureClaudeMDSectionAt(claudeMD, skills); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update ~/.claude/CLAUDE.md: %v", err))
	}

	// Wire skills into ~/.codex/skills/ so Codex auto-discovers them, and inject
	// the literal manifest into Codex's global AGENTS.md as an index.
	wireCodexSkillsGlobal(skills)
	injectCodexGlobal()

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

	// Inject forge section into CLAUDE.md (Claude resolves @agents.md)
	if err := ensureClaudeMDSectionAt("CLAUDE.md", skills); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update CLAUDE.md: %v", err))
	}

	// Embed the literal manifest into the project AGENTS.md for Codex. Skipped
	// automatically if AGENTS.md is the toolkit symlink (see ensureCodexAgentsMDAt).
	if err := ensureCodexAgentsMDAt("AGENTS.md"); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update AGENTS.md: %v", err))
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

// writeForgeSection adds or updates the forge marker block (with `body` as its
// contents) in the file at path. Replaces an existing block in place; otherwise
// prepends a new one so it loads first. `label` is used only for log output.
func writeForgeSection(path, body, label string) error {
	forgeSection := forgeMarkerBegin + "\n" + body + "\n" + forgeMarkerEnd

	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	// Replace an existing forge block in place.
	if strings.Contains(existing, forgeMarkerBegin) {
		beginIdx := strings.Index(existing, forgeMarkerBegin)
		endIdx := strings.Index(existing, forgeMarkerEnd)
		if endIdx > beginIdx {
			updated := existing[:beginIdx] + forgeSection + existing[endIdx+len(forgeMarkerEnd):]
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return err
			}
			ui.Log.Success(fmt.Sprintf("Updated %s (forge section)", label))
			return nil
		}
	}

	// Prepend — the forge block must load first so all agents get the manifest.
	content := forgeSection + "\n"
	if existing != "" {
		content += "\n" + existing
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	if existing == "" {
		ui.Log.Success(fmt.Sprintf("Created %s with forge section", label))
	} else {
		ui.Log.Success(fmt.Sprintf("Prepended forge section to %s", label))
	}
	return nil
}

// ensureClaudeMDSectionAt adds or updates a forge section in the given CLAUDE.md
// file. Claude resolves `@agents.md`, so the section is just that import.
func ensureClaudeMDSectionAt(claudeMD string, _ []resolve.SkillInfo) error {
	return writeForgeSection(claudeMD, "@agents.md", "CLAUDE.md")
}

// codexHome returns Codex's config dir ($CODEX_HOME, else ~/.codex). Empty if
// the home directory can't be resolved.
func codexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// forgeManifestBody returns the toolkit's agents.md content, embedded literally
// for agents (like Codex) that do NOT resolve `@agents.md` imports.
func forgeManifestBody() (string, bool) {
	data, err := os.ReadFile(filepath.Join(resolve.ForgeHome(), "agents.md"))
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(data), "\n"), true
}

// ensureCodexAgentsMDAt embeds the toolkit manifest (literal content, not an
// @import) into an AGENTS.md so Codex picks up the toolkit. Skips symlinked
// targets: on a case-insensitive filesystem `AGENTS.md` may resolve to the
// `agents.md` symlink that already points at the toolkit, and writing would
// follow the link and clobber the toolkit's own agents.md.
func ensureCodexAgentsMDAt(agentsMD string) error {
	if info, err := os.Lstat(agentsMD); err == nil && info.Mode()&os.ModeSymlink != 0 {
		ui.Log.Step(fmt.Sprintf("%s (symlinked to toolkit, skipped)", agentsMD))
		return nil
	}
	body, ok := forgeManifestBody()
	if !ok {
		return nil // no toolkit manifest yet — forge sync will create it
	}
	return writeForgeSection(agentsMD, body, "AGENTS.md")
}

// injectCodexGlobal writes the toolkit manifest into Codex's global AGENTS.md,
// but only if Codex's config dir already exists (don't presume Codex is used).
func injectCodexGlobal() {
	ch := codexHome()
	if ch == "" {
		return
	}
	if info, err := os.Stat(ch); err != nil || !info.IsDir() {
		return
	}
	if err := ensureCodexAgentsMDAt(filepath.Join(ch, "AGENTS.md")); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update %s: %v", filepath.Join(ch, "AGENTS.md"), err))
	}
}

// wireCodexSkillsGlobal symlinks toolkit skills into ~/.codex/skills/ so Codex
// auto-discovers them, exactly as we do for ~/.claude/skills/. Codex uses the
// same <name>/SKILL.md layout as Claude. No-op if Codex isn't installed. The
// AGENTS.md manifest (injectCodexGlobal) is just an index — this is what makes
// the skills actually loadable.
func wireCodexSkillsGlobal(skills []resolve.SkillInfo) {
	ch := codexHome()
	if ch == "" {
		return
	}
	if info, err := os.Stat(ch); err != nil || !info.IsDir() {
		return // Codex not installed
	}
	skillsDir := filepath.Join(ch, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return
	}
	installed, _ := wireSkillsInto(skillsDir, skills, false, false)
	pruneStaleSkillsIn(skillsDir)
	if installed > 0 {
		ui.Log.Success(fmt.Sprintf("Wired %d skill(s) into ~/.codex/skills/", installed))
	}
}
