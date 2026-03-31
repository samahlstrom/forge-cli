package cmd

import (
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/detect"
	"github.com/samahlstrom/forge-cli/internal/render"
	"github.com/samahlstrom/forge-cli/internal/static"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var upgradeForce bool

func init() {
	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade core pipeline and addons to the latest version",
		RunE:  runUpgrade,
	}
	upgradeCmd.Flags().BoolVar(&upgradeForce, "force", false, "Overwrite all files without prompting")
	rootCmd.AddCommand(upgradeCmd)
}

// Files that are ALWAYS overwritten (tool-owned).
var alwaysOverwrite = map[string]bool{
	".forge/context/stack.md":  true,
	".claude/settings.json":    true,
}

// Files that are NEVER touched (user-owned).
var neverTouch = map[string]bool{
	".forge/context/project.md": true,
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold("forge upgrade"))

	if !util.Exists(filepath.Join(cwd, "forge.yaml")) {
		ui.Log.Error("No forge.yaml found. Run `forge init` first.")
		os.Exit(1)
	}

	hashes, _ := util.ReadHashes(cwd)
	currentVersion := hashes.Version
	newVersion := static.Version

	ui.Log.Info(fmt.Sprintf("Current: v%s → Available: v%s", currentVersion, newVersion))

	var config map[string]any
	if err := util.ReadYAML(filepath.Join(cwd, "forge.yaml"), &config); err != nil {
		return err
	}
	project, _ := config["project"].(map[string]any)
	preset, _ := project["preset"].(string)
	if preset == "" {
		preset = "sveltekit-ts"
	}

	detected := detect.Detect(cwd)
	ctx := buildUpgradeContext(config, detected, preset)

	updated := 0
	skipped := 0
	prompted := 0
	newHashes := util.HashManifest{Version: newVersion, Files: map[string]string{}}

	for relPath, installedHash := range hashes.Files {
		// Skip addon files
		if strings.HasPrefix(relPath, ".forge/addons/") {
			continue
		}

		// Never touch user-owned files
		if neverTouch[relPath] {
			ui.Log.Message(fmt.Sprintf("  %s %s %s", ui.Dim("⊘"), relPath, ui.Dim("— user-owned, skipped")))
			skipped++
			newHashes.Files[relPath] = installedHash
			continue
		}

		filePath := filepath.Join(cwd, relPath)
		if !util.Exists(filePath) {
			skipped++
			continue
		}

		templatePath := findTemplatePath(relPath, preset)
		if templatePath == "" {
			newHashes.Files[relPath] = installedHash
			continue
		}

		templateContent, err := fs.ReadFile(static.TemplatesFS, filepath.Join("templates", templatePath))
		if err != nil {
			newHashes.Files[relPath] = installedHash
			continue
		}
		newContent := render.Render(string(templateContent), ctx)
		newContentHash := util.HashContent(newContent)

		// Always overwrite tool-owned files
		if alwaysOverwrite[relPath] {
			_ = util.WriteText(filePath, newContent)
			newHashes.Files[relPath] = newContentHash
			ui.Log.Success(fmt.Sprintf("%s %s", relPath, ui.Dim("— tool-owned, updated")))
			updated++
			continue
		}

		// Check if user modified the file
		currentHash, _ := util.HashFile(filePath)
		userModified := currentHash != installedHash

		if !userModified || upgradeForce {
			_ = util.WriteText(filePath, newContent)
			newHashes.Files[relPath] = newContentHash
			if newContentHash != installedHash {
				ui.Log.Success(fmt.Sprintf("%s %s", relPath, ui.Dim("— updated")))
				updated++
			}
		} else {
			action, cancelled := ui.Select(
				fmt.Sprintf("%s — you modified this. What to do?", relPath),
				[]ui.SelectOption{
					{Value: "skip", Label: "Skip (keep your version)"},
					{Value: "overwrite", Label: "Overwrite (use new version)"},
				},
			)
			if cancelled {
				ui.Cancel("Upgrade cancelled.")
				os.Exit(0)
			}
			if action == "overwrite" {
				_ = util.WriteText(filePath, newContent)
				newHashes.Files[relPath] = newContentHash
				updated++
			} else {
				newHashes.Files[relPath] = currentHash
				skipped++
			}
			prompted++
		}
	}

	// Merge forge.yaml
	templateContent, err := fs.ReadFile(static.TemplatesFS, filepath.Join("templates", "core", "forge.yaml.hbs"))
	if err == nil {
		newForgeYAML := render.Render(string(templateContent), ctx)
		existingForgeYAML, _ := util.ReadText(filepath.Join(cwd, "forge.yaml"))
		merged, err := render.MergeForgeYAML(existingForgeYAML, newForgeYAML)
		if err == nil && merged != existingForgeYAML {
			_ = util.WriteText(filepath.Join(cwd, "forge.yaml"), merged)
			ui.Log.Success("forge.yaml — merged new fields")
			updated++
		}
	}

	// Clean up deprecated files from previous versions
	deprecatedFiles := []string{
		".forge/pipeline/classify.sh",      // Replaced by classify.md (LLM agent classifier)
		".forge/pipeline/orchestrator.sh",  // Replaced by SKILL.md inline orchestration
		".forge/pipeline/decompose.md",     // Merged into architect.md
		".forge/pipeline/execute.md",       // Absorbed into SKILL.md
		".forge/pipeline/evaluate.md",      // Absorbed into SKILL.md
		".claude/skills/deliver/SKILL.md",  // Renamed to /forge
	}
	for _, dep := range deprecatedFiles {
		depPath := filepath.Join(cwd, dep)
		if util.Exists(depPath) {
			_ = os.Remove(depPath)
			ui.Log.Success(fmt.Sprintf("%s %s", dep, ui.Dim("— deprecated, removed")))
		}
	}

	// Clean up empty directories left by deprecated files
	deprecatedDirs := []string{
		".claude/skills/deliver",
	}
	for _, dir := range deprecatedDirs {
		dirPath := filepath.Join(cwd, dir)
		if util.Exists(dirPath) {
			entries, _ := os.ReadDir(dirPath)
			if len(entries) == 0 {
				_ = os.Remove(dirPath)
			}
		}
	}

	_ = util.WriteHashes(cwd, newHashes)

	promptedStr := ""
	if prompted > 0 {
		promptedStr = fmt.Sprintf(", %s", ui.Yellow(fmt.Sprintf("%d prompted", prompted)))
	}
	ui.Outro(fmt.Sprintf("Updated to v%s. %s, %s%s",
		newVersion,
		ui.Green(fmt.Sprintf("%d updated", updated)),
		ui.Dim(fmt.Sprintf("%d skipped", skipped)),
		promptedStr,
	))
	return nil
}

func findTemplatePath(relPath, preset string) string {
	mappings := map[string]string{
		"forge.yaml":                        "core/forge.yaml.hbs",
		"CLAUDE.md":                         "core/CLAUDE.md.hbs",
		".claude/settings.json":             "core/settings.json.hbs",
		".claude/skills/forge/SKILL.md":     "core/skill-forge.md.hbs",
		".forge/pipeline/helpers.sh":        "core/pipeline/helpers.sh.hbs",
		".forge/pipeline/intake.sh":         "core/pipeline/intake.sh.hbs",
		".forge/pipeline/classify.md":       "core/pipeline/classify.md.hbs",
		".forge/pipeline/verify.sh":         "core/pipeline/verify.sh.hbs",
		".forge/pipeline/deliver.sh":        "core/pipeline/deliver.sh.hbs",
		".forge/agents/architect.md":        "core/agents/architect.md.hbs",
		".forge/agents/quality.md":          "core/agents/quality.md.hbs",
		".forge/agents/security.md":         "core/agents/security.md.hbs",
		".forge/agents/frontend.md":         "core/agents/frontend.md.hbs",
		".forge/agents/backend.md":          "core/agents/backend.md.hbs",
		".forge/context/stack.md":           fmt.Sprintf("presets/%s/stack.md.hbs", preset),
		".forge/context/project.md":         "core/context/project.md.hbs",
		".forge/hooks/pre-edit.sh":          "core/hooks/pre-edit.sh.hbs",
		".forge/hooks/post-edit.sh":         "core/hooks/post-edit.sh.hbs",
		".forge/hooks/session-start.sh":     "core/hooks/session-start.sh.hbs",
	}
	return mappings[relPath]
}

func buildUpgradeContext(config map[string]any, detected detect.DetectedStack, preset string) render.Ctx {
	project, _ := config["project"].(map[string]any)
	commands, _ := config["commands"].(map[string]any)
	agents := toStringSlice(config["agents"])
	pipeline, _ := config["pipeline"].(map[string]any)
	isFrontend := preset == "sveltekit-ts" || preset == "react-next-ts" || preset == "vue-nuxt-ts"

	name, _ := project["name"].(string)
	if name == "" {
		name = "my-app"
	}

	autoPR := true
	if pipeline != nil {
		if v, ok := pipeline["auto_pr"].(bool); ok {
			autoPR = v
		}
	}

	cmdMap := map[string]any{}
	if commands != nil {
		cmdMap = commands
	}

	return render.Ctx{
		"project": map[string]any{
			"name":   name,
			"preset": preset,
		},
		"commands":     cmdMap,
		"agents":       agents,
		"has_frontend": isFrontend,
		"has_format":   commands != nil && commands["format"] != nil && commands["format"] != "",
		"auto_pr":      autoPR,
		"detected": map[string]any{
			"language":  detected.Language,
			"framework": detected.Framework,
			"features":  map[string]any{"git": detected.Features.Git, "ci": detected.Features.CI, "docker": detected.Features.Docker},
		},
		"preset":     preset,
		"is_sveltekit": preset == "sveltekit-ts",
		"is_nextjs":    preset == "react-next-ts",
		"is_fastapi":   preset == "python-fastapi",
		"is_go":        preset == "go",
		"stackFile":    ".forge/context/stack.md",
		"projectFile":  ".forge/context/project.md",
	}
}
