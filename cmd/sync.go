package cmd

import (
	"fmt"
	"os"
	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Pull the latest from your toolkit's remote",
		RunE:  runSync,
	})
}

func runSync(_ *cobra.Command, _ []string) error {
	if !resolve.IsSetup() {
		return fmt.Errorf("toolkit not found — run 'forge setup' first")
	}

	home := resolve.ForgeHome()

	if !resolve.IsGitRepo() {
		return fmt.Errorf("%s is not a git repo — run 'git -C %s init' first", home, home)
	}

	if !resolve.HasRemote() {
		return fmt.Errorf("no remote configured — add one with:\n  cd %s && git remote add origin <your-repo-url>", home)
	}

	// Pull remote changes first
	ui.Log.Step("Pulling latest...")
	pull := forgeGit(home, "pull", "--ff-only")
	pull.Stdout = os.Stdout
	pull.Stderr = os.Stderr
	if err := pull.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	// Push local changes
	ui.Log.Step("Pushing local changes...")
	push := forgeGit(home, "push")
	push.Stdout = os.Stdout
	push.Stderr = os.Stderr
	if err := push.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}

	ui.Log.Success("Toolkit synced.")

	// Re-wire skills into current project if initialized
	wireAllSkills()

	return nil
}
