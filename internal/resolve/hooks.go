package resolve

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// HookInfo describes one hook declared in the toolkit's hooks manifest
// (~/.forge/hooks/manifest.json, resolved at runtime via HooksDir()). The
// installer walks these generically — it switches on Kind, never on Name — so
// new hooks are added by editing the manifest, not the Go code.
type HookInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`              // "git-hook" | "claude-settings-hook"
	GitHook string `json:"gitHook,omitempty"` // git-hook only, e.g. "pre-push"
	Event   string `json:"event,omitempty"`   // claude-settings-hook only, e.g. "PreToolUse"
	Matcher string `json:"matcher,omitempty"` // claude-settings-hook only, e.g. "Bash"
	Script  string `json:"script"`            // script filename under HooksDir()
	Scope   string `json:"scope"`             // "repo" | "global"; claude-settings hooks default to global, git hooks are per-repo
	Default bool   `json:"default"`           // install automatically during init?
	Note    string `json:"note,omitempty"`
}

// ScriptInfo describes a standalone script declared in the manifest.
type ScriptInfo struct {
	Name    string `json:"name"`
	Script  string `json:"script"`
	Scope   string `json:"scope"`
	Default bool   `json:"default"`
}

// HooksManifest is the parsed toolkit hooks manifest (~/.forge/hooks/manifest.json).
type HooksManifest struct {
	Hooks   []HookInfo   `json:"hooks,omitempty"`
	Scripts []ScriptInfo `json:"scripts,omitempty"`
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

// loadManifestForEdit loads the manifest for a read-modify-write. A missing file
// is an empty manifest (so the first `forge hook add` creates it); a malformed
// file is an error, so we never clobber a manifest we couldn't parse.
func loadManifestForEdit() (HooksManifest, error) {
	m, err := LoadHooksManifest()
	if os.IsNotExist(err) {
		return HooksManifest{}, nil
	}
	return m, err
}

// SaveHooksManifest writes the manifest to HooksDir()/manifest.json, pretty-printed.
func SaveHooksManifest(m HooksManifest) error {
	if err := os.MkdirAll(HooksDir(), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(HooksDir(), "manifest.json"), append(out, '\n'), 0o644)
}

// UpsertHook adds h to the toolkit manifest, replacing any existing hook with the
// same Name (idempotent). It edits the manifest generically by Name and does not
// interpret Kind — the installer is what switches on Kind.
func UpsertHook(h HookInfo) error {
	m, err := loadManifestForEdit()
	if err != nil {
		return err
	}
	for i := range m.Hooks {
		if m.Hooks[i].Name == h.Name {
			m.Hooks[i] = h
			return SaveHooksManifest(m)
		}
	}
	m.Hooks = append(m.Hooks, h)
	return SaveHooksManifest(m)
}

// RemoveHookFromManifest drops the hook named name. It returns the removed entry
// (so the caller can delete its script) and whether one was found.
func RemoveHookFromManifest(name string) (HookInfo, bool, error) {
	m, err := loadManifestForEdit()
	if err != nil {
		return HookInfo{}, false, err
	}
	for i, h := range m.Hooks {
		if h.Name == name {
			m.Hooks = append(m.Hooks[:i], m.Hooks[i+1:]...)
			return h, true, SaveHooksManifest(m)
		}
	}
	return HookInfo{}, false, nil
}
