package addon

import (
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/static"
	"github.com/samahlstrom/forge-cli/internal/util"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest describes an addon's structure and requirements.
type Manifest struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     int    `yaml:"version"`
	Requires    *struct {
		Commands map[string]bool `yaml:"commands"`
	} `yaml:"requires,omitempty"`
	Patches struct {
		ForgeYAML map[string]any `yaml:"forge_yaml,omitempty"`
		Agents    []string       `yaml:"agents,omitempty"`
	} `yaml:"patches"`
	Files []struct {
		Source string `yaml:"source"`
		Target string `yaml:"target"`
	} `yaml:"files"`
	PostInstall []string `yaml:"post_install,omitempty"`
}

var validAddons = []string{"browser-testing", "compliance-hipaa", "compliance-soc2"}

// IsValid checks if an addon name is known.
func IsValid(name string) bool {
	for _, a := range validAddons {
		if a == name {
			return true
		}
	}
	return false
}

// ListAvailable returns all known addon names.
func ListAvailable() []string { return validAddons }

// GetManifest reads an addon's manifest from embedded templates.
func GetManifest(name string) (Manifest, error) {
	path := filepath.Join("templates", "addons", name, "manifest.yaml")
	data, err := fs.ReadFile(static.TemplatesFS, path)
	if err != nil {
		return Manifest{}, fmt.Errorf("addon manifest not found: %s", name)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Install copies addon files and patches forge.yaml.
func Install(name, cwd string) ([]string, error) {
	manifest, err := GetManifest(name)
	if err != nil {
		return nil, err
	}

	// Check requirements
	if manifest.Requires != nil {
		var config map[string]any
		if err := util.ReadYAML(filepath.Join(cwd, "forge.yaml"), &config); err != nil {
			return nil, err
		}
		commands, _ := config["commands"].(map[string]any)
		for cmd, required := range manifest.Requires.Commands {
			if required && (commands == nil || commands[cmd] == nil || commands[cmd] == "") {
				return nil, fmt.Errorf("addon %q requires the %q command in forge.yaml", name, cmd)
			}
		}
	}

	var installed []string
	for _, f := range manifest.Files {
		srcPath := filepath.Join("templates", "addons", name, "files", f.Source)
		content, err := fs.ReadFile(static.TemplatesFS, srcPath)
		if err != nil {
			return nil, fmt.Errorf("addon file not found: %s", srcPath)
		}
		targetPath := filepath.Join(cwd, f.Target)
		if err := util.WriteText(targetPath, string(content)); err != nil {
			return nil, err
		}
		installed = append(installed, f.Target)
	}

	if err := patchForgeYAML(cwd, manifest, "add"); err != nil {
		return installed, err
	}

	// Update hashes
	hashes, _ := util.ReadHashes(cwd)
	for _, f := range manifest.Files {
		content, _ := util.ReadText(filepath.Join(cwd, f.Target))
		hashes.Files[f.Target] = util.HashContent(content)
	}
	_ = util.WriteHashes(cwd, hashes)

	return installed, nil
}

// Uninstall removes addon files and unpatches forge.yaml.
func Uninstall(name, cwd string) ([]string, error) {
	manifest, err := GetManifest(name)
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, f := range manifest.Files {
		target := filepath.Join(cwd, f.Target)
		if util.Exists(target) {
			os.Remove(target)
			removed = append(removed, f.Target)
		}
	}

	if err := patchForgeYAML(cwd, manifest, "remove"); err != nil {
		return removed, err
	}

	hashes, _ := util.ReadHashes(cwd)
	for _, f := range manifest.Files {
		delete(hashes.Files, f.Target)
	}
	_ = util.WriteHashes(cwd, hashes)

	return removed, nil
}

func patchForgeYAML(cwd string, manifest Manifest, action string) error {
	yamlPath := filepath.Join(cwd, "forge.yaml")
	var config map[string]any
	if err := util.ReadYAML(yamlPath, &config); err != nil {
		return err
	}

	// Patch addons array
	if manifest.Patches.ForgeYAML != nil {
		if v, ok := manifest.Patches.ForgeYAML["addons"]; ok {
			addons := toStringSlice(config["addons"])
			entries := toStringSlice(v)
			for _, entry := range entries {
				entry = strings.TrimPrefix(entry, "+")
				if action == "add" && !contains(addons, entry) {
					addons = append(addons, entry)
				} else if action == "remove" {
					addons = removeStr(addons, entry)
				}
			}
			config["addons"] = addons
		}
	}

	// Patch agents
	if manifest.Patches.Agents != nil {
		agents := toStringSlice(config["agents"])
		for _, entry := range manifest.Patches.Agents {
			name := strings.TrimPrefix(entry, "+")
			if action == "add" && !contains(agents, name) {
				agents = append(agents, name)
			} else if action == "remove" {
				agents = removeStr(agents, name)
			}
		}
		config["agents"] = agents
	}

	return util.WriteYAML(yamlPath, config)
}

func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		return ss
	}
	if si, ok := v.([]any); ok {
		var ss []string
		for _, item := range si {
			ss = append(ss, fmt.Sprint(item))
		}
		return ss
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func removeStr(ss []string, s string) []string {
	var result []string
	for _, v := range ss {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}
