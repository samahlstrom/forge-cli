package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/samahlstrom/forge-cli/internal/detect"
	"github.com/samahlstrom/forge-cli/internal/render"
	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/static"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

var (
	upgradeForce  bool
	upgradeGlobal bool
)

func init() {
	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade forge — global library and/or project files",
		Long: `Upgrades forge files to the latest version.

By default, upgrades both ~/.forge/ (global library) and the current project.
Use --global to upgrade only the global library (no project needed).`,
		RunE: runUpgrade,
	}
	upgradeCmd.Flags().BoolVar(&upgradeForce, "force", false, "Overwrite all files without prompting")
	upgradeCmd.Flags().BoolVar(&upgradeGlobal, "global", false, "Upgrade only the global library (~/.forge/)")
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

	// Guard: prevent upgrading forge inside its own source repo
	if isForgeSelfRepo(cwd) {
		ui.Log.Error("Cannot run `forge upgrade` inside the forge-cli source repository.")
		return fmt.Errorf("self-upgrade blocked")
	}

	ui.Intro(ui.Bold("forge upgrade"))

	// Always upgrade global library
	ui.Log.Info(fmt.Sprintf("Upgrading global library at %s", resolve.ForgeHome()))
	globalCount, err := bootstrapGlobal(resolve.ForgeHome(), upgradeForce)
	if err != nil {
		return fmt.Errorf("global upgrade failed: %w", err)
	}
	ui.Log.Success(fmt.Sprintf("Global library: %d files updated", globalCount))

	// If --global only, stop here
	if upgradeGlobal {
		ui.Outro(fmt.Sprintf("Global library upgraded to v%s.", static.Version))
		return nil
	}

	// Project upgrade — only project-local files
	if !util.Exists(filepath.Join(cwd, "forge.yaml")) {
		ui.Log.Step("No forge.yaml found in current directory — skipping project upgrade.")
		ui.Outro(fmt.Sprintf("Global library upgraded to v%s.", static.Version))
		return nil
	}

	hashes, _ := util.ReadHashes(cwd)
	newVersion := static.Version

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
	newHashes := util.HashManifest{Version: newVersion, Files: map[string]string{}}

	for relPath, installedHash := range hashes.Files {
		// Only upgrade project-local files
		if !isProjectFile(relPath) {
			continue
		}

		// Never touch user-owned files
		if neverTouch[relPath] {
			newHashes.Files[relPath] = installedHash
			skipped++
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
			ui.Log.Success(fmt.Sprintf("%s %s", relPath, ui.Dim("— updated")))
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
		}
	}

	// Merge forge.yaml (add new keys only)
	templateContent, err := fs.ReadFile(static.TemplatesFS, filepath.Join("templates", "core", "forge.yaml.hbs"))
	if err == nil {
		newForgeYAML := render.Render(string(templateContent), ctx)
		existingForgeYAML, _ := util.ReadText(filepath.Join(cwd, "forge.yaml"))
		merged, mergeErr := render.MergeForgeYAML(existingForgeYAML, newForgeYAML)
		if mergeErr == nil && merged != existingForgeYAML {
			_ = util.WriteText(filepath.Join(cwd, "forge.yaml"), merged)
			ui.Log.Success("forge.yaml — merged new fields")
			updated++
		}
	}

	// Clean up deprecated per-project files (agents, pipeline, hooks now live in ~/.forge/)
	deprecatedProjectFiles := []string{
		".forge/pipeline/classify.sh",
		".forge/pipeline/orchestrator.sh",
		".forge/pipeline/helpers.sh",
		".forge/pipeline/intake.sh",
		".forge/pipeline/classify.md",
		".forge/pipeline/review-plan.md",
		".forge/pipeline/verify.sh",
		".forge/pipeline/deliver.sh",
		".forge/pipeline/browser-smoke.sh",
		".forge/hooks/pre-edit.sh",
		".forge/hooks/post-edit.sh",
		".forge/hooks/session-start.sh",
	}
	for _, dep := range deprecatedProjectFiles {
		depPath := filepath.Join(cwd, dep)
		if util.Exists(depPath) {
			_ = os.Remove(depPath)
			ui.Log.Step(fmt.Sprintf("%s %s", dep, ui.Dim("— moved to ~/.forge/, removed local copy")))
		}
	}

	// Clean up deprecated agent files (now in ~/.forge/agents/)
	agentsDir := filepath.Join(cwd, ".forge", "agents")
	if util.Exists(agentsDir) {
		entries, _ := os.ReadDir(agentsDir)
		if len(entries) == 0 {
			_ = os.Remove(agentsDir)
		}
	}

	// Clean up deprecated directories
	for _, dir := range []string{".claude/skills/deliver", ".forge/hooks", ".forge/addons", ".forge/state"} {
		dirPath := filepath.Join(cwd, dir)
		if util.Exists(dirPath) {
			entries, _ := os.ReadDir(dirPath)
			if len(entries) == 0 {
				_ = os.Remove(dirPath)
			}
		}
	}

	_ = util.WriteHashes(cwd, newHashes)

	ui.Outro(fmt.Sprintf("Upgraded to v%s. %s project files updated, %s skipped.",
		newVersion,
		ui.Green(fmt.Sprintf("%d", updated)),
		ui.Dim(fmt.Sprintf("%d", skipped)),
	))
	return nil
}

// isProjectFile returns true if the file is a project-local file (not a global library file).
func isProjectFile(relPath string) bool {
	projectFiles := map[string]bool{
		"forge.yaml":              true,
		"CLAUDE.md":               true,
		".claude/settings.json":   true,
		".forge/context/stack.md": true,
		".forge/context/project.md": true,
	}
	return projectFiles[relPath]
}

func findTemplatePath(relPath, preset string) string {
	mappings := map[string]string{
		"forge.yaml":              "core/forge.yaml.hbs",
		"CLAUDE.md":               "core/CLAUDE.md.hbs",
		".claude/settings.json":   "core/settings.json.hbs",
		".forge/context/stack.md": fmt.Sprintf("presets/%s/stack.md.hbs", preset),
		".forge/context/project.md": "core/context/project.md.hbs",
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
