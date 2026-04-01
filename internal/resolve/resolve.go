package resolve

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentInfo holds metadata about a discovered agent.
type AgentInfo struct {
	Name   string // agent name (e.g. "architect")
	Path   string // absolute path to the .md file
	Global bool   // true if from ~/.forge/, false if local override
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

// IsGlobalSetup returns true if ~/.forge/ exists and contains at minimum
// an agents/ directory (indicating setup has been run).
func IsGlobalSetup() bool {
	info, err := os.Stat(filepath.Join(ForgeHome(), "agents"))
	return err == nil && info.IsDir()
}

// GlobalDir returns ~/.forge/<subpath>.
func GlobalDir(subpath string) string {
	return filepath.Join(ForgeHome(), subpath)
}

// ResolveFile checks for a file at .forge/<relPath> in cwd first (local override),
// then at ~/.forge/<relPath> (global). Returns the absolute path of the first
// found, or empty string if neither exists.
func ResolveFile(cwd, relPath string) string {
	local := filepath.Join(cwd, ".forge", relPath)
	if fileExists(local) {
		return local
	}
	global := filepath.Join(ForgeHome(), relPath)
	if fileExists(global) {
		return global
	}
	return ""
}

// ResolveAgent resolves an agent definition file by name.
// Checks .forge/agents/<name>.md locally, then ~/.forge/agents/<name>.md.
func ResolveAgent(cwd, name string) string {
	return ResolveFile(cwd, filepath.Join("agents", name+".md"))
}

// ResolvePipeline resolves a pipeline script by filename.
// Checks .forge/pipeline/<name> locally, then ~/.forge/pipeline/<name>.
func ResolvePipeline(cwd, name string) string {
	return ResolveFile(cwd, filepath.Join("pipeline", name))
}

// ResolveHook resolves a hook script by filename.
// Checks .forge/hooks/<name> locally, then ~/.forge/hooks/<name>.
func ResolveHook(cwd, name string) string {
	return ResolveFile(cwd, filepath.Join("hooks", name))
}

// ResolveSkill resolves a skill directory by name.
// Checks .claude/skills/<name>/SKILL.md locally, then ~/.forge/skills/<name>/SKILL.md.
func ResolveSkill(cwd, name string) string {
	local := filepath.Join(cwd, ".claude", "skills", name, "SKILL.md")
	if fileExists(local) {
		return local
	}
	global := filepath.Join(ForgeHome(), "skills", name, "SKILL.md")
	if fileExists(global) {
		return global
	}
	return ""
}

// ListAgents merges agents from both ~/.forge/agents/ and .forge/agents/ in cwd.
// Local agents override global agents with the same name.
func ListAgents(cwd string) []AgentInfo {
	agents := make(map[string]AgentInfo)

	// Load global agents first
	globalDir := filepath.Join(ForgeHome(), "agents")
	loadAgentsFromDir(globalDir, true, agents)

	// Load local agents (overrides global on name collision)
	localDir := filepath.Join(cwd, ".forge", "agents")
	loadAgentsFromDir(localDir, false, agents)

	result := make([]AgentInfo, 0, len(agents))
	for _, a := range agents {
		result = append(result, a)
	}
	return result
}

func loadAgentsFromDir(dir string, global bool, into map[string]AgentInfo) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		into[name] = AgentInfo{
			Name:   name,
			Path:   filepath.Join(dir, entry.Name()),
			Global: global,
		}
	}
}

// ProjectID returns a stable short identifier for a project directory.
// Uses the git remote URL if available, otherwise the absolute path.
// The result is a 12-char hex hash safe for use as a directory name.
func ProjectID(cwd string) string {
	// Try git remote first for stable identity across clones
	remote := gitRemoteURL(cwd)
	if remote != "" {
		return shortHash(remote)
	}
	// Fall back to absolute path
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	return shortHash(abs)
}

// ProjectDir returns ~/.forge/projects/<project-id>/ for project-specific state.
func ProjectDir(cwd string) string {
	return filepath.Join(ForgeHome(), "projects", ProjectID(cwd))
}

// ProjectRunsDir returns the pipeline runs directory for a project.
func ProjectRunsDir(cwd string) string {
	return filepath.Join(ProjectDir(cwd), "runs")
}

// ProjectContextDir returns the context directory for a project.
func ProjectContextDir(cwd string) string {
	return filepath.Join(ProjectDir(cwd), "context")
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:6])
}

func gitRemoteURL(cwd string) string {
	// Read .git/config for remote URL — avoid exec for speed
	gitConfig := filepath.Join(cwd, ".git", "config")
	data, err := os.ReadFile(gitConfig)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	inRemote := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == `[remote "origin"]` {
			inRemote = true
			continue
		}
		if inRemote && strings.HasPrefix(trimmed, "url = ") {
			return strings.TrimPrefix(trimmed, "url = ")
		}
		if inRemote && strings.HasPrefix(trimmed, "[") {
			break
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
