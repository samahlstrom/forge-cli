package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Pull the latest tools from the forge repo",
		RunE:  runSync,
	})
}

func runSync(_ *cobra.Command, _ []string) error {
	if !resolve.IsRepoCloned() {
		return fmt.Errorf("forge not set up — run 'forge setup' first")
	}

	repoDir := resolve.RepoDir()
	ui.Log.Step("Pulling latest...")

	cmd := exec.Command("git", "-C", repoDir, "pull", "--ff-only")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	ui.Log.Success("Toolkit synced.")
	return nil
}
