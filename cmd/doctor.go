package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Diagnose harness health: check files, scripts, deps, and config",
		RunE:  runDoctor,
	})
}

type check struct {
	Name   string
	Passed bool
	Detail string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	var checks []check

	ui.Intro(ui.Bold("forge doctor"))
	spinner := ui.NewSpinner()
	spinner.Start("Running diagnostics...")

	// 1. forge.yaml exists and is valid
	forgeYAML := filepath.Join(cwd, "forge.yaml")
	if util.Exists(forgeYAML) {
		var config map[string]any
		if err := util.ReadYAML(forgeYAML, &config); err == nil {
			project, _ := config["project"].(map[string]any)
			preset, _ := project["preset"].(string)
			checks = append(checks, check{"forge.yaml", true, "preset: " + preset})
		} else {
			checks = append(checks, check{"forge.yaml", false, "Invalid YAML"})
		}
	} else {
		checks = append(checks, check{"forge.yaml", false, "Not found. Run `forge init`"})
	}

	// 2. CLAUDE.md
	checks = append(checks, check{
		"CLAUDE.md",
		util.Exists(filepath.Join(cwd, "CLAUDE.md")),
		iff(!util.Exists(filepath.Join(cwd, "CLAUDE.md")), "Not found", ""),
	})

	// 3. Global library (~/.forge/)
	globalHome := resolve.ForgeHome()
	globalSetup := resolve.IsGlobalSetup()
	checks = append(checks, check{
		"~/.forge/ library",
		globalSetup,
		iff(!globalSetup, "Not found. Run `forge setup`", globalHome),
	})

	// 4. Pipeline files (check global, then local override)
	pipelineFiles := []string{"intake.sh", "classify.md", "review-plan.md", "verify.sh", "deliver.sh", "helpers.sh"}
	pipelineMissing := 0
	for _, f := range pipelineFiles {
		if resolve.ResolvePipeline(cwd, f) == "" {
			pipelineMissing++
		}
	}
	checks = append(checks, check{
		"Pipeline scripts",
		pipelineMissing == 0,
		iff(pipelineMissing > 0, fmt.Sprintf("%d missing", pipelineMissing), fmt.Sprintf("%d files OK", len(pipelineFiles))),
	})

	// 5. Scripts executable
	nonExec := 0
	for _, f := range pipelineFiles {
		if strings.HasSuffix(f, ".sh") {
			path := resolve.ResolvePipeline(cwd, f)
			if path != "" && !isExecutable(path) {
				nonExec++
			}
		}
	}
	for _, f := range []string{"pre-edit.sh", "post-edit.sh", "session-start.sh"} {
		path := resolve.ResolveHook(cwd, f)
		if path != "" && !isExecutable(path) {
			nonExec++
		}
	}
	checks = append(checks, check{
		"Scripts executable",
		nonExec == 0,
		iff(nonExec > 0, fmt.Sprintf("%d not executable. Run: chmod +x ~/.forge/pipeline/*.sh ~/.forge/hooks/*.sh", nonExec), "All executable"),
	})

	// 6. Agent definitions (check global + local)
	agentInfos := resolve.ListAgents(cwd)
	checks = append(checks, check{
		"Agent definitions",
		len(agentInfos) > 0,
		iff(len(agentInfos) == 0, "No agents found. Run `forge setup`", fmt.Sprintf("%d agents available", len(agentInfos))),
	})

	// 6. Context files
	checks = append(checks, check{"context/stack.md", util.Exists(filepath.Join(cwd, ".forge", "context", "stack.md")), ""})
	checks = append(checks, check{"context/project.md", util.Exists(filepath.Join(cwd, ".forge", "context", "project.md")), ""})

	// 7. bd installed
	bdInstalled := util.WhichExists("bd")
	bdInit := util.Exists(filepath.Join(cwd, ".beads"))
	detail := "Not installed. Install: brew install beads"
	if bdInstalled && bdInit {
		detail = "Installed and initialized"
	} else if bdInstalled {
		detail = "Installed but not initialized. Run: bd init --quiet"
	}
	checks = append(checks, check{"bd (beads) installed", bdInstalled, detail})

	// 8. Claude Code settings
	settingsPath := filepath.Join(cwd, ".claude", "settings.json")
	if util.Exists(settingsPath) {
		data, err := util.ReadText(settingsPath)
		if err == nil {
			var settings map[string]any
			if json.Unmarshal([]byte(data), &settings) == nil {
				hooks, _ := settings["hooks"].(map[string]any)
				hooksDetail := "No hooks registered"
				if len(hooks) > 0 {
					hooksDetail = "Hooks registered"
				}
				checks = append(checks, check{".claude/settings.json", true, hooksDetail})
			} else {
				checks = append(checks, check{".claude/settings.json", false, "Invalid JSON"})
			}
		}
	} else {
		checks = append(checks, check{".claude/settings.json", false, "Not found"})
	}

	// 9-10. Tool checks
	checks = append(checks, check{"jq installed", util.WhichExists("jq"), iff(!util.WhichExists("jq"), "Required for pipeline scripts. Install: brew install jq", "")})
	checks = append(checks, check{"gh CLI installed", util.WhichExists("gh"), iff(!util.WhichExists("gh"), "Required for PR creation. Install: brew install gh", "")})

	spinner.Stop("Diagnostics complete")

	// Display
	passed := 0
	failed := 0
	var lines []string
	for _, c := range checks {
		if c.Passed {
			passed++
			icon := ui.Green("✓")
			detail := ""
			if c.Detail != "" {
				detail = ui.Dim(" — " + c.Detail)
			}
			lines = append(lines, fmt.Sprintf("  %s %s%s", icon, c.Name, detail))
		} else {
			failed++
			icon := ui.Red("✗")
			detail := ""
			if c.Detail != "" {
				detail = ui.Dim(" — " + c.Detail)
			}
			lines = append(lines, fmt.Sprintf("  %s %s%s", icon, c.Name, detail))
		}
	}

	ui.Note(strings.Join(lines, "\n"), "Health Check")

	if failed > 0 {
		ui.Outro(ui.Yellow(fmt.Sprintf("%d passed, %d failed", passed, failed)))
	} else {
		ui.Outro(ui.Green(fmt.Sprintf("All %d checks passed", passed)))
	}
	return nil
}

func iff(cond bool, t, f string) string {
	if cond {
		return t
	}
	return f
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}
