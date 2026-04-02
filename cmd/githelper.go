package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
)

// commitAndPush stages, commits, and pushes a file in the ~/.forge/ toolkit repo.
// If the toolkit isn't a git repo or has no remote, it skips gracefully.
func commitAndPush(relPath, commitMsg string) {
	home := resolve.ForgeHome()

	if !resolve.IsGitRepo() {
		return
	}

	gitAdd := exec.Command("git", "-C", home, "add", relPath)
	if err := gitAdd.Run(); err != nil {
		ui.Log.Warn("Failed to stage — commit manually.")
		return
	}

	gitCommit := exec.Command("git", "-C", home, "commit", "-m", commitMsg)
	gitCommit.Stdout = os.Stdout
	gitCommit.Stderr = os.Stderr
	if err := gitCommit.Run(); err != nil {
		ui.Log.Warn("Failed to commit — commit manually.")
		return
	}

	if !resolve.HasRemote() {
		ui.Log.Success("Committed locally.")
		ui.Log.Info(fmt.Sprintf("Add a remote to sync: cd %s && git remote add origin <url>", home))
		return
	}

	gitPush := exec.Command("git", "-C", home, "push")
	gitPush.Stdout = os.Stdout
	gitPush.Stderr = os.Stderr
	if err := gitPush.Run(); err != nil {
		ui.Log.Warn(fmt.Sprintf("Committed locally but push failed — run 'git -C %s push' when online.", home))
		return
	}

	ui.Log.Success("Committed and pushed — available on all machines after 'forge sync'.")
}
