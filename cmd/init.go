package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

// forgeManagedMarker marks a Codex skill directory as one we copied (vs. a
// user-authored skill of the same name), so refresh/prune only touches ours.
const forgeManagedMarker = ".forge-managed"

const forgeMarkerBegin = "<!-- BEGIN FORGE INTEGRATION -->"
const forgeMarkerEnd = "<!-- END FORGE INTEGRATION -->"

var globalFlag bool
var forceFlag bool
var enableHookFlags []string

func init() {
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize forge skills in the current project (or globally)",
		Long: `Symlinks your toolkit's skills into .claude/skills/ so they're
available as slash commands in Claude Code, and installs the toolkit's
default hooks (e.g. the git pre-push validation gate) from the hooks
manifest.

Use --global to install skills into ~/.claude/skills/ instead, making
them available in every Claude Code session (CLI, Desktop, VS Code,
JetBrains).

Use --force to overwrite existing non-symlink skill directories
(useful when ~/.claude/skills/ contains stale copies from earlier
installs).

Use --enable-hook <name> (repeatable) to install an opt-in hook that is
off by default in the manifest (e.g. the leaky PreToolUse validate-gate).

Skills are symlinked, not copied — running 'forge sync' updates them
everywhere automatically.`,
		RunE: runInit,
	}
	initCmd.Flags().BoolVarP(&globalFlag, "global", "g", false, "Install skills globally into ~/.claude/ (available in all projects and interfaces)")
	initCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Overwrite existing non-symlink skill directories")
	initCmd.Flags().StringSliceVar(&enableHookFlags, "enable-hook", nil, "Install an opt-in (default:false) hook by name; repeatable")
	rootCmd.AddCommand(initCmd)
}

// enabledHooks returns the set of opt-in hooks requested via --enable-hook.
func enabledHooks() map[string]bool {
	if len(enableHookFlags) == 0 {
		return nil
	}
	m := make(map[string]bool, len(enableHookFlags))
	for _, name := range enableHookFlags {
		m[strings.TrimSpace(name)] = true
	}
	return m
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

	// Install repo-scoped hooks from the toolkit manifest (git pre-push gate by
	// default; opt-in hooks only when requested via --enable-hook).
	installRepoHooks(".", enabledHooks())

	return nil
}

// ensureAgentsMDLink symlinks ~/.forge/agents.md → ./agents.md if not already present.
func ensureAgentsMDLink() {
	globalAgentsMD := resolve.ToolkitManifestPath()
	localAgentsMD := "agents.md" // local link name; Claude's `@agents.md` import resolves this

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
// file. Claude resolves the `@AGENTS.md` import, so the section is just that
// import. Uppercase so it resolves on case-sensitive filesystems (Linux/CI),
// where the toolkit/source file is AGENTS.md.
func ensureClaudeMDSectionAt(claudeMD string, _ []resolve.SkillInfo) error {
	return writeForgeSection(claudeMD, "@AGENTS.md", "CLAUDE.md")
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
	data, err := os.ReadFile(resolve.ToolkitManifestPath())
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

// wireCodexSkillsGlobal copies toolkit skills into ~/.codex/skills/ so Codex
// auto-discovers them. Codex uses the same <name>/SKILL.md layout as Claude but,
// unlike Claude, its loader does NOT follow symlinks — so these must be real file
// copies (matching Codex's own skill-installer). No-op if Codex isn't installed.
// The AGENTS.md manifest (injectCodexGlobal) is just an index; this is what makes
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

	wanted := map[string]bool{}
	copied := 0
	for _, s := range skills {
		wanted[s.Name] = true
		dst := filepath.Join(skillsDir, s.Name)
		// Don't clobber a user-authored skill of the same name (no marker).
		if _, err := os.Stat(dst); err == nil {
			if _, mErr := os.Stat(filepath.Join(dst, forgeManagedMarker)); mErr != nil {
				continue
			}
		}
		if err := copySkillDir(filepath.Dir(s.Path), dst); err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(dst, forgeManagedMarker), []byte("forge\n"), 0o644)
		copied++
	}
	pruneStaleCodexSkills(skillsDir, wanted)
	if copied > 0 {
		ui.Log.Success(fmt.Sprintf("Copied %d skill(s) into ~/.codex/skills/", copied))
	}
}

// copySkillDir recursively copies a skill source directory to dst as real files,
// replacing any previous copy.
func copySkillDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p) // resolves symlinked source files to real content
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// pruneStaleCodexSkills removes forge-managed skill copies that are no longer in
// the toolkit. Never touches user-authored skills (no marker) or .system.
func pruneStaleCodexSkills(skillsDir string, wanted map[string]bool) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".system" || wanted[e.Name()] {
			continue
		}
		marker := filepath.Join(skillsDir, e.Name(), forgeManagedMarker)
		if _, err := os.Stat(marker); err == nil {
			os.RemoveAll(filepath.Join(skillsDir, e.Name()))
		}
	}
}
