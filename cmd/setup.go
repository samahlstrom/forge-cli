package cmd

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

// StarterContent is set by main.go with the embedded library/ files.
var StarterContent embed.FS

func init() {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create your personal toolkit at ~/.forge/",
		Long: `Creates a new toolkit at ~/.forge/ with starter agents, skills,
and pipeline scripts. The toolkit is a local git repository —
add a remote to sync across machines.

First time:
  forge setup

Sync across machines:
  cd ~/.forge && git remote add origin <your-repo-url> && git push -u origin main
  # On another machine:
  git clone <your-repo-url> ~/.forge`,
		RunE: runSetup,
	}
	rootCmd.AddCommand(cmd)
}

func runSetup(_ *cobra.Command, _ []string) error {
	home := resolve.ForgeHome()

	if resolve.IsSetup() {
		ui.Log.Step(fmt.Sprintf("Toolkit already exists at %s", home))
		ui.Log.Step("Run 'forge sync' to pull latest, or 'forge list' to see your toolkit.")
		return nil
	}

	ui.Intro("Creating your forge toolkit")

	// Create ~/.forge/
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", home, err)
	}

	// Extract embedded starter content
	ui.Log.Step("Extracting starter toolkit...")
	if err := extractEmbedded(StarterContent, "library", home); err != nil {
		return fmt.Errorf("failed to extract starter content: %w", err)
	}

	// Initialize git repo
	ui.Log.Step("Initializing git repository...")
	gitInit := exec.Command("git", "-C", home, "init")
	gitInit.Stdout = os.Stdout
	gitInit.Stderr = os.Stderr
	if err := gitInit.Run(); err != nil {
		ui.Log.Warn("Failed to initialize git repo — toolkit works but won't sync.")
	} else {
		// Initial commit
		gitAdd := exec.Command("git", "-C", home, "add", "-A")
		_ = gitAdd.Run()
		gitCommit := exec.Command("git", "-C", home, "commit", "-m", "Initial toolkit from forge setup")
		gitCommit.Stdout = os.Stdout
		gitCommit.Stderr = os.Stderr
		_ = gitCommit.Run()
	}

	fmt.Println()
	ui.Log.Success(fmt.Sprintf("Toolkit created at %s", home))
	ui.Log.Step(fmt.Sprintf("Agents: %s", resolve.AgentsDir()))
	ui.Log.Step(fmt.Sprintf("Skills: %s", resolve.SkillsDir()))
	ui.Log.Step(fmt.Sprintf("Pipeline: %s", resolve.PipelineDir()))
	fmt.Println()
	ui.Log.Info("To sync across machines, add a remote:")
	ui.Log.Message(fmt.Sprintf("  cd %s && git remote add origin <your-repo-url> && git push -u origin main", home))
	fmt.Println()
	ui.Log.Step("Run 'forge list' to see your toolkit.")
	return nil
}

// extractEmbedded walks the embedded FS starting at root and writes files to dst.
// Files under root/ are written directly into dst/ (the root prefix is stripped).
func extractEmbedded(content embed.FS, root, dst string) error {
	return fs.WalkDir(content, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the root prefix to get the relative path
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := content.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o644)
	})
}
