package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

const defaultRepoURL = "https://github.com/samahlstrom/forge-cli.git"

func init() {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install your toolkit to ~/.forge/",
		Long: `Clones the forge repo and sets up your personal toolkit at ~/.forge/.

This is a one-time operation per machine. After setup:
  - Your agents live at ~/.forge/library/agents/
  - Your skills live at ~/.forge/library/skills/
  - Run 'forge sync' to pull the latest tools

If you have an older flat layout (~/.forge/agents/ instead of
~/.forge/library/agents/), setup will migrate it automatically,
preserving any custom agents or skills you've added.`,
		RunE: runSetup,
	}
	rootCmd.AddCommand(cmd)
}

func runSetup(_ *cobra.Command, _ []string) error {
	home := resolve.ForgeHome()
	repoDir := resolve.RepoDir()
	libraryLink := filepath.Join(home, "library")

	// Already fully set up: repo cloned + library symlink exists
	if resolve.IsRepoCloned() {
		if info, err := os.Lstat(libraryLink); err == nil && info.Mode()&os.ModeSymlink != 0 {
			ui.Log.Step(fmt.Sprintf("Toolkit already installed at %s", home))
			ui.Log.Step("Run 'forge sync' to pull the latest tools.")
			return nil
		}
	}

	ui.Intro("Setting up forge toolkit")

	// Detect flat layout that needs migration
	flatLayout := isFlatLayout(home)
	if flatLayout {
		ui.Log.Step("Detected older layout — will migrate to repo-based toolkit.")
	}

	// Clone the repo if not already present
	if !resolve.IsRepoCloned() {
		ui.Log.Step("Cloning forge repo...")
		if err := os.MkdirAll(home, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", home, err)
		}
		cmd := exec.Command("git", "clone", defaultRepoURL, repoDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
	} else {
		ui.Log.Step("Forge repo already cloned, pulling latest...")
		cmd := exec.Command("git", "-C", repoDir, "pull", "--ff-only")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}

	// Migrate custom content from flat layout into the repo before symlinking
	if flatLayout {
		if err := migrateFlat(home, repoDir); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// Create library symlink
	repoLibrary := filepath.Join(repoDir, "library")
	if _, err := os.Stat(repoLibrary); err != nil {
		return fmt.Errorf("repo missing library/ directory at %s", repoLibrary)
	}

	// Remove existing symlink if present
	if info, err := os.Lstat(libraryLink); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			os.Remove(libraryLink)
		}
	}

	if err := os.Symlink(repoLibrary, libraryLink); err != nil {
		return fmt.Errorf("failed to symlink library: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Toolkit installed at %s", home))
	ui.Log.Step(fmt.Sprintf("Agents: %s", resolve.AgentsDir()))
	ui.Log.Step(fmt.Sprintf("Skills: %s", resolve.SkillsDir()))
	ui.Log.Step("Run 'forge list' to see your toolkit.")
	return nil
}

// isFlatLayout detects the old layout where agents/skills live directly in ~/.forge/
// rather than under ~/.forge/library/ (symlinked from repo).
func isFlatLayout(home string) bool {
	libraryLink := filepath.Join(home, "library")
	// If library symlink already exists, not a flat layout
	if info, err := os.Lstat(libraryLink); err == nil && (info.Mode()&os.ModeSymlink != 0 || info.IsDir()) {
		return false
	}
	// Flat layout has agents/ or skills/ directly under home
	agentsDir := filepath.Join(home, "agents")
	if info, err := os.Stat(agentsDir); err == nil && info.IsDir() {
		return true
	}
	skillsDir := filepath.Join(home, "skills")
	if info, err := os.Stat(skillsDir); err == nil && info.IsDir() {
		return true
	}
	return false
}

// migrateFlat copies custom agents and skills from the flat layout into the repo,
// then removes the old flat directories.
func migrateFlat(home, repoDir string) error {
	repoLibrary := filepath.Join(repoDir, "library")

	// Migrate custom agents
	flatAgents := filepath.Join(home, "agents")
	repoAgents := filepath.Join(repoLibrary, "agents")
	if err := copyCustomFiles(flatAgents, repoAgents); err != nil {
		return fmt.Errorf("migrating agents: %w", err)
	}

	// Migrate custom skills
	flatSkills := filepath.Join(home, "skills")
	repoSkills := filepath.Join(repoLibrary, "skills")
	if err := copyCustomSkills(flatSkills, repoSkills); err != nil {
		return fmt.Errorf("migrating skills: %w", err)
	}

	// Remove old flat directories
	flatDirs := []string{"agents", "skills", "pipeline", "hooks", "presets", "projects"}
	for _, dir := range flatDirs {
		path := filepath.Join(home, dir)
		if util.Exists(path) {
			os.RemoveAll(path)
			ui.Log.Step(fmt.Sprintf("Removed old %s/", dir))
		}
	}

	// Remove old flat files
	flatFiles := []string{".hashes.json", "config.yaml"}
	for _, f := range flatFiles {
		path := filepath.Join(home, f)
		if util.Exists(path) {
			os.Remove(path)
		}
	}

	return nil
}

// copyCustomFiles copies .md files from src to dst that don't already exist in dst.
func copyCustomFiles(src, dst string) error {
	if !util.Exists(src) {
		return nil
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		dstPath := filepath.Join(dst, e.Name())
		if util.Exists(dstPath) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			continue
		}
		if err := util.WriteText(dstPath, string(data)); err != nil {
			continue
		}
		ui.Log.Success(fmt.Sprintf("Migrated custom agent: %s", e.Name()))
	}
	return nil
}

// copyCustomSkills copies skill directories from src to dst that don't already exist in dst.
func copyCustomSkills(src, dst string) error {
	if !util.Exists(src) {
		return nil
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dstDir := filepath.Join(dst, e.Name())
		if util.Exists(dstDir) {
			continue
		}
		srcSkill := filepath.Join(src, e.Name(), "SKILL.md")
		if !util.Exists(srcSkill) {
			continue
		}
		data, err := os.ReadFile(srcSkill)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			continue
		}
		if err := util.WriteText(filepath.Join(dstDir, "SKILL.md"), string(data)); err != nil {
			continue
		}
		ui.Log.Success(fmt.Sprintf("Migrated custom skill: %s", e.Name()))
	}
	return nil
}
