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

// ToolkitManifestPath returns the path to the toolkit's agent-instructions
// manifest inside ForgeHome, preferring the canonical uppercase AGENTS.md and
// falling back to a legacy lowercase agents.md when only that exists. When
// neither exists it returns the AGENTS.md path (the canonical name forge sync
// now writes). Resolving by existence (not a hardcoded literal) keeps the
// installer correct on case-sensitive filesystems where AGENTS.md and agents.md
// are distinct files.
func ToolkitManifestPath() string {
	upper := filepath.Join(ForgeHome(), "AGENTS.md")
	if fileExists(upper) {
		return upper
	}
	lower := filepath.Join(ForgeHome(), "agents.md")
	if fileExists(lower) {
		return lower
	}
	return upper
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

// Summary returns a compact one-line description for the skill, used to render
// the auto-generated AGENTS.md skills list. It prefers an optional curated
// `summary:` frontmatter field; absent that, it falls back to the first sentence
// of the long `description:` field. Empty if the SKILL.md can't be read.
func (s SkillInfo) Summary() string {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return ""
	}
	content := string(data)
	if v := frontmatterField(content, "summary"); v != "" {
		return v
	}
	return firstSentence(frontmatterField(content, "description"))
}

// frontmatterField returns the single-line value of `key:` from the leading
// `---` frontmatter block, matched at line start. Empty if absent.
// ponytail: handles single-line `key: value` only — no folded/multi-line YAML;
// SKILL.md frontmatter is flat, upgrade to a YAML parser if that ever changes.
func frontmatterField(content, key string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	body := content[3:]
	if i := strings.Index(body, "\n---"); i >= 0 {
		body = body[:i]
	}
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, key+":") {
			return unquote(strings.TrimSpace(ln[len(key)+1:]))
		}
	}
	return ""
}

// unquote strips a single matching pair of surrounding YAML quotes (real
// SKILL.md files quote descriptions; the raw quotes must not leak into the line).
func unquote(s string) string {
	if len(s) >= 2 {
		if q := s[0]; (q == '"' || q == '\'') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// firstSentence returns the text up to the first period followed by a space or
// end-of-string, with the period dropped for a clean bullet line.
// ponytail: naive period split; an abbreviation like "e.g." can cut early — fine
// for a fallback, the curated `summary:` field is the real path.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		if s[i] == '.' && (i+1 == len(s) || s[i+1] == ' ') {
			return s[:i]
		}
	}
	return s
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
