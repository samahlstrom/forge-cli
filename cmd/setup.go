package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"

	"github.com/spf13/cobra"
)

// StarterContent is set by main.go with the embedded library/ files.
var StarterContent embed.FS

const toolkitRepoName = "forge-toolkit"

func init() {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create your personal toolkit at ~/.forge/",
		Long: `Creates a new toolkit at ~/.forge/ with starter agents, skills,
and pipeline scripts. The toolkit is a local git repository backed
by a private GitHub repo (forge-toolkit) for cross-machine sync.

First time:
  forge setup

On another machine (same GitHub account):
  forge setup    # auto-detects and clones your existing toolkit`,
		RunE: runSetup,
	}
	rootCmd.AddCommand(cmd)
}

func runSetup(_ *cobra.Command, _ []string) error {
	home := resolve.ForgeHome()

	// If toolkit already exists, just ensure remote is wired
	if resolve.IsSetup() {
		ui.Log.Step(fmt.Sprintf("Toolkit already exists at %s", home))
		if resolve.IsGitRepo() && !resolve.HasRemote() {
			return ensureRemote(home)
		}
		ui.Log.Step("Run 'forge sync' to pull latest, or 'forge list' to see your toolkit.")
		return nil
	}

	// Toolkit doesn't exist — check if user has an existing forge-toolkit repo on GitHub
	ghUser := detectGitHubUser()
	if ghUser != "" {
		if repoExists(ghUser, toolkitRepoName) {
			return cloneExistingToolkit(home, ghUser)
		}
	}

	// Fresh setup: scaffold from embedded starter content
	return freshSetup(home, ghUser)
}

// freshSetup creates ~/.forge from embedded starter content, initializes git,
// and creates a private GitHub repo.
func freshSetup(home, ghUser string) error {
	ui.Intro("Creating your forge toolkit")

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
		return nil
	}

	// Initial commit
	gitAdd := exec.Command("git", "-C", home, "add", "-A")
	_ = gitAdd.Run()
	gitCommit := exec.Command("git", "-C", home, "commit", "-m", "Initial toolkit from forge setup")
	gitCommit.Stdout = os.Stdout
	gitCommit.Stderr = os.Stderr
	_ = gitCommit.Run()

	fmt.Println()
	ui.Log.Success(fmt.Sprintf("Toolkit created at %s", home))
	ui.Log.Step(fmt.Sprintf("Agents: %s", resolve.AgentsDir()))
	ui.Log.Step(fmt.Sprintf("Skills: %s", resolve.SkillsDir()))
	ui.Log.Step(fmt.Sprintf("Pipeline: %s", resolve.PipelineDir()))

	// Create remote repo and wire it up
	if ghUser != "" {
		fmt.Println()
		if err := createAndWireRemote(home, ghUser); err != nil {
			ui.Log.Warn(fmt.Sprintf("Remote setup failed: %v", err))
			ui.Log.Info("Your toolkit works locally. Run 'forge setup' again to retry remote setup.")
		}
	} else {
		fmt.Println()
		ui.Log.Warn("GitHub CLI (gh) not found or not authenticated.")
		ui.Log.Info("Install gh and run 'gh auth login', then 'forge setup' to enable sync.")
	}

	fmt.Println()
	ui.Log.Step("Run 'forge list' to see your toolkit.")
	return nil
}

// cloneExistingToolkit clones the user's existing forge-toolkit repo to ~/.forge.
// If ~/.forge/ already exists (e.g. manually created), it's moved aside first.
func cloneExistingToolkit(home, ghUser string) error {
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", ghUser, toolkitRepoName)

	ui.Intro("Found your existing toolkit on GitHub")

	// If the directory exists but isn't a proper toolkit, move it aside
	if info, err := os.Stat(home); err == nil && info.IsDir() {
		backup := home + ".bak"
		ui.Log.Step(fmt.Sprintf("Moving existing %s to %s...", home, backup))
		os.RemoveAll(backup) // remove previous backup if any
		if err := os.Rename(home, backup); err != nil {
			return fmt.Errorf("failed to move existing %s: %w", home, err)
		}
	}

	ui.Log.Step(fmt.Sprintf("Cloning %s/%s...", ghUser, toolkitRepoName))

	gitClone := exec.Command("git", "-c", "credential.helper=!gh auth git-credential", "clone", repoURL, home)
	gitClone.Stdout = os.Stdout
	gitClone.Stderr = os.Stderr
	if err := gitClone.Run(); err != nil {
		return fmt.Errorf("failed to clone toolkit: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Toolkit restored at %s", home))
	ui.Log.Step("All your skills, agents, and pipeline scripts are back.")
	fmt.Println()
	ui.Log.Step("Run 'forge list' to see your toolkit.")
	return nil
}

// ensureRemote creates the GitHub repo and wires it as remote for an existing local toolkit.
func ensureRemote(home string) error {
	ghUser := detectGitHubUser()
	if ghUser == "" {
		ui.Log.Warn("GitHub CLI (gh) not found or not authenticated.")
		ui.Log.Info("Install gh and run 'gh auth login', then 'forge setup' to enable sync.")
		return nil
	}

	return createAndWireRemote(home, ghUser)
}

// createAndWireRemote creates the private GitHub repo and adds it as origin.
func createAndWireRemote(home, ghUser string) error {
	repoFullName := fmt.Sprintf("%s/%s", ghUser, toolkitRepoName)

	if !repoExists(ghUser, toolkitRepoName) {
		ui.Log.Step(fmt.Sprintf("Creating private repo %s...", repoFullName))
		create := exec.Command("gh", "repo", "create", repoFullName,
			"--private",
			"--description", "Personal AI agent toolkit for Claude Code (managed by forge)",
		)
		create.Stdout = os.Stdout
		create.Stderr = os.Stderr
		if err := create.Run(); err != nil {
			return fmt.Errorf("failed to create repo: %w", err)
		}
	}

	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", ghUser, toolkitRepoName)

	addRemote := exec.Command("git", "-C", home, "remote", "add", "origin", repoURL)
	if err := addRemote.Run(); err != nil {
		// Remote might already exist with wrong URL
		setURL := exec.Command("git", "-C", home, "remote", "set-url", "origin", repoURL)
		_ = setURL.Run()
	}

	ui.Log.Step("Pushing toolkit to GitHub...")
	push := forgeGit(home, "push", "-u", "origin", "main")
	push.Stdout = os.Stdout
	push.Stderr = os.Stderr
	if err := push.Run(); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Toolkit backed up to %s (private)", repoFullName))
	ui.Log.Info("Run 'forge sync' on any machine to stay in sync.")
	return nil
}

// forgeGit creates a git command that runs in the forge home directory
// and uses gh for credential auth (so HTTPS push/pull works regardless
// of the user's global git credential config).
func forgeGit(home string, args ...string) *exec.Cmd {
	fullArgs := append([]string{"-C", home, "-c", "credential.helper=!gh auth git-credential"}, args...)
	return exec.Command("git", fullArgs...)
}

// detectGitHubUser returns the authenticated GitHub username, or empty string.
func detectGitHubUser() string {
	cmd := exec.Command("gh", "api", "user", "--jq", ".login")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// repoExists checks if a GitHub repo exists.
func repoExists(owner, name string) bool {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/%s", owner, name), "--jq", ".full_name")
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	var result interface{}
	// If it parsed as JSON with a "message" field, it's a 404
	if json.Unmarshal(out, &result) == nil {
		if m, ok := result.(map[string]interface{}); ok {
			if _, has := m["message"]; has {
				return false
			}
		}
	}

	return strings.TrimSpace(string(out)) != ""
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
