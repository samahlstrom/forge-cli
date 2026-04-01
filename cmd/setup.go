package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/render"
	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/static"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

var setupForce bool

func init() {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap your personal forge library at ~/.forge/",
		Long: `Sets up your personal forge library with agents, pipeline scripts,
hooks, skills, and presets. This is your workflow — shared across all projects.

Run this once. After that, use 'forge agent edit', 'forge upgrade --global',
or edit files in ~/.forge/ directly to evolve your workflow.`,
		RunE: runSetup,
	}
	cmd.Flags().BoolVar(&setupForce, "force", false, "Overwrite all files without prompting")
	rootCmd.AddCommand(cmd)
}

// globalFileMap defines all files that live in ~/.forge/.
// Format: [templatePath, outputRelPath (relative to ~/.forge/)]
var globalFileMap = [][2]string{
	// Skills
	{"core/skill-forge.md.hbs", "skills/forge/SKILL.md"},
	{"core/skill-creator.md.hbs", "skills/skill-creator/SKILL.md"},
	{"core/skill-ingest.md.hbs", "skills/ingest/SKILL.md"},

	// Pipeline scripts
	{"core/pipeline/helpers.sh.hbs", "pipeline/helpers.sh"},
	{"core/pipeline/intake.sh.hbs", "pipeline/intake.sh"},
	{"core/pipeline/classify.md.hbs", "pipeline/classify.md"},
	{"core/pipeline/review-plan.md.hbs", "pipeline/review-plan.md"},
	{"core/pipeline/verify.sh.hbs", "pipeline/verify.sh"},
	{"core/pipeline/deliver.sh.hbs", "pipeline/deliver.sh"},
	{"core/pipeline/browser-smoke.sh.hbs", "pipeline/browser-smoke.sh"},

	// Agents
	{"core/agents/architect.md.hbs", "agents/architect.md"},
	{"core/agents/quality.md.hbs", "agents/quality.md"},
	{"core/agents/security.md.hbs", "agents/security.md"},
	{"core/agents/edgar.md.hbs", "agents/edgar.md"},
	{"core/agents/code-quality.md.hbs", "agents/code-quality.md"},
	{"core/agents/um-actually.md.hbs", "agents/um-actually.md"},
	{"core/agents/visual-qa.md.hbs", "agents/visual-qa.md"},
	{"core/agents/frontend.md.hbs", "agents/frontend.md"},
	{"core/agents/backend.md.hbs", "agents/backend.md"},

	// Hooks
	{"core/hooks/pre-edit.sh.hbs", "hooks/pre-edit.sh"},
	{"core/hooks/post-edit.sh.hbs", "hooks/post-edit.sh"},
	{"core/hooks/session-start.sh.hbs", "hooks/session-start.sh"},
}

// globalPresetMap defines preset stack files.
var globalPresetMap = [][2]string{
	{"presets/go/stack.md.hbs", "presets/go.md"},
	{"presets/sveltekit-ts/stack.md.hbs", "presets/sveltekit-ts.md"},
	{"presets/react-next-ts/stack.md.hbs", "presets/react-next-ts.md"},
	{"presets/python-fastapi/stack.md.hbs", "presets/python-fastapi.md"},
}

func runSetup(_ *cobra.Command, _ []string) error {
	home := resolve.ForgeHome()

	if resolve.IsGlobalSetup() && !setupForce {
		ui.Log.Step(fmt.Sprintf("~/.forge/ already exists at %s", home))
		ui.Log.Step("Use --force to overwrite, or edit files directly.")
		return nil
	}

	ui.Intro("Setting up personal forge library")
	ui.Log.Step(fmt.Sprintf("Location: %s", home))

	count, err := bootstrapGlobal(home, setupForce)
	if err != nil {
		return err
	}

	ui.Log.Success(fmt.Sprintf("Setup complete: %d files written to %s", count, home))
	ui.Log.Step("Your agents, skills, pipeline, and hooks are ready.")
	ui.Log.Step("Edit them anytime with: forge agent edit <name>")
	ui.Log.Step("Run 'forge init' in a project to connect it to your library.")
	return nil
}

// bootstrapGlobal writes all global files to ~/.forge/.
// Returns the number of files written.
func bootstrapGlobal(home string, force bool) (int, error) {
	ctx := globalTemplateContext()
	hashes := util.HashManifest{Version: static.Version, Files: map[string]string{}}
	count := 0

	allFiles := append(globalFileMap, globalPresetMap...)

	for _, pair := range allFiles {
		templateRel := pair[0]
		outputRel := pair[1]
		outputPath := filepath.Join(home, outputRel)

		templateData, err := fs.ReadFile(static.TemplatesFS, filepath.Join("templates", templateRel))
		if err != nil {
			continue
		}

		content := render.Render(string(templateData), ctx)

		if !force && util.Exists(outputPath) {
			existingHash, _ := util.HashFile(outputPath)
			if storedHash, ok := hashes.Files[outputRel]; ok && existingHash != storedHash {
				ui.Log.Step(fmt.Sprintf("Kept modified %s", outputRel))
				continue
			}
		}

		if err := util.WriteText(outputPath, content); err != nil {
			return count, fmt.Errorf("failed to write %s: %w", outputRel, err)
		}

		if strings.HasSuffix(outputRel, ".sh") {
			_ = os.Chmod(outputPath, 0o755)
		}

		hashes.Files[outputRel] = util.HashContent(content)
		count++
	}

	// Write config.yaml scaffold if it doesn't exist
	configPath := filepath.Join(home, "config.yaml")
	if !util.Exists(configPath) {
		configContent := "# ~/.forge/config.yaml — global forge preferences\n# Edit this file to customize your workflow across all projects.\n\nversion: 1\n"
		if err := util.WriteText(configPath, configContent); err != nil {
			return count, fmt.Errorf("failed to write config.yaml: %w", err)
		}
		count++
	}

	// Write hash manifest for global files
	hashPath := filepath.Join(home, ".hashes.json")
	hashData, err := json.MarshalIndent(hashes, "", "  ")
	if err != nil {
		return count, fmt.Errorf("failed to marshal hashes: %w", err)
	}
	if err := util.WriteText(hashPath, string(hashData)); err != nil {
		return count, fmt.Errorf("failed to write hashes: %w", err)
	}

	return count, nil
}

// globalTemplateContext provides a minimal context for rendering global templates.
// No project-specific variables — those are read at runtime from forge.yaml.
func globalTemplateContext() render.Ctx {
	return render.Ctx{
		"project": map[string]any{
			"name":   "",
			"preset": "",
		},
		"commands": map[string]any{
			"typecheck": "",
			"lint":      "",
			"test":      "",
			"format":    "",
			"dev":       "",
		},
		"agents":       []any{},
		"has_frontend":  true,
		"has_backend":   true,
		"has_format":    false,
		"auto_pr":       true,
		"detected": map[string]any{
			"language":  "",
			"framework": "",
			"features": map[string]any{
				"git": true, "ci": false, "docker": false,
				"playwright": false, "semgrep": false,
				"firebase": false, "vercel": false,
			},
		},
		"preset":      "",
		"is_sveltekit": false,
		"is_nextjs":    false,
		"is_fastapi":   false,
		"is_go":        false,
		"onboarding": map[string]any{
			"description":    "",
			"projectType":    "",
			"modules":        []any{},
			"architecture":   "",
			"sensitivePaths": "",
			"domainRules":    "",
		},
		"has_sensitive":    false,
		"has_domain_rules": false,
		"has_modules":      false,
	}
}
