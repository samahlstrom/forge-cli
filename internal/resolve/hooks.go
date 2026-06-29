package resolve

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// HookInfo describes one hook declared in library/hooks/manifest.json. The
// installer walks these generically — it switches on Kind, never on Name — so
// new hooks are added by editing the manifest, not the Go code.
type HookInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`    // "git-hook" | "claude-settings-hook"
	GitHook string `json:"gitHook"` // git-hook only, e.g. "pre-push"
	Event   string `json:"event"`   // claude-settings-hook only, e.g. "PreToolUse"
	Matcher string `json:"matcher"` // claude-settings-hook only, e.g. "Bash"
	Script  string `json:"script"`  // script filename under HooksDir()
	Scope   string `json:"scope"`   // "repo" | "global"
	Default bool   `json:"default"` // install automatically during init?
	Note    string `json:"note"`
}

// ScriptInfo describes a standalone script declared in the manifest.
type ScriptInfo struct {
	Name    string `json:"name"`
	Script  string `json:"script"`
	Scope   string `json:"scope"`
	Default bool   `json:"default"`
}

// HooksManifest is the parsed library/hooks/manifest.json.
type HooksManifest struct {
	Hooks   []HookInfo   `json:"hooks"`
	Scripts []ScriptInfo `json:"scripts"`
}

// HooksDir returns the path to the toolkit's hooks directory.
func HooksDir() string {
	return filepath.Join(ForgeHome(), "hooks")
}

// HookScriptPath returns the absolute path to a hook/script file in the toolkit.
func HookScriptPath(script string) string {
	return filepath.Join(HooksDir(), script)
}

// LoadHooksManifest parses the toolkit's hooks manifest. Returns an error if the
// file is absent or the JSON is malformed.
func LoadHooksManifest() (HooksManifest, error) {
	var m HooksManifest
	data, err := os.ReadFile(filepath.Join(HooksDir(), "manifest.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}

// ListHooks returns the hooks declared in the manifest, or nil if the manifest
// is absent or malformed (the installer treats "no manifest" as "no hooks").
func ListHooks() []HookInfo {
	m, err := LoadHooksManifest()
	if err != nil {
		return nil
	}
	return m.Hooks
}

// ListScripts returns the standalone scripts declared in the manifest, or nil.
func ListScripts() []ScriptInfo {
	m, err := LoadHooksManifest()
	if err != nil {
		return nil
	}
	return m.Scripts
}
