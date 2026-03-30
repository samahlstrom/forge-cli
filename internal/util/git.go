package util

import (
	"os/exec"
	"strings"
)

// IsGitRepo checks if cwd is inside a git repository.
func IsGitRepo(cwd string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = cwd
	return cmd.Run() == nil
}

// GetMainBranch returns the main branch name (main or master).
func GetMainBranch(cwd string) string {
	cmd := exec.Command("bash", "-c", "git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null || echo refs/heads/main")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	ref := strings.TrimSpace(string(out))
	ref = strings.TrimPrefix(ref, "refs/remotes/origin/")
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return ref
}

// GetCurrentBranch returns the current git branch name.
func GetCurrentBranch(cwd string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GitExec runs a git command in the given directory.
func GitExec(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
