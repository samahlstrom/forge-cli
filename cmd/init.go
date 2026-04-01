package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samahlstrom/forge-cli/internal/detect"
	"github.com/samahlstrom/forge-cli/internal/render"
	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/static"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

// --- flags ---

var (
	initPreset string
	initForce  bool
	initYes    bool
	initSpec   string
)

func init() {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the Forge agent harness in the current project",
		RunE:  runInit,
	}
	cmd.Flags().StringVar(&initPreset, "preset", "", "Stack preset (sveltekit-ts, react-next-ts, python-fastapi, go)")
	cmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing harness without asking")
	cmd.Flags().BoolVar(&initYes, "yes", false, "Accept all defaults non-interactively")
	cmd.Flags().StringVar(&initSpec, "spec", "", "Path to a spec file to analyze for project context")
	rootCmd.AddCommand(cmd)
}

// --- types ---

type specAnalysis struct {
	ProjectName    string   `json:"project_name"`
	Description    string   `json:"description"`
	Language       string   `json:"language"`
	Framework      *string  `json:"framework"`
	ProjectType    string   `json:"project_type"`
	Modules        []string `json:"modules"`
	Architecture   string   `json:"architecture"`
	SensitiveAreas string   `json:"sensitive_areas"`
	DomainRules    string   `json:"domain_rules"`
	Constraints    []string `json:"constraints"`
	PageCount      *int     `json:"page_count"`
}

type initAnswers struct {
	preset             string
	commands           defaultCommands
	autoPr             bool
	projectName        string
	projectDescription string
	projectType        string
	keyModules         []string
	architectureStyle  string
	sensitivePaths     string
	domainRules        string
}

type defaultCommands struct {
	typecheck string
	lint      string
	test      string
	format    string
	dev       string
}

type generatedFile struct {
	relativePath string
	content      string
}

// --- constants ---

var availablePresets = []string{"sveltekit-ts", "react-next-ts", "python-fastapi", "go"}

var languageOptions = []ui.SelectOption{
	{Value: "typescript", Label: "TypeScript"},
	{Value: "javascript", Label: "JavaScript"},
	{Value: "python", Label: "Python"},
	{Value: "go", Label: "Go"},
}

type frameworkOption struct {
	Value  string
	Label  string
	Preset string
}

var frameworkOptions = map[string][]frameworkOption{
	"typescript": {
		{Value: "next", Label: "Next.js", Preset: "react-next-ts"},
		{Value: "sveltekit", Label: "SvelteKit", Preset: "sveltekit-ts"},
		{Value: "other", Label: "Other / None", Preset: "react-next-ts"},
	},
	"javascript": {
		{Value: "next", Label: "Next.js", Preset: "react-next-ts"},
		{Value: "sveltekit", Label: "SvelteKit", Preset: "sveltekit-ts"},
		{Value: "other", Label: "Other / None", Preset: "react-next-ts"},
	},
	"python": {
		{Value: "fastapi", Label: "FastAPI", Preset: "python-fastapi"},
		{Value: "django", Label: "Django", Preset: "python-fastapi"},
		{Value: "flask", Label: "Flask", Preset: "python-fastapi"},
		{Value: "other", Label: "Other / None", Preset: "python-fastapi"},
	},
	"go": {
		{Value: "gin", Label: "Gin", Preset: "go"},
		{Value: "chi", Label: "Chi", Preset: "go"},
		{Value: "fiber", Label: "Fiber", Preset: "go"},
		{Value: "other", Label: "Other / None", Preset: "go"},
	},
}

var defaultCmds = map[string]defaultCommands{
	"typescript": {typecheck: "npx tsc --noEmit", lint: "npm run lint", test: "npx vitest run", format: "npx prettier --write .", dev: "npm run dev"},
	"javascript": {typecheck: `echo "no typecheck"`, lint: "npm run lint", test: "npx vitest run", format: "npx prettier --write .", dev: "npm run dev"},
	"python":     {typecheck: "mypy .", lint: "ruff check .", test: "pytest", format: "ruff format .", dev: "uvicorn app.main:app --reload"},
	"go":         {typecheck: "go vet ./...", lint: "golangci-lint run", test: "go test ./...", format: "gofmt -w .", dev: "go run ."},
}

var projectTypeOptions = []ui.SelectOption{
	{Value: "web-app", Label: "Web application — frontend + backend"},
	{Value: "api", Label: "API / Backend service — no frontend"},
	{Value: "cli", Label: "CLI tool — command-line interface"},
	{Value: "library", Label: "Library / Package — consumed by other projects"},
	{Value: "automation", Label: "Automation / Scripts — GitHub Actions, bots, pipelines"},
	{Value: "fullstack", Label: "Full-stack monorepo — multiple apps in one repo"},
}

var architectureOptions = []ui.SelectOption{
	{Value: "monolith", Label: "Monolith — single deployable unit"},
	{Value: "client-server", Label: "Client + Server — separate frontend and backend"},
	{Value: "microservices", Label: "Microservices — multiple independent services"},
	{Value: "static-site", Label: "Static site — pre-rendered or JAMstack"},
	{Value: "library", Label: "Library / Package — consumed by other projects"},
}

// --- main command ---

func runInit(_ *cobra.Command, _ []string) error {
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold(fmt.Sprintf("forge v%s", static.Version)) + ui.Dim(" — Agent Harness for Claude Code"))

	// Check for existing harness
	if !initForce && util.Exists(filepath.Join(cwd, "forge.yaml")) {
		overwrite, cancelled := ui.Confirm("A forge.yaml already exists. Overwrite?", false)
		if cancelled || !overwrite {
			ui.Cancel("Init cancelled. Use --force to overwrite.")
			os.Exit(0)
		}
	}

	// Phase 1: Detect
	spinner := ui.NewSpinner()
	spinner.Start("Scanning project...")
	detected := detect.Detect(cwd)
	spinner.Stop("Scan complete")

	displayDetectedStack(detected)

	// Phase 2: Spec analysis
	var analysis *specAnalysis
	var specId string

	if initSpec != "" {
		specPath, _ := filepath.Abs(initSpec)
		if !util.Exists(specPath) {
			ui.Cancel(fmt.Sprintf("Spec file not found: %s", specPath))
			os.Exit(1)
		}

		spinner.Start("Analyzing spec with Claude Code...")
		result, err := analyzeSpecForInit(specPath)
		if err != nil {
			spinner.Stop("Spec analysis failed")
			ui.Log.Warn(fmt.Sprintf("Could not analyze spec: %s", err))
			ui.Log.Warn(ui.Dim("Falling back to manual onboarding."))
		} else {
			spinner.Stop("Spec analysis complete")
			analysis = result
		}

		if analysis != nil {
			lines := []string{
				fmt.Sprintf("Project:      %s", ui.Cyan(analysis.ProjectName)),
				fmt.Sprintf("Description:  %s", ui.Cyan(analysis.Description)),
				fmt.Sprintf("Type:         %s", ui.Cyan(analysis.ProjectType)),
				fmt.Sprintf("Language:     %s", ui.Cyan(analysis.Language)),
			}
			if analysis.Framework != nil {
				lines = append(lines, fmt.Sprintf("Framework:    %s", ui.Cyan(*analysis.Framework)))
			}
			if len(analysis.Modules) > 0 {
				lines = append(lines, fmt.Sprintf("Modules:      %s", ui.Cyan(strings.Join(analysis.Modules, ", "))))
			}
			lines = append(lines, fmt.Sprintf("Architecture: %s", ui.Cyan(analysis.Architecture)))
			if analysis.SensitiveAreas != "" {
				lines = append(lines, fmt.Sprintf("Sensitive:    %s", ui.Cyan(analysis.SensitiveAreas)))
			}
			if len(analysis.Constraints) > 0 {
				limit := 3
				if len(analysis.Constraints) < limit {
					limit = len(analysis.Constraints)
				}
				lines = append(lines, fmt.Sprintf("Constraints:  %s", ui.Cyan(strings.Join(analysis.Constraints[:limit], "; "))))
			}

			ui.Note(strings.Join(lines, "\n"), "Extracted from spec")

			confirmed, cancelled := ui.Confirm("Does this look right?", true)
			if cancelled {
				ui.Cancel("Cancelled.")
				os.Exit(0)
			}

			if !confirmed {
				corrections, cancelled := ui.Text("What needs to change?", "e.g. Use SvelteKit instead of Next.js")
				if cancelled {
					ui.Cancel("Cancelled.")
					os.Exit(0)
				}
				if corrections != "" {
					spinner.Start("Re-analyzing with corrections...")
					reResult, err := analyzeSpecForInit(specPath)
					if err != nil {
						spinner.Stop("Re-analysis failed, using original")
					} else {
						spinner.Stop("Updated")
						analysis = reResult
					}
				}
			}
		}

		// Copy spec into .forge/specs/
		specId = fmt.Sprintf("spec-%s", util.RandomHex(4))
		specDir := filepath.Join(cwd, ".forge", "specs", specId)
		_ = util.EnsureDir(specDir)
		_ = util.CopyFile(specPath, filepath.Join(specDir, "source"+filepath.Ext(specPath)))

		if analysis != nil {
			analysisJSON, _ := json.MarshalIndent(analysis, "", "  ")
			_ = util.WriteText(filepath.Join(specDir, "analysis.json"), string(analysisJSON))
		}

		meta := map[string]any{
			"spec_id": specId,
			"source": map[string]any{
				"file":   filepath.Base(specPath),
				"format": strings.TrimPrefix(filepath.Ext(specPath), "."),
			},
			"status":      "pending-analysis",
			"ingested_at": time.Now().UTC().Format(time.RFC3339),
		}
		metaJSON, _ := json.MarshalIndent(meta, "", "  ")
		_ = util.WriteText(filepath.Join(specDir, "meta.json"), string(metaJSON))
	}

	// Phase 3: Ask questions
	answers := askInitQuestions(detected, analysis)

	// Phase 4: Generate
	spinner.Start("Generating harness...")
	files, err := generateHarness(cwd, detected, answers)
	if err != nil {
		spinner.Stop("Generation failed")
		ui.Log.Error(err.Error())
		os.Exit(1)
	}
	spinner.Stop(fmt.Sprintf("%d files created", len(files)))

	// Phase 5: Display results
	displayInitResults(files, specId)

	ui.Log.Warn(ui.Yellow("If you ran this inside Claude Code, restart the session so it picks up the new settings, skills, and hooks."))
	if specId != "" {
		ui.Outro(ui.Green("Harness ready!") + ui.Dim(fmt.Sprintf(` Tell your agent: /ingest %s`, specId)))
	} else {
		ui.Outro(ui.Green("Harness ready!") + ui.Dim(` Tell your agent: /forge "what you want to build"`))
	}
	return nil
}

// --- detect display ---

func displayDetectedStack(detected detect.DetectedStack) {
	var lines []string

	if detected.Language != "unknown" {
		lines = append(lines, fmt.Sprintf("Language:    %s", ui.Cyan(detected.Language)))
	}
	if detected.Framework != "" {
		lines = append(lines, fmt.Sprintf("Framework:   %s", ui.Cyan(detected.Framework)))
	}
	if detected.TestRunner != nil {
		lines = append(lines, fmt.Sprintf("Testing:     %s", ui.Cyan(detected.TestRunner.Name)))
	}
	if detected.Linter != nil {
		lines = append(lines, fmt.Sprintf("Linting:     %s", ui.Cyan(detected.Linter.Name)))
	}
	if detected.TypeChecker != nil {
		lines = append(lines, fmt.Sprintf("Type check:  %s", ui.Cyan(detected.TypeChecker.Name)))
	}
	if detected.Features.Git {
		lines = append(lines, fmt.Sprintf("VCS:         %s", ui.Cyan("Git")))
	}

	if len(lines) > 0 {
		ui.Note(strings.Join(lines, "\n"), "Detected")
	} else {
		ui.Log.Warn("Could not auto-detect project stack. You will need to select a preset manually.")
	}
}

// --- questions ---

func askInitQuestions(detected detect.DetectedStack, analysis *specAnalysis) initAnswers {
	projectName := filepath.Base(mustCwd())
	if analysis != nil && analysis.ProjectName != "" {
		projectName = analysis.ProjectName
	}

	nothingDetected := detected.Language == "unknown"
	// Auto-mode: detection succeeded and we have a preset — skip interactive questions
	autoMode := !nothingDetected && detected.Preset != "" && initPreset == ""

	// --- Stack selection ---
	preset := initPreset
	chosenLanguage := detected.Language
	if chosenLanguage == "unknown" {
		chosenLanguage = "typescript"
	}
	var cmds defaultCommands

	// If spec analysis provided language/framework, use those
	if analysis != nil && preset == "" {
		if analysis.Language != "" {
			chosenLanguage = analysis.Language
		}
		frameworkMap := map[string]string{
			"next": "react-next-ts", "sveltekit": "sveltekit-ts",
			"fastapi": "python-fastapi", "django": "python-fastapi", "flask": "python-fastapi",
			"gin": "go", "chi": "go", "fiber": "go",
		}
		if analysis.Framework != nil {
			if p, ok := frameworkMap[*analysis.Framework]; ok {
				preset = p
			}
		}
		if preset == "" {
			langPresets := map[string]string{
				"typescript": "react-next-ts", "javascript": "react-next-ts",
				"python": "python-fastapi", "go": "go",
			}
			if p, ok := langPresets[chosenLanguage]; ok {
				preset = p
			} else {
				preset = "react-next-ts"
			}
		}
	} else if autoMode && preset == "" {
		// Detection succeeded — use detected preset without asking
		preset = detected.Preset
		ui.Log.Step(fmt.Sprintf("Using detected preset: %s", ui.Cyan(preset)))
	} else if preset == "" && !initYes {
		if nothingDetected {
			// Empty repo — try Claude inference first, fall back to interactive
			ui.Log.Step("No existing code detected — inferring project context...")
			inferred := inferProjectContext(mustCwd())
			if inferred != nil && inferred.Language != "" {
				chosenLanguage = inferred.Language
				langPresets := map[string]string{
					"typescript": "react-next-ts", "javascript": "react-next-ts",
					"python": "python-fastapi", "go": "go",
				}
				if p, ok := langPresets[chosenLanguage]; ok {
					preset = p
				}
				ui.Log.Step(fmt.Sprintf("Inferred: %s (%s)", ui.Cyan(chosenLanguage), ui.Cyan(preset)))
			}

			// If inference failed, ask interactively
			if preset == "" {
				ui.Log.Step("Could not infer stack — let's set it up manually.")

				langAnswer, cancelled := ui.Select("What language will you use?", languageOptions)
				if cancelled {
					ui.Cancel("Init cancelled.")
					os.Exit(0)
				}
				chosenLanguage = langAnswer

				frameworks := frameworkOptions[chosenLanguage]
				if len(frameworks) > 0 {
					opts := make([]ui.SelectOption, len(frameworks))
					for i, f := range frameworks {
						opts[i] = ui.SelectOption{Value: f.Value, Label: f.Label}
					}
					fwAnswer, cancelled := ui.Select("What framework?", opts)
					if cancelled {
						ui.Cancel("Init cancelled.")
						os.Exit(0)
					}
					for _, f := range frameworks {
						if f.Value == fwAnswer {
							preset = f.Preset
							break
						}
					}
					if preset == "" {
						preset = frameworks[0].Preset
					}
				}
			}
		} else {
			// Existing code detected but no preset matched — ask
			if detected.Preset != "" {
				preset = detected.Preset
				ui.Log.Step(fmt.Sprintf("Using detected preset: %s", ui.Cyan(preset)))
			} else {
				presetOpts := make([]ui.SelectOption, len(availablePresets))
				for i, pr := range availablePresets {
					presetOpts[i] = ui.SelectOption{Value: pr, Label: pr}
				}
				selected, cancelled := ui.Select("Could not determine preset — select one:", presetOpts)
				if cancelled {
					ui.Cancel("Init cancelled.")
					os.Exit(0)
				}
				preset = selected
			}
		}
	}

	// Fallback for --yes or if still empty
	if preset == "" {
		if detected.Preset != "" {
			preset = detected.Preset
		} else {
			preset = "react-next-ts"
		}
	}

	// --- Commands ---
	langDefaults, ok := defaultCmds[chosenLanguage]
	if !ok {
		langDefaults = defaultCmds["typescript"]
	}
	cmds = defaultCommands{
		typecheck: langDefaults.typecheck,
		lint:      langDefaults.lint,
		test:      langDefaults.test,
		format:    langDefaults.format,
		dev:       langDefaults.dev,
	}
	if detected.TypeChecker != nil {
		cmds.typecheck = detected.TypeChecker.Command
	}
	if detected.Linter != nil {
		cmds.lint = detected.Linter.Command
	}
	if detected.TestRunner != nil {
		cmds.test = detected.TestRunner.Command
	}
	if detected.Formatter != nil {
		cmds.format = detected.Formatter.Command
	}

	// Show commands for awareness (not asking to change them)
	cmdLines := []string{
		fmt.Sprintf("Typecheck: %s", ui.Cyan(cmds.typecheck)),
		fmt.Sprintf("Lint:      %s", ui.Cyan(cmds.lint)),
		fmt.Sprintf("Test:      %s", ui.Cyan(cmds.test)),
	}
	if cmds.format != "" {
		cmdLines = append(cmdLines, fmt.Sprintf("Format:    %s", ui.Cyan(cmds.format)))
	}
	ui.Note(strings.Join(cmdLines, "\n"), "Verification commands (edit in forge.yaml later)")

	// --- Auto-PR: default true, only ask if interactive and nothing was auto-detected ---
	autoPr := true
	if !initYes && !autoMode {
		result, cancelled := ui.Confirm("Auto-create PRs on delivery?", true)
		if cancelled {
			ui.Cancel("Init cancelled.")
			os.Exit(0)
		}
		autoPr = result
	}

	// --- Onboarding ---
	projectDescription := ""
	projectType := "web-app"
	var keyModules []string
	architectureStyle := "monolith"
	sensitivePaths := ""
	domainRules := ""

	if analysis != nil {
		// Spec provided all context
		projectDescription = analysis.Description
		projectType = analysis.ProjectType
		keyModules = analysis.Modules
		architectureStyle = analysis.Architecture
		sensitivePaths = analysis.SensitiveAreas
		domainRules = analysis.DomainRules
	} else if autoMode || initYes {
		// Auto-mode: infer project context from codebase using Claude
		spinner := ui.NewSpinner()
		spinner.Start("Analyzing codebase with Claude...")
		inferred := inferProjectContext(mustCwd())
		if inferred != nil {
			spinner.Stop("Project context inferred")
			projectDescription = inferred.Description
			projectType = inferred.ProjectType
			keyModules = inferred.Modules
			architectureStyle = inferred.Architecture
			sensitivePaths = inferred.SensitiveAreas
			domainRules = inferred.DomainRules
			if inferred.ProjectName != "" {
				projectName = inferred.ProjectName
			}

			lines := []string{
				fmt.Sprintf("Project:      %s", ui.Cyan(projectName)),
				fmt.Sprintf("Type:         %s", ui.Cyan(projectType)),
				fmt.Sprintf("Architecture: %s", ui.Cyan(architectureStyle)),
			}
			if projectDescription != "" {
				lines = append(lines, fmt.Sprintf("Description:  %s", ui.Cyan(projectDescription)))
			}
			if len(keyModules) > 0 {
				lines = append(lines, fmt.Sprintf("Modules:      %s", ui.Cyan(strings.Join(keyModules, ", "))))
			}
			ui.Note(strings.Join(lines, "\n"), "Inferred project context")
		} else {
			spinner.Stop("Could not infer project context")
		}
	} else {
		// Interactive: ask the user
		ui.Log.Step("Tell us about your project so agents understand what they're working on.")

		descAnswer, cancelled := ui.Text("What are you building?", "e.g. A SaaS platform for restaurant inventory management")
		if cancelled {
			ui.Cancel("Init cancelled.")
			os.Exit(0)
		}
		projectDescription = descAnswer

		typeAnswer, cancelled := ui.Select("What kind of project is this?", projectTypeOptions)
		if cancelled {
			ui.Cancel("Init cancelled.")
			os.Exit(0)
		}
		projectType = typeAnswer

		modulesAnswer, cancelled := ui.Text("What are the main features or modules?", "e.g. auth, dashboard, inventory, notifications")
		if cancelled {
			ui.Cancel("Init cancelled.")
			os.Exit(0)
		}
		keyModules = splitAndTrim(modulesAnswer, ",")

		archAnswer, cancelled := ui.Select("How is the app structured?", architectureOptions)
		if cancelled {
			ui.Cancel("Init cancelled.")
			os.Exit(0)
		}
		architectureStyle = archAnswer

		sensitiveAnswer, cancelled := ui.Text("Any sensitive areas? (leave blank to skip)", "e.g. src/auth/ handles tokens, src/payments/ has Stripe integration")
		if cancelled {
			ui.Cancel("Init cancelled.")
			os.Exit(0)
		}
		sensitivePaths = sensitiveAnswer

		domainAnswer, cancelled := ui.Text("Any domain-specific rules agents should know? (leave blank to skip)", "e.g. All prices stored in cents. Users always belong to exactly one org.")
		if cancelled {
			ui.Cancel("Init cancelled.")
			os.Exit(0)
		}
		domainRules = domainAnswer
	}

	return initAnswers{
		preset:             preset,
		commands:           cmds,
		autoPr:             autoPr,
		projectName:        projectName,
		projectDescription: projectDescription,
		projectType:        projectType,
		keyModules:         keyModules,
		architectureStyle:  architectureStyle,
		sensitivePaths:     sensitivePaths,
		domainRules:        domainRules,
	}
}

// --- generation ---

func generateHarness(cwd string, detected detect.DetectedStack, answers initAnswers) ([]generatedFile, error) {
	ctx := buildTemplateContext(detected, answers)
	var files []generatedFile
	hashes := util.HashManifest{Version: static.Version, Files: map[string]string{}}

	// Auto-bootstrap ~/.forge/ if not already set up
	if !resolve.IsGlobalSetup() {
		ui.Log.Step("First run — bootstrapping personal forge library...")
		if _, err := bootstrapGlobal(resolve.ForgeHome(), false); err != nil {
			return nil, fmt.Errorf("failed to bootstrap ~/.forge/: %w", err)
		}
		ui.Log.Success(fmt.Sprintf("Personal library created at %s", resolve.ForgeHome()))
	}

	// ZERO project footprint — all files go to ~/.forge/projects/<id>/ or ~/.claude/
	projectDir := resolve.ProjectDir(cwd)
	contextDir := resolve.ProjectContextDir(cwd)
	runsDir := resolve.ProjectRunsDir(cwd)
	projectID := resolve.ProjectID(cwd)

	_ = util.EnsureDir(contextDir)
	_ = util.EnsureDir(runsDir)

	// Project-specific files written to ~/.forge/projects/<id>/
	type projectFile struct {
		templateRel string
		outputPath  string
		label       string // display name
		userOwned   bool   // skip if exists
	}

	presetTemplatePath := fmt.Sprintf("presets/%s/stack.md.hbs", answers.preset)

	projectFiles := []projectFile{
		{"core/forge.yaml.hbs", filepath.Join(projectDir, "forge.yaml"), "forge.yaml", true},
		{"core/context/project.md.hbs", filepath.Join(contextDir, "project.md"), "context/project.md", true},
		{presetTemplatePath, filepath.Join(contextDir, "stack.md"), "context/stack.md", false},
	}

	for _, pf := range projectFiles {
		templateData, err := fs.ReadFile(static.TemplatesFS, filepath.Join("templates", pf.templateRel))
		if err != nil {
			continue
		}
		content := render.Render(string(templateData), ctx)

		if pf.userOwned && !initForce && util.Exists(pf.outputPath) {
			ui.Log.Step(fmt.Sprintf("Kept existing %s (use --force to overwrite)", pf.label))
			hashes.Files[pf.label] = util.HashContent(content)
			continue
		}

		if err := util.WriteText(pf.outputPath, content); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", pf.label, err)
		}
		files = append(files, generatedFile{relativePath: fmt.Sprintf("~/.forge/projects/%s/%s", projectID, pf.label), content: content})
		hashes.Files[pf.label] = util.HashContent(content)
	}

	// Write project metadata for lookup
	metadata := fmt.Sprintf("# Project: %s\npath: %s\npreset: %s\n", answers.projectName, cwd, answers.preset)
	_ = util.WriteText(filepath.Join(projectDir, "metadata.yaml"), metadata)

	// User-scope settings.json — write to ~/.claude/settings.json (NOT project)
	settingsTemplate, err := fs.ReadFile(static.TemplatesFS, filepath.Join("templates", "core/settings.json.hbs"))
	if err == nil {
		settingsContent := render.Render(string(settingsTemplate), ctx)
		userSettingsPath := filepath.Join(homeDir(), ".claude", "settings.json")
		settingsContent = mergeSettingsJSON(userSettingsPath, settingsContent)
		if err := util.WriteText(userSettingsPath, settingsContent); err != nil {
			ui.Log.Warn(fmt.Sprintf("Could not write ~/.claude/settings.json: %v", err))
		} else {
			files = append(files, generatedFile{relativePath: "~/.claude/settings.json", content: settingsContent})
		}
	}

	// User-scope skill stubs — write to ~/.claude/skills/ (NOT project)
	// These are tiny loaders that point Claude to the forge CLI
	installSkillStubs()

	// Write hash manifest to project dir (not the repo)
	hashPath := filepath.Join(projectDir, ".hashes.json")
	hashes.Version = static.Version
	if hashData, err := json.MarshalIndent(hashes, "", "  "); err == nil {
		_ = util.WriteText(hashPath, string(hashData))
	}

	// Initialize bd (beads) — best effort, not critical
	initBd(cwd)

	return files, nil
}

func buildTemplateContext(detected detect.DetectedStack, answers initAnswers) render.Ctx {
	projectType := answers.projectType
	needsFrontend := projectType == "web-app" || projectType == "fullstack"
	needsBackend := projectType == "web-app" || projectType == "api" || projectType == "fullstack" || projectType == "microservices"

	agents := []any{"architect", "quality", "security", "edgar", "code-quality", "um-actually", "visual-qa"}
	if needsFrontend {
		agents = append(agents, "frontend")
	}
	if needsBackend {
		agents = append(agents, "backend")
	}

	// Convert modules to []any for template engine
	var modulesAny []any
	for _, m := range answers.keyModules {
		modulesAny = append(modulesAny, m)
	}

	return render.Ctx{
		"project": map[string]any{
			"name":   answers.projectName,
			"preset": answers.preset,
		},
		"commands": map[string]any{
			"typecheck": answers.commands.typecheck,
			"lint":      answers.commands.lint,
			"test":      answers.commands.test,
			"format":    answers.commands.format,
			"dev":       answers.commands.dev,
		},
		"agents":       agents,
		"has_frontend":  needsFrontend,
		"has_backend":   needsBackend,
		"has_format":    answers.commands.format != "",
		"auto_pr":       answers.autoPr,
		"detected": map[string]any{
			"language":  detected.Language,
			"framework": detected.Framework,
			"features": map[string]any{
				"git":        detected.Features.Git,
				"ci":         detected.Features.CI,
				"docker":     detected.Features.Docker,
				"playwright": detected.Features.Playwright,
				"semgrep":    detected.Features.Semgrep,
				"firebase":   detected.Features.Firebase,
				"vercel":     detected.Features.Vercel,
			},
		},
		"preset":      answers.preset,
		"stackFile":    ".forge/context/stack.md",
		"projectFile":  ".forge/context/project.md",
		"is_sveltekit": answers.preset == "sveltekit-ts",
		"is_nextjs":    answers.preset == "react-next-ts",
		"is_fastapi":   answers.preset == "python-fastapi",
		"is_go":        answers.preset == "go",
		"onboarding": map[string]any{
			"description":    answers.projectDescription,
			"projectType":    answers.projectType,
			"modules":        modulesAny,
			"architecture":   answers.architectureStyle,
			"sensitivePaths": answers.sensitivePaths,
			"domainRules":    answers.domainRules,
		},
		"has_sensitive":    answers.sensitivePaths != "",
		"has_domain_rules": answers.domainRules != "",
		"has_modules":      len(answers.keyModules) > 0,
	}
}

// --- spec analysis ---

func analyzeSpecForInit(specPath string) (*specAnalysis, error) {
	prompt := fmt.Sprintf(`You are analyzing a project specification document to extract structured metadata for an agent harness.

Read the spec at: %s

Return ONLY a JSON object with these fields:
{
  "project_name": "short project name",
  "description": "one-sentence description of what is being built",
  "language": "typescript|javascript|python|go",
  "framework": "next|sveltekit|fastapi|django|flask|gin|chi|fiber|null",
  "project_type": "web-app|api|cli|library|automation|fullstack",
  "modules": ["auth", "dashboard", ...],
  "architecture": "monolith|client-server|microservices|static-site|library",
  "sensitive_areas": "description of sensitive paths or empty string",
  "domain_rules": "key domain rules or empty string",
  "constraints": ["constraint1", "constraint2"],
  "page_count": null
}

Be concise. Infer from context where the spec is ambiguous. Return ONLY valid JSON, no explanation.`, specPath)

	// Write prompt to temp file
	tmpFile, err := os.CreateTemp("", "forge-spec-prompt-*.txt")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(prompt); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("failed to write prompt: %w", err)
	}
	tmpFile.Close()

	// Run claude -p --output-format json
	output, err := util.RunShell(".", 120*time.Second,
		fmt.Sprintf("cat %q | claude -p --output-format json", tmpFile.Name()))
	if err != nil {
		return nil, fmt.Errorf("claude analysis failed: %w", err)
	}

	// Parse JSON from output — handle both raw JSON and ```json code blocks
	jsonStr := extractJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in claude output")
	}

	var result specAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse analysis JSON: %w", err)
	}

	return &result, nil
}

// inferProjectContext uses claude to analyze the codebase and infer project metadata
// without requiring user input. Returns nil if inference fails.
func inferProjectContext(cwd string) *specAnalysis {
	prompt := `You are analyzing an existing codebase to extract project metadata for an agent harness.

Look at the directory structure, package files, README, and source code to infer:

Return ONLY a JSON object with these fields:
{
  "project_name": "short project name",
  "description": "one-sentence description of what this project does",
  "language": "typescript|javascript|python|go",
  "framework": "next|sveltekit|fastapi|django|flask|gin|chi|fiber|express|hono|null",
  "project_type": "web-app|api|cli|library|automation|fullstack",
  "modules": ["module1", "module2"],
  "architecture": "monolith|client-server|microservices|static-site|library",
  "sensitive_areas": "description of sensitive paths or empty string",
  "domain_rules": "key domain rules or empty string",
  "constraints": []
}

Be concise. Infer from the code — do not ask questions. Return ONLY valid JSON, no explanation.`

	output, err := util.RunShell(cwd, 60*time.Second,
		fmt.Sprintf("echo %q | claude -p --output-format json 2>/dev/null", prompt))
	if err != nil {
		return nil
	}

	jsonStr := extractJSON(output)
	if jsonStr == "" {
		return nil
	}

	var result specAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil
	}

	return &result
}

// --- bd init ---

// installSkillStubs writes tiny loader SKILL.md files to ~/.claude/skills/.
// Claude Code discovers these natively. Each stub just tells Claude to run
// the forge CLI to get the full skill content — the real logic lives in ~/.forge/.
func installSkillStubs() {
	stubs := map[string]string{
		"forge": `# forge

> Run the forge pipeline to deliver tracked work.

## Trigger

User runs /forge <description> or /forge --flag

## Instructions

Run this command and follow the instructions it outputs exactly:

` + "```\nBash(\"forge skill forge\")\n```" + `

That command outputs the full pipeline orchestrator. Read its output and execute every step.
Before starting, also run:

` + "```\nBash(\"forge paths\")\n```" + `

This gives you all resolved directories (runs, context, agents, pipeline). Use those paths throughout.
`,
		"ingest": `# ingest

> Parse a spec document into a structured project plan.

## Trigger

User runs /ingest <spec-id> or /ingest <file>

## Instructions

Run this command and follow the instructions it outputs exactly:

` + "```\nBash(\"forge skill ingest\")\n```" + `

That command outputs the full spec ingestion pipeline. Read its output and execute every step.
`,
	}

	skillsDir := filepath.Join(homeDir(), ".claude", "skills")
	for name, content := range stubs {
		stubPath := filepath.Join(skillsDir, name, "SKILL.md")
		// Don't overwrite if user has customized
		if util.Exists(stubPath) {
			continue
		}
		_ = util.EnsureDir(filepath.Dir(stubPath))
		_ = util.WriteText(stubPath, content)
	}
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return home
}

func initBd(cwd string) {
	// Best effort — bd is not critical
	if !util.WhichExists("bd") {
		// Try to install via brew
		_, err := util.RunShell(cwd, 120*time.Second, "brew install beads")
		if err != nil {
			return
		}
	}

	// --stealth: uses .git/info/exclude so .beads/ is never committed
	// --skip-agents: forge manages its own agent instructions
	// --skip-hooks: forge manages its own hooks via ~/.claude/settings.json
	_, _ = util.RunShell(cwd, 30*time.Second, "bd init --stealth --skip-agents --skip-hooks --server --quiet")
	_, _ = util.RunShell(cwd, 15*time.Second, "bd dolt start")

	// Clean up any .gitignore beads created (stealth should use .git/info/exclude only)
	beadsGitignore := filepath.Join(cwd, ".gitignore")
	if util.Exists(beadsGitignore) {
		content, _ := util.ReadText(beadsGitignore)
		if strings.Contains(content, "Beads") && !strings.Contains(content, "\n\n") {
			// The .gitignore was created by beads and contains only beads entries — remove it
			_ = os.Remove(beadsGitignore)
		}
	}
}

// --- results display ---

func displayInitResults(files []generatedFile, specId string) {
	lines := make([]string, len(files))
	for i, f := range files {
		lines[i] = fmt.Sprintf("  %s %s", ui.Green("✓"), f.relativePath)
	}

	ui.Note(strings.Join(lines, "\n"), fmt.Sprintf("Generated (%d files)", len(files)))

	ui.Log.Step("Next steps:")
	ui.Log.Message("")
	ui.Log.Message(fmt.Sprintf("  %s Open Claude Code and tell your agent what to build:", ui.Bold("1.")))
	if specId != "" {
		ui.Log.Message(fmt.Sprintf("     %s  — decompose the spec into tasks", ui.Cyan(fmt.Sprintf("/ingest %s", specId))))
		ui.Log.Message(fmt.Sprintf("     %s   — then start building", ui.Cyan(`/forge "first task from the plan"`)))
	} else {
		ui.Log.Message(fmt.Sprintf("     %s  — full pipeline", ui.Cyan(`/forge "add user authentication"`)))
		ui.Log.Message(fmt.Sprintf("     %s         — small fix", ui.Cyan(`/forge --quick "fix typo"`)))
	}
	ui.Log.Message("")
	ui.Log.Message(fmt.Sprintf("  %s", ui.Dim("Your agent handles everything from there — planning, coding, testing, and PR creation.")))
	ui.Log.Message(fmt.Sprintf("  %s", ui.Dim("Nothing was written to the project — your workflow lives in ~/.forge/")))
	ui.Log.Message(fmt.Sprintf("  %s", ui.Dim("Edit project context: forge paths | jq -r .context_dir")))
	ui.Log.Message("")
	ui.Log.Message(fmt.Sprintf("  %s", ui.Dim("Optional:")))
	ui.Log.Message(fmt.Sprintf("    %s   — HIPAA security checks", ui.Dim("forge add compliance-hipaa")))
	ui.Log.Message(fmt.Sprintf("    %s    — SOC2 compliance", ui.Dim("forge add compliance-soc2")))
	ui.Log.Message(fmt.Sprintf("    %s                 — Verify harness health", ui.Dim("forge doctor")))
}

// --- merge strategies ---

const forgeDelimiter = "<!-- forge:start -->"
const forgeDelimiterEnd = "<!-- forge:end -->"

// mergeCLAUDEmd preserves existing CLAUDE.md content and appends/replaces the
// forge-managed section. If the file doesn't exist, returns content as-is.
func mergeCLAUDEmd(existingPath, forgeContent string) string {
	existing, err := util.ReadText(existingPath)
	if err != nil {
		// No existing file — use forge content directly
		return forgeContent
	}

	existing = strings.TrimSpace(existing)
	if existing == "" {
		return forgeContent
	}

	wrappedForge := forgeDelimiter + "\n" + forgeContent + "\n" + forgeDelimiterEnd

	// If the file already has forge delimiters, replace that section
	startIdx := strings.Index(existing, forgeDelimiter)
	endIdx := strings.Index(existing, forgeDelimiterEnd)
	if startIdx != -1 && endIdx != -1 {
		before := strings.TrimRight(existing[:startIdx], "\n")
		after := strings.TrimLeft(existing[endIdx+len(forgeDelimiterEnd):], "\n")
		parts := []string{before, wrappedForge}
		if after != "" {
			parts = append(parts, after)
		}
		return strings.Join(parts, "\n\n") + "\n"
	}

	// No existing forge section — append
	return existing + "\n\n" + wrappedForge + "\n"
}

// mergeSettingsJSON merges forge permissions and hooks into an existing
// settings.json without removing user-defined entries. If the file doesn't
// exist, returns content as-is.
func mergeSettingsJSON(existingPath, forgeContent string) string {
	existingData, err := util.ReadText(existingPath)
	if err != nil {
		return forgeContent
	}

	var existing map[string]any
	if err := json.Unmarshal([]byte(existingData), &existing); err != nil {
		// Can't parse existing — back it up and overwrite
		_ = util.WriteText(existingPath+".backup", existingData)
		return forgeContent
	}

	var forge map[string]any
	if err := json.Unmarshal([]byte(forgeContent), &forge); err != nil {
		return forgeContent
	}

	// Merge permissions.allow — union of both lists
	mergePermissionsAllow(existing, forge)

	// Merge hooks — add forge hooks without removing existing ones
	mergeHooks(existing, forge)

	merged, err := json.MarshalIndent(existing, "", "\t")
	if err != nil {
		return forgeContent
	}
	return string(merged) + "\n"
}

func mergePermissionsAllow(existing, forge map[string]any) {
	forgePerms, ok := forge["permissions"].(map[string]any)
	if !ok {
		return
	}
	forgeAllow, ok := forgePerms["allow"].([]any)
	if !ok {
		return
	}

	existingPerms, ok := existing["permissions"].(map[string]any)
	if !ok {
		existing["permissions"] = forgePerms
		return
	}

	existingAllow, ok := existingPerms["allow"].([]any)
	if !ok {
		existingPerms["allow"] = forgeAllow
		return
	}

	// Build set from existing
	seen := map[string]bool{}
	for _, v := range existingAllow {
		if s, ok := v.(string); ok {
			seen[s] = true
		}
	}

	// Add forge entries that don't already exist
	for _, v := range forgeAllow {
		if s, ok := v.(string); ok {
			if !seen[s] {
				existingAllow = append(existingAllow, v)
				seen[s] = true
			}
		}
	}
	existingPerms["allow"] = existingAllow
}

func mergeHooks(existing, forge map[string]any) {
	forgeHooks, ok := forge["hooks"].(map[string]any)
	if !ok {
		return
	}

	existingHooks, ok := existing["hooks"].(map[string]any)
	if !ok {
		existing["hooks"] = forgeHooks
		return
	}

	// For each hook event (SessionStart, PreToolUse, PostToolUse), merge entries
	for event, forgeEntries := range forgeHooks {
		forgeArr, ok := forgeEntries.([]any)
		if !ok {
			continue
		}

		existingArr, ok := existingHooks[event].([]any)
		if !ok {
			existingHooks[event] = forgeArr
			continue
		}

		// Check if forge hooks are already present (by command string)
		existingCmds := map[string]bool{}
		for _, entry := range existingArr {
			if m, ok := entry.(map[string]any); ok {
				if hooks, ok := m["hooks"].([]any); ok {
					for _, h := range hooks {
						if hm, ok := h.(map[string]any); ok {
							if cmd, ok := hm["command"].(string); ok {
								existingCmds[cmd] = true
							}
						}
					}
				}
			}
		}

		for _, entry := range forgeArr {
			if m, ok := entry.(map[string]any); ok {
				isNew := true
				if hooks, ok := m["hooks"].([]any); ok {
					for _, h := range hooks {
						if hm, ok := h.(map[string]any); ok {
							if cmd, ok := hm["command"].(string); ok {
								if existingCmds[cmd] {
									isNew = false
									break
								}
							}
						}
					}
				}
				if isNew {
					existingArr = append(existingArr, entry)
				}
			}
		}
		existingHooks[event] = existingArr
	}
}

// --- helpers ---

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func mustCwd() string {
	cwd, _ := os.Getwd()
	return cwd
}
