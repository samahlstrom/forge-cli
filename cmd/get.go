package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

var getAgent bool

func init() {
	cmd := &cobra.Command{
		Use:   "get <repo> [name]",
		Short: "Pull a skill or agent from any skills repo into your toolkit",
		Long: `Pull skills or agents from any Git repository into your personal toolkit.

Works with any repo that contains skills/ or agents/ directories, including:
  - anthropics/skills (Anthropic's official skill library)
  - Community skill collections
  - Anyone's personal toolkit

Examples:
  forge get anthropics/skills                       # List available skills
  forge get anthropics/skills pdf                   # Pull the "pdf" skill
  forge get anthropics/skills document-skills       # Pull a skill by name
  forge get https://github.com/user/repo my-skill  # Pull from any repo
  forge get user/repo my-agent --agent              # Pull an agent instead

GitHub shorthand (user/repo) is expanded to https://github.com/user/repo.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runGet,
	}
	cmd.Flags().BoolVar(&getAgent, "agent", false, "Pull an agent instead of a skill")
	rootCmd.AddCommand(cmd)
}

func runGet(_ *cobra.Command, args []string) error {
	if !resolve.IsSetup() {
		return fmt.Errorf("toolkit not found — run 'forge setup' first")
	}

	repoURL := expandRepoURL(args[0])

	// Clone to temp directory (shallow, fast)
	tmpDir, err := os.MkdirTemp("", "forge-get-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ui.Log.Step(fmt.Sprintf("Fetching %s...", args[0]))
	gitClone := exec.Command("git", "clone", "--depth=1", repoURL, tmpDir)
	gitClone.Stderr = os.Stderr
	if err := gitClone.Run(); err != nil {
		return fmt.Errorf("failed to clone %s: %w", repoURL, err)
	}

	if len(args) == 1 {
		return listRemote(tmpDir, args[0])
	}

	name := args[1]
	if getAgent {
		return pullAgent(tmpDir, name)
	}
	return pullSkill(tmpDir, name)
}

func listRemote(tmpDir, source string) error {
	skills := findRemoteSkills(tmpDir)
	agents := findRemoteAgents(tmpDir)

	if len(skills) == 0 && len(agents) == 0 {
		ui.Log.Warn(fmt.Sprintf("No skills or agents found in %s", source))
		return nil
	}

	fmt.Println()
	if len(skills) > 0 {
		fmt.Println(ui.Bold(fmt.Sprintf("Skills in %s", source)))
		sort.Strings(skills)
		for _, s := range skills {
			fmt.Printf("  %s\n", s)
		}
		fmt.Println()
	}

	if len(agents) > 0 {
		fmt.Println(ui.Bold(fmt.Sprintf("Agents in %s", source)))
		sort.Strings(agents)
		for _, a := range agents {
			fmt.Printf("  %s\n", a)
		}
		fmt.Println()
	}

	ui.Log.Info(fmt.Sprintf("Pull one with: forge get %s <name>", source))
	return nil
}

func pullSkill(tmpDir, name string) error {
	// Search for the skill in common locations
	srcDir := findSkillDir(tmpDir, name)
	if srcDir == "" {
		return fmt.Errorf("skill %q not found in repo", name)
	}

	dstDir := filepath.Join(resolve.SkillsDir(), name)
	if util.Exists(dstDir) {
		return fmt.Errorf("skill %q already exists in your toolkit — remove it first or use a different name", name)
	}

	if err := copyDir(srcDir, dstDir); err != nil {
		return fmt.Errorf("failed to copy skill: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Added skill: %s", name))

	commitAndPush(
		filepath.Join("skills", name),
		fmt.Sprintf("feat: add %s skill (from external repo)", name),
	)
	return nil
}

func pullAgent(tmpDir, name string) error {
	// Search for the agent in common locations
	srcFile := findAgentFile(tmpDir, name)
	if srcFile == "" {
		return fmt.Errorf("agent %q not found in repo", name)
	}

	dstFile := filepath.Join(resolve.AgentsDir(), name+".md")
	if util.Exists(dstFile) {
		return fmt.Errorf("agent %q already exists in your toolkit — remove it first or use a different name", name)
	}

	data, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("failed to read agent: %w", err)
	}

	if err := util.WriteText(dstFile, string(data)); err != nil {
		return fmt.Errorf("failed to write agent: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Added agent: %s", name))

	commitAndPush(
		filepath.Join("agents", name+".md"),
		fmt.Sprintf("feat: add %s agent (from external repo)", name),
	)
	return nil
}

// findSkillDir looks for a skill directory in common repo layouts.
func findSkillDir(tmpDir, name string) string {
	candidates := []string{
		filepath.Join(tmpDir, "skills", name),          // skills/name/ (standard)
		filepath.Join(tmpDir, "library", "skills", name), // library/skills/name/ (forge layout)
		filepath.Join(tmpDir, name),                     // name/ (flat repo of skills)
	}
	for _, c := range candidates {
		skillFile := filepath.Join(c, "SKILL.md")
		if util.Exists(skillFile) {
			return c
		}
	}
	// Deep search: walk the repo for a matching SKILL.md
	var found string
	filepath.WalkDir(tmpDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			dir := filepath.Dir(path)
			if filepath.Base(dir) == name {
				found = dir
			}
		}
		return nil
	})
	return found
}

// findAgentFile looks for an agent .md file in common repo layouts.
func findAgentFile(tmpDir, name string) string {
	candidates := []string{
		filepath.Join(tmpDir, "agents", name+".md"),
		filepath.Join(tmpDir, "library", "agents", name+".md"),
		filepath.Join(tmpDir, name+".md"),
	}
	for _, c := range candidates {
		if util.Exists(c) {
			return c
		}
	}
	return ""
}

// findRemoteSkills lists all skill names found in the repo.
func findRemoteSkills(tmpDir string) []string {
	var skills []string
	seen := map[string]bool{}

	filepath.WalkDir(tmpDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			name := filepath.Base(filepath.Dir(path))
			if !seen[name] {
				seen[name] = true
				skills = append(skills, name)
			}
		}
		return nil
	})
	return skills
}

// findRemoteAgents lists all agent names found in the repo.
func findRemoteAgents(tmpDir string) []string {
	var agents []string

	// Check standard locations
	for _, dir := range []string{
		filepath.Join(tmpDir, "agents"),
		filepath.Join(tmpDir, "library", "agents"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				agents = append(agents, strings.TrimSuffix(e.Name(), ".md"))
			}
		}
	}
	return agents
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// expandRepoURL converts GitHub shorthand (user/repo) to a full URL.
func expandRepoURL(input string) string {
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "git@") {
		return input
	}
	// GitHub shorthand: user/repo
	if strings.Count(input, "/") == 1 && !strings.Contains(input, " ") {
		return "https://github.com/" + input
	}
	return input
}
