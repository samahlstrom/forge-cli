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

// LibraryDir returns the library content directory inside ~/.forge/.
// Supports both layouts:
//   - New: ~/.forge/library/ (symlink to repo/library/)
//   - Flat: ~/.forge/ (files copied directly)
func LibraryDir() string {
	lib := filepath.Join(ForgeHome(), "library")
	if info, err := os.Stat(lib); err == nil && info.IsDir() {
		return lib
	}
	// Fall back to flat layout where agents/skills live directly in forge home
	return ForgeHome()
}

// IsSetup returns true if the agents directory exists in either layout.
func IsSetup() bool {
	info, err := os.Stat(AgentsDir())
	return err == nil && info.IsDir()
}

// RepoDir returns the path to the forge-cli source repo clone inside ~/.forge/.
func RepoDir() string {
	return filepath.Join(ForgeHome(), "repo")
}

// IsRepoCloned returns true if the forge repo is cloned.
func IsRepoCloned() bool {
	info, err := os.Stat(filepath.Join(RepoDir(), ".git"))
	return err == nil && info.IsDir()
}

// AgentsDir returns the path to the agents directory.
func AgentsDir() string {
	return filepath.Join(LibraryDir(), "agents")
}

// SkillsDir returns the path to the skills directory.
func SkillsDir() string {
	return filepath.Join(LibraryDir(), "skills")
}

// PipelineDir returns the path to the pipeline directory.
func PipelineDir() string {
	return filepath.Join(LibraryDir(), "pipeline")
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

// ListAgents returns all agents in the library.
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

// ListSkills returns all skills in the library.
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
