package resolve

import (
	"os"
	"path/filepath"
	"strings"
)

// AgentInfo holds metadata about a discovered agent.
type AgentInfo struct {
	Name string
	Path string
}

// SkillInfo holds metadata about a discovered skill.
type SkillInfo struct {
	Name string
	Path string
}

// ForgeHome returns the global forge directory. Respects FORGE_HOME env var
// for testing and CI, otherwise defaults to ~/.forge/.
func ForgeHome() string {
	if h := os.Getenv("FORGE_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".forge")
	}
	return filepath.Join(home, ".forge")
}

// IsSetup returns true if ~/.forge/ has been initialized.
func IsSetup() bool {
	info, err := os.Stat(AgentsDir())
	return err == nil && info.IsDir()
}

// IsGitRepo returns true if ~/.forge/ is a git repository.
func IsGitRepo() bool {
	info, err := os.Stat(filepath.Join(ForgeHome(), ".git"))
	return err == nil && info.IsDir()
}

// HasRemote returns true if ~/.forge/ git repo has a remote configured.
func HasRemote() bool {
	entries, err := os.ReadDir(filepath.Join(ForgeHome(), ".git", "refs", "remotes"))
	return err == nil && len(entries) > 0
}

// AgentsDir returns the path to the agents directory.
func AgentsDir() string {
	return filepath.Join(ForgeHome(), "agents")
}

// SkillsDir returns the path to the skills directory.
func SkillsDir() string {
	return filepath.Join(ForgeHome(), "skills")
}

// PipelineDir returns the path to the pipeline directory.
func PipelineDir() string {
	return filepath.Join(ForgeHome(), "pipeline")
}

// ResolveAgent finds an agent by name. Returns empty string if not found.
func ResolveAgent(name string) string {
	path := filepath.Join(AgentsDir(), name+".md")
	if fileExists(path) {
		return path
	}
	return ""
}

// ResolveSkill finds a skill by name. Returns empty string if not found.
func ResolveSkill(name string) string {
	path := filepath.Join(SkillsDir(), name, "SKILL.md")
	if fileExists(path) {
		return path
	}
	return ""
}

// ListAgents returns all agents in the toolkit.
func ListAgents() []AgentInfo {
	dir := AgentsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var agents []AgentInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		agents = append(agents, AgentInfo{
			Name: name,
			Path: filepath.Join(dir, entry.Name()),
		})
	}
	return agents
}

// ListSkills returns all skills in the toolkit.
func ListSkills() []SkillInfo {
	dir := SkillsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var skills []SkillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		if fileExists(skillFile) {
			skills = append(skills, SkillInfo{
				Name: entry.Name(),
				Path: skillFile,
			})
		}
	}
	return skills
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
