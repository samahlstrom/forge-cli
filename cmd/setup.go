package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

const defaultRepoURL = "git@github.com:samahlstrom/forge-cli.git"

func init() {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install your toolkit to ~/.forge/",
		Long: `Clones the forge repo and sets up your personal toolkit at ~/.forge/.

This is a one-time operation per machine. After setup:
  - Your agents live at ~/.forge/library/agents/
  - Your skills live at ~/.forge/library/skills/
  - Run 'forge sync' to pull the latest tools`,
		RunE: runSetup,
	}
	rootCmd.AddCommand(cmd)
}

func runSetup(_ *cobra.Command, _ []string) error {
	home := resolve.ForgeHome()
	repoDir := resolve.RepoDir()
	libraryLink := resolve.LibraryDir()

	if resolve.IsSetup() {
		ui.Log.Step(fmt.Sprintf("Toolkit already installed at %s", home))
		ui.Log.Step("Run 'forge sync' to pull the latest tools.")
		return nil
	}

	ui.Intro("Setting up forge toolkit")

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

	// Symlink library/ from repo into ~/.forge/library
	repoLibrary := filepath.Join(repoDir, "library")
	if _, err := os.Stat(repoLibrary); err != nil {
		return fmt.Errorf("repo missing library/ directory at %s", repoLibrary)
	}

	// Remove existing symlink or directory if it exists
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
