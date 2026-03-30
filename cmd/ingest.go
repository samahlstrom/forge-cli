package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	ingestChunkSize string
	ingestResume    string
)

// SpecAnalysis holds the structured output from Claude's spec analysis.
type SpecAnalysis struct {
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

type resolvedFile struct {
	path        string
	ext         string
	name        string
	sizeDisplay string
	sizeBytes   int64
	pageCount   *int
}

func init() {
	ingestCmd := &cobra.Command{
		Use:   "ingest <files...>",
		Short: "Ingest one or more spec documents for project planning and decomposition",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runIngest,
	}
	ingestCmd.Flags().StringVar(&ingestChunkSize, "chunk-size", "20", "Pages per chunk for PDF processing")
	ingestCmd.Flags().StringVar(&ingestResume, "resume", "", "Resume analysis of an existing spec")
	rootCmd.AddCommand(ingestCmd)
}

func runIngest(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold("forge") + ui.Dim(" — Spec Ingestion"))

	// ── Resolve and validate all spec files ──
	supportedFormats := map[string]bool{".pdf": true, ".md": true, ".txt": true, ".markdown": true}
	var resolved []resolvedFile

	for _, file := range args {
		specPath, err := filepath.Abs(file)
		if err != nil {
			ui.Cancel(fmt.Sprintf("Invalid path: %s", file))
			os.Exit(1)
		}
		if !util.Exists(specPath) {
			ui.Cancel(fmt.Sprintf("File not found: %s", specPath))
			os.Exit(1)
		}

		ext := strings.ToLower(filepath.Ext(specPath))
		if !supportedFormats[ext] {
			supported := []string{".pdf", ".md", ".txt", ".markdown"}
			ui.Cancel(fmt.Sprintf("Unsupported format: %s (%s). Supported: %s",
				ext, filepath.Base(specPath), strings.Join(supported, ", ")))
			os.Exit(1)
		}

		info, err := os.Stat(specPath)
		if err != nil {
			ui.Cancel(fmt.Sprintf("Cannot stat file: %s", specPath))
			os.Exit(1)
		}
		sizeBytes := info.Size()
		var sizeDisplay string
		if sizeBytes < 102400 {
			sizeDisplay = fmt.Sprintf("%d KB", sizeBytes/1024)
		} else {
			sizeDisplay = fmt.Sprintf("%.1f MB", float64(sizeBytes)/(1024*1024))
		}

		var pageCount *int
		if ext == ".pdf" {
			pageCount = detectPageCount(specPath)
		}

		resolved = append(resolved, resolvedFile{
			path:        specPath,
			ext:         ext,
			name:        filepath.Base(specPath),
			sizeDisplay: sizeDisplay,
			sizeBytes:   sizeBytes,
			pageCount:   pageCount,
		})
	}

	isMulti := len(resolved) > 1

	// ── Display source info ──
	var infoLines []string
	if isMulti {
		infoLines = append(infoLines, fmt.Sprintf("Documents: %s", ui.Cyan(fmt.Sprint(len(resolved)))))
		for _, f := range resolved {
			format := extToFormat(f.ext)
			pageInfo := ""
			if f.pageCount != nil {
				pageInfo = fmt.Sprintf(" (%dp)", *f.pageCount)
			}
			infoLines = append(infoLines, fmt.Sprintf("  %s %s %s",
				ui.Dim("\u2022"), ui.Cyan(f.name), ui.Dim(fmt.Sprintf("%s, %s%s", format, f.sizeDisplay, pageInfo))))
		}
	} else {
		f := resolved[0]
		format := extToFormatLong(f.ext)
		infoLines = append(infoLines, fmt.Sprintf("File:     %s", ui.Cyan(f.name)))
		infoLines = append(infoLines, fmt.Sprintf("Format:   %s", ui.Cyan(format)))
		infoLines = append(infoLines, fmt.Sprintf("Size:     %s", ui.Cyan(f.sizeDisplay)))
		if f.pageCount != nil {
			infoLines = append(infoLines, fmt.Sprintf("Pages:    %s", ui.Cyan(fmt.Sprint(*f.pageCount))))
		}
	}

	chunkSize, _ := strconv.Atoi(ingestChunkSize)
	if chunkSize <= 0 {
		chunkSize = 20
	}
	totalPages := 0
	for _, f := range resolved {
		if f.pageCount != nil {
			totalPages += *f.pageCount
		}
	}
	if totalPages > chunkSize {
		chunks := (totalPages + chunkSize - 1) / chunkSize
		infoLines = append(infoLines, fmt.Sprintf("Chunks:   %s", ui.Cyan(fmt.Sprintf("%d x %d pages", chunks, chunkSize))))
	}

	noteTitle := "Source Document"
	if isMulti {
		noteTitle = "Source Documents"
	}
	ui.Note(strings.Join(infoLines, "\n"), noteTitle)

	// ── Generate spec ID and create directory ──
	specID := "spec-" + util.RandomHex(4)
	specDir := filepath.Join(cwd, ".forge", "specs", specID)
	util.EnsureDir(specDir)

	sourceFiles := make([]string, len(resolved))
	for i, f := range resolved {
		var destName string
		if isMulti {
			destName = fmt.Sprintf("source-%d%s", i+1, f.ext)
		} else {
			destName = "source" + f.ext
		}
		util.CopyFile(f.path, filepath.Join(specDir, destName))
		sourceFiles[i] = destName
	}

	// ── If multiple text/markdown files, create combined.md ──
	var combinedPath string
	if isMulti {
		var parts []string
		for _, f := range resolved {
			if f.ext == ".pdf" {
				parts = append(parts, fmt.Sprintf("\n\n---\n# [PDF Document: %s]\n# (Read separately via PDF reader)\n---\n\n", f.name))
			} else {
				content, err := util.ReadText(f.path)
				if err != nil {
					content = ""
				}
				parts = append(parts, fmt.Sprintf("\n\n---\n# Document: %s\n---\n\n%s", f.name, content))
			}
		}
		combinedPath = filepath.Join(specDir, "combined.md")
		_ = util.WriteText(combinedPath, strings.Join(parts, ""))
	}

	suffix := ""
	if isMulti {
		suffix = "s"
	}
	ui.Log.Success(fmt.Sprintf("Copied %d file%s to %s", len(resolved), suffix, ui.Dim(fmt.Sprintf(".forge/specs/%s/", specID))))
	if combinedPath != "" {
		ui.Log.Success(fmt.Sprintf("Combined document created at %s", ui.Dim(fmt.Sprintf(".forge/specs/%s/combined.md", specID))))
	}

	// ── Write spec metadata ──
	type metaSource struct {
		Original string   `json:"original"`
		Stored   string   `json:"stored"`
		Format   string   `json:"format"`
		SizeMB   float64  `json:"size_mb"`
		Pages    *int     `json:"pages"`
	}
	type metaDoc struct {
		SpecID string `json:"spec_id"`
		Source struct {
			Files      []metaSource `json:"files"`
			Combined   *string      `json:"combined"`
			IngestedAt string       `json:"ingested_at"`
		} `json:"source"`
		Status    string `json:"status"`
		ChunkSize int    `json:"chunk_size"`
	}

	meta := metaDoc{
		SpecID:    specID,
		Status:    "pending-analysis",
		ChunkSize: chunkSize,
	}
	meta.Source.IngestedAt = time.Now().UTC().Format(time.RFC3339)
	if combinedPath != "" {
		c := "combined.md"
		meta.Source.Combined = &c
	}
	for i, f := range resolved {
		meta.Source.Files = append(meta.Source.Files, metaSource{
			Original: f.name,
			Stored:   sourceFiles[i],
			Format:   strings.TrimPrefix(f.ext, "."),
			SizeMB:   float64(int(float64(f.sizeBytes)/(1024*1024)*100)) / 100,
			Pages:    f.pageCount,
		})
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	_ = util.WriteText(filepath.Join(specDir, "meta.json"), string(metaJSON))

	// ── Spec analysis (if no harness exists) ──
	harnessExists := util.Exists(filepath.Join(cwd, "forge.yaml"))
	var analysis *SpecAnalysis

	if !harnessExists {
		spinner := ui.NewSpinner()
		spinner.Start("Analyzing spec with Claude Code...")

		analysisTarget := filepath.Join(specDir, sourceFiles[0])
		analysisExt := resolved[0].ext
		analysisPagesCount := resolved[0].pageCount
		if combinedPath != "" {
			analysisTarget = combinedPath
			analysisExt = ".md"
			analysisPagesCount = nil
		}

		a, err := analyzeSpecWithClaude(analysisTarget, analysisExt, analysisPagesCount, "")
		if err != nil {
			spinner.Stop("Spec analysis failed")
			ui.Log.Warn("Could not analyze spec automatically. You can configure manually.")
			ui.Log.Warn(ui.Dim(err.Error()))
		} else {
			spinner.Stop("Spec analysis complete")
			analysis = a
		}

		if analysis != nil {
			extractedLines := []string{
				fmt.Sprintf("Project:      %s", analysis.ProjectName),
				fmt.Sprintf("Description:  %s", analysis.Description),
				fmt.Sprintf("Type:         %s", analysis.ProjectType),
				fmt.Sprintf("Language:     %s", analysis.Language),
			}
			if analysis.Framework != nil && *analysis.Framework != "" {
				extractedLines = append(extractedLines, fmt.Sprintf("Framework:    %s", *analysis.Framework))
			}
			if len(analysis.Modules) > 0 {
				extractedLines = append(extractedLines, fmt.Sprintf("Modules:      %s", strings.Join(analysis.Modules, ", ")))
			}
			extractedLines = append(extractedLines, fmt.Sprintf("Architecture: %s", analysis.Architecture))
			if analysis.SensitiveAreas != "" {
				extractedLines = append(extractedLines, fmt.Sprintf("Sensitive:    %s", analysis.SensitiveAreas))
			}
			if len(analysis.Constraints) > 0 {
				max := 3
				if len(analysis.Constraints) < max {
					max = len(analysis.Constraints)
				}
				extractedLines = append(extractedLines, fmt.Sprintf("Constraints:  %s", strings.Join(analysis.Constraints[:max], "; ")))
			}

			ui.Note(strings.Join(extractedLines, "\n"), "Extracted from spec")

			confirmed, cancelled := ui.Confirm("Does this look right?", true)
			if cancelled {
				ui.Cancel("Cancelled.")
				os.Exit(0)
			}

			if !confirmed {
				corrections, cancelled := ui.Text("What needs to change?", "e.g. Use SvelteKit instead of Next.js, add billing as a module")
				if cancelled {
					ui.Cancel("Cancelled.")
					os.Exit(0)
				}
				if corrections != "" {
					spinner2 := ui.NewSpinner()
					spinner2.Start("Re-analyzing with corrections...")
					a2, err := analyzeSpecWithClaude(analysisTarget, analysisExt, analysisPagesCount, corrections)
					if err != nil {
						spinner2.Stop("Re-analysis failed, using original analysis")
					} else {
						spinner2.Stop("Updated analysis complete")
						analysis = a2
					}
				}
			}

			analysisJSON, _ := json.MarshalIndent(analysis, "", "  ")
			_ = util.WriteText(filepath.Join(specDir, "analysis.json"), string(analysisJSON))
			ui.Log.Success("Spec analysis saved.")
		}
	}

	// ── Offer to run init ──
	if !harnessExists {
		runInit, cancelled := ui.Confirm("Scaffold the harness now?", true)
		if cancelled {
			ui.Cancel("Cancelled.")
			os.Exit(0)
		}

		if runInit {
			ui.Outro(ui.Green("Spec ingested — starting init..."))
			return RunInit(analysis, specID)
		}
	}

	// ── Next steps ──
	if harnessExists {
		ui.Log.Step("Next step:")
		ui.Log.Message(fmt.Sprintf("  Open Claude Code -> %s", ui.Cyan(fmt.Sprintf("/ingest %s", specID))))
	} else {
		ui.Log.Step("Next steps:")
		ui.Log.Message(fmt.Sprintf("  1. Run %s to scaffold the harness", ui.Cyan("forge init")))
		ui.Log.Message(fmt.Sprintf("     %s", ui.Dim("(use the spec analysis above to answer onboarding questions)")))
		ui.Log.Message(fmt.Sprintf("  2. Open Claude Code -> %s", ui.Cyan(fmt.Sprintf("/ingest %s", specID))))
	}

	ui.Outro(ui.Green("Spec ready for analysis."))
	return nil
}

// RunInit is called from ingest to launch the init flow with pre-filled spec analysis.
// This is a placeholder that should be wired to the real init command once cmd/init.go exists.
func RunInit(analysis *SpecAnalysis, specID string) error {
	// Build args for the init command invocation
	initArgs := []string{"--spec-id", specID}
	if analysis != nil {
		// Write analysis to a temp file so init can read it
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("forge-analysis-%s.json", util.RandomHex(4)))
		data, _ := json.MarshalIndent(analysis, "", "  ")
		_ = util.WriteText(tmpFile, string(data))
		defer os.Remove(tmpFile)
		initArgs = append(initArgs, "--spec-analysis", tmpFile)
	}

	// Find and execute the init subcommand on rootCmd
	initCmd, _, err := rootCmd.Find([]string{"init"})
	if err != nil || initCmd == rootCmd {
		// init command not yet registered; fall back to exec
		ui.Log.Warn("Init command not available. Run `forge init` manually.")
		return nil
	}
	initCmd.SetArgs(initArgs)
	return initCmd.Execute()
}

// ─── Spec analysis via Claude subprocess ──────────────────────────

func analyzeSpecWithClaude(specPath, ext string, pageCount *int, corrections string) (*SpecAnalysis, error) {
	prompt := buildAnalysisPrompt(specPath, ext, pageCount, corrections)

	// Write prompt to temp file to avoid shell escaping issues
	tmpPromptFile := filepath.Join(os.TempDir(), fmt.Sprintf("forge-prompt-%s.txt", util.RandomHex(4)))
	if err := util.WriteText(tmpPromptFile, prompt); err != nil {
		return nil, fmt.Errorf("failed to write prompt file: %w", err)
	}
	defer os.Remove(tmpPromptFile)

	// Pipe prompt to claude -p --output-format json
	catCmd := exec.Command("cat", tmpPromptFile)
	claudeCmd := exec.Command("claude", "-p", "--output-format", "json")

	pr, pw := io.Pipe()
	catCmd.Stdout = pw
	claudeCmd.Stdin = pr

	var outBuf strings.Builder
	claudeCmd.Stdout = &outBuf
	claudeCmd.Stderr = os.Stderr

	if err := catCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start cat: %w", err)
	}
	if err := claudeCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	go func() {
		_ = catCmd.Wait()
		pw.Close()
	}()

	if err := claudeCmd.Wait(); err != nil {
		return nil, fmt.Errorf("claude analysis failed: %w", err)
	}

	responseText := outBuf.String()

	// claude -p --output-format json wraps output as {"type":"result","result":"..."}
	var content string
	var wrapper struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(responseText), &wrapper); err == nil && wrapper.Result != "" {
		content = wrapper.Result
	} else {
		content = responseText
	}

	// Extract JSON from response
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		truncated := content
		if len(truncated) > 500 {
			truncated = truncated[:500]
		}
		return nil, fmt.Errorf("could not find JSON in Claude response. Raw output:\n%s", truncated)
	}

	var analysis SpecAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return nil, fmt.Errorf("failed to parse analysis JSON: %w", err)
	}
	analysis.PageCount = pageCount

	if analysis.ProjectName == "" || analysis.Language == "" {
		return nil, fmt.Errorf("incomplete analysis: missing project_name or language")
	}

	return &analysis, nil
}

func buildAnalysisPrompt(specPath, ext string, pageCount *int, corrections string) string {
	var readInstruction string
	if ext == ".pdf" {
		pagesToRead := 20
		if pageCount != nil {
			pagesToRead = *pageCount
		}
		if pagesToRead > 40 {
			pagesToRead = 40
		}
		readInstruction = fmt.Sprintf(`Read the PDF at "%s" (first %d pages) to understand what this project is.`, specPath, pagesToRead)
	} else {
		readInstruction = fmt.Sprintf(`Read the file at "%s" to understand what this project is.`, specPath)
	}

	correctionsClause := ""
	if corrections != "" {
		correctionsClause = fmt.Sprintf("\n\nThe user has provided these corrections to a previous analysis: \"%s\". Apply these corrections to your analysis.", corrections)
	}

	return fmt.Sprintf(`%s

Extract the following as a JSON object. Output ONLY the JSON, no explanation, no markdown formatting:

{
  "project_name": "short project name",
  "description": "1-2 sentence description of what this project does",
  "language": "typescript | javascript | python | go",
  "framework": "next | sveltekit | fastapi | django | gin | null",
  "project_type": "web-app | api | cli | library | automation | fullstack",
  "modules": ["list", "of", "main", "modules"],
  "architecture": "monolith | client-server | microservices | static-site | library",
  "sensitive_areas": "description of sensitive data or security concerns, or empty string",
  "domain_rules": "key business rules agents must follow, or empty string",
  "constraints": ["list", "of", "hard", "constraints"]
}

Infer language and framework from the spec's technology requirements. If not specified, choose the best fit based on the project type. For medical/healthcare projects, note HIPAA requirements in sensitive_areas.%s`, readInstruction, correctionsClause)
}

func extractJSON(content string) string {
	// Pattern 1: ```json ... ``` code block
	codeBlock := regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
	if m := codeBlock.FindStringSubmatch(content); len(m) > 1 {
		return m[1]
	}

	// Pattern 2: raw JSON object with project_name
	rawObj := regexp.MustCompile(`(?s)\{.*?"project_name".*?\}`)
	if m := rawObj.FindString(content); m != "" {
		return m
	}

	// Pattern 3: the entire content is JSON
	var tmp any
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &tmp); err == nil {
		return strings.TrimSpace(content)
	}

	return ""
}

// ─── Helpers ──────────────────────────────────────────────────────

func detectPageCount(pdfPath string) *int {
	// macOS: use mdls
	out, err := exec.Command("mdls", "-name", "kMDItemNumberOfPages", "-raw", pdfPath).Output()
	if err == nil {
		count, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err == nil && count > 0 {
			return &count
		}
	}

	// Fallback: pdfinfo
	out, err = exec.Command("bash", "-c", fmt.Sprintf(`pdfinfo "%s" 2>/dev/null | grep "^Pages:" | awk '{print $2}'`, pdfPath)).Output()
	if err == nil {
		count, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err == nil && count > 0 {
			return &count
		}
	}

	return nil
}

func extToFormat(ext string) string {
	switch ext {
	case ".pdf":
		return "PDF"
	case ".md", ".markdown":
		return "MD"
	default:
		return "TXT"
	}
}

func extToFormatLong(ext string) string {
	switch ext {
	case ".pdf":
		return "PDF"
	case ".md", ".markdown":
		return "Markdown"
	default:
		return "Text"
	}
}
