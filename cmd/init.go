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

// forgeManagedMarker identifies a Codex skill directory an older forge COPIED.
// Nothing writes it any more — skills are linked, not copied — but it is still
// read to recognise those legacy copies for removal (see retireLegacyCodexCopies).
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

Use --global to link skills into ~/.claude/skills/ (Claude Code) and
~/.agents/skills/ (Codex), making them available in every session. Both
point at the same toolkit directories, so neither backend can drift.

Use --force to replace a skill directory whose contents differ from the
toolkit's. Copies forge itself made are migrated automatically; --force
is only needed when a directory carries edits that would be lost.

Use --enable-hook <name> (repeatable) to install an opt-in hook that is
off by default in the manifest (e.g. the leaky PreToolUse validate-gate).

Skills are symlinked, not copied — running 'forge sync' updates them
everywhere automatically.`,
		RunE: runInit,
	}
	initCmd.Flags().BoolVarP(&globalFlag, "global", "g", false, "Link skills globally for Claude Code and Codex (all projects and interfaces)")
	initCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Replace skill directories whose contents differ from the toolkit")
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

	// Refresh the auto-generated skills list in the toolkit AGENTS.md first, so
	// the regenerated manifest flows into every embed/import below.
	regenerateToolkitSkills()

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

	ui.Intro("Installing forge globally for Claude Code and Codex")

	installed, _ := syncGlobalSkills(forceFlag, true)

	fmt.Println()
	if installed > 0 {
		ui.Log.Info(fmt.Sprintf("%d skill link(s) installed globally — available in every Claude Code and Codex session.", installed))
	}

	// Inject forge section into ~/.claude/CLAUDE.md. The import MUST be absolute:
	// a relative @AGENTS.md here resolves to ~/.claude/AGENTS.md (the user's own
	// profile), not the toolkit — so the toolkit would never load globally.
	claudeMD := filepath.Join(claudeDir, "CLAUDE.md")
	if err := ensureClaudeMDSectionAt(claudeMD, globalForgeImport()); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not update ~/.claude/CLAUDE.md: %v", err))
	}

	// Both backends must ingest IDENTICAL instructions: Claude resolves the
	// toolkit's CLAUDE.md import chain, Codex reads the literal manifest.
	ensureToolkitClaudeMDImport()
	injectCodexGlobal()

	// Install global-scoped claude-settings hooks (e.g. ponytail-preload's
	// laziness ladder) into ~/.claude/settings.json, so the discipline fires for
	// every agent and session — not just inside an initialized repo.
	installGlobalHooks(claudeDir, enabledHooks())

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

	// Inject forge section into the project CLAUDE.md. Relative @AGENTS.md resolves
	// to the project's own AGENTS.md (which carries the toolkit manifest); the
	// global ~/.claude/CLAUDE.md import already loads the toolkit for every agent.
	if err := ensureClaudeMDSectionAt("CLAUDE.md", "@AGENTS.md"); err != nil {
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
// file. The section is a single `@import` line Claude resolves; importLine is the
// exact import to inject. The global ~/.claude/CLAUDE.md needs an ABSOLUTE import
// (see globalForgeImport); a per-project CLAUDE.md uses the relative @AGENTS.md
// that resolves to the project's own AGENTS.md.
func ensureClaudeMDSectionAt(claudeMD, importLine string) error {
	return writeForgeSection(claudeMD, importLine, "CLAUDE.md")
}

// toolkitClaudeMDImport is the toolkit's entire CLAUDE.md: a one-line import of
// the canonical AGENTS.md.
const toolkitClaudeMDImport = "@AGENTS.md\n"

// ensureToolkitClaudeMDImport verifies (and repairs) the toolkit's CLAUDE.md so
// Claude and Codex ingest identical instructions: AGENTS.md is the single
// canonical file, CLAUDE.md just imports it, and Codex reads AGENTS.md directly.
//
// It is deliberately NOT a symlink to AGENTS.md. The toolkit is a git repo that
// syncs to Sam's Windows machine, where a committed symlink degrades to a broken
// text file (no core.symlinks) and the instructions silently stop loading. The
// uppercase @AGENTS.md import is plain text Claude resolves on Windows, macOS and
// Linux alike — see PR #12 (2568e66), which moved off the symlink for this reason.
//
// Only two states are repaired: a missing file, and the pre-PR#12 CLAUDE.md ->
// AGENTS.md symlink (forge-managed, and the thing that breaks on Windows). A
// regular file is left alone — it is either already our one-liner or the user's
// own content, and neither wants rewriting.
//
// Lstat, not ReadFile: ReadFile FOLLOWS a symlink, so a content check would read
// AGENTS.md through the link, conclude it isn't the managed import, and silently
// leave the symlink in place — never repairing the one case this exists for.
func ensureToolkitClaudeMDImport() {
	path := filepath.Join(resolve.ForgeHome(), "CLAUDE.md")

	switch info, err := os.Lstat(path); {
	case err != nil: // missing — create it
	case info.Mode()&os.ModeSymlink == 0:
		return // a regular file: ours already, or the user's
	default:
		dest, err := os.Readlink(path)
		if err != nil || filepath.Base(dest) != "AGENTS.md" {
			return // a link we do not own
		}
		os.Remove(path) // replace the link itself, never write through it
	}

	if err := os.WriteFile(path, []byte(toolkitClaudeMDImport), 0o644); err != nil {
		ui.Log.Warn(fmt.Sprintf("Could not repair toolkit CLAUDE.md: %v", err))
		return
	}
	ui.Log.Success("Repaired toolkit CLAUDE.md (portable @AGENTS.md import)")
}

// globalForgeImport is the import line injected into ~/.claude/CLAUDE.md. It must
// be ABSOLUTE (@<forgeHome>/CLAUDE.md): a relative @AGENTS.md there resolves next
// to that file — ~/.claude/AGENTS.md, the user's personal profile — not the
// toolkit, so the toolkit would never auto-load for Claude globally.
func globalForgeImport() string {
	return "@" + filepath.Join(resolve.ForgeHome(), "CLAUDE.md")
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

// retireLegacyCodexCopies removes the skill copies older Forge builds wrote into
// ~/.codex/skills. Those copies existed because Codex's loader once ignored
// symlinks; codex-cli 0.144.1 follows them, so the canonical dirs are now linked
// into ~/.agents/skills instead (see globalSkillRoots).
//
// They must GO, not merely stop being refreshed: Codex reads ~/.codex/skills AND
// ~/.agents/skills and does not dedupe by name, so a leftover copy loads as a
// second, drifting version of the same skill. Nothing is lost — every copy is
// reproducible from the canonical dir it was copied from.
//
// Only Forge's own footprint is touched. Anything else — plugin/user-authored
// skills, Codex's bundled .system skills — is preserved.
func retireLegacyCodexCopies() {
	ch := codexHome()
	if ch == "" {
		return
	}
	skillsDir := filepath.Join(ch, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return // Codex not installed, or never had skills
	}
	canonical := map[string]bool{}
	for _, s := range resolve.ListSkills() {
		canonical[s.Name] = true
	}

	removed := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".system" {
			continue
		}
		dir := filepath.Join(skillsDir, e.Name())
		if !isLegacyForgeCopy(dir, e.Name(), canonical) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			ui.Log.Warn(fmt.Sprintf("Could not retire legacy Codex copy %s: %v", e.Name(), err))
			continue
		}
		removed++
	}
	if removed > 0 {
		ui.Log.Success(fmt.Sprintf("Retired %d legacy Codex skill copy(ies) from ~/.codex/skills/", removed))
	}
}

// isLegacyForgeCopy reports whether dir under ~/.codex/skills is a skill copy
// Forge itself created. Three signatures:
//   - the .forge-managed marker newer builds wrote (catches copies of skills
//     since REMOVED from the toolkit, whose names are no longer canonical);
//   - Forge's own "<toolkit-skill>.stale.<stamp>" archive, which Codex still
//     loads under its frontmatter name and so duplicates the canonical skill;
//   - a toolkit skill name — what older, pre-marker builds copied there.
//
// The last rung is a name match and nothing more. Forge stopped managing this
// directory entirely, so a <canonical-name> dir here is ours by construction.
// Inspecting SKILL.md's frontmatter would look more careful but protects nobody:
// a user's own "validate" skill declares name: validate too, exactly like our
// copy. Better an honest name match than a check that only rejects malformed
// files while reading as consent.
func isLegacyForgeCopy(dir, name string, canonical map[string]bool) bool {
	if _, err := os.Stat(filepath.Join(dir, forgeManagedMarker)); err == nil {
		return true
	}
	if base, _, found := strings.Cut(name, ".stale."); found && canonical[base] {
		return true
	}
	return canonical[name]
}
