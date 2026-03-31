package cmd

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/samahlstrom/forge-cli/internal/refine"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

var (
	refineMaxIter    int
	refineAPIKey     string
	refineModel      string
	refineIdleTimeout int
	refineYes        bool
)

func init() {
	refineCmd := &cobra.Command{
		Use:   "refine <criteria.yaml>",
		Short: "Hill-climbing loop: measure, improve, keep or discard, repeat",
		Long: `Autoresearch-style iterative improvement. Define success criteria in a YAML file,
and forge refine will autonomously run an agent to improve your code against those metrics.

Each iteration: agent makes a change → measure → keep if improved, discard if not → repeat.`,
		Args: cobra.ExactArgs(1),
		RunE: runRefine,
	}
	refineCmd.Flags().IntVar(&refineMaxIter, "max-iterations", 0, "Override max iterations from criteria")
	refineCmd.Flags().StringVar(&refineAPIKey, "api-key", "", "Anthropic API key")
	refineCmd.Flags().StringVar(&refineModel, "model", "claude-sonnet-4-6", "Model for the agent")
	refineCmd.Flags().IntVar(&refineIdleTimeout, "idle-timeout", 0, "Override idle timeout (seconds)")
	refineCmd.Flags().BoolVar(&refineYes, "yes", false, "Skip confirmation prompt")
	rootCmd.AddCommand(refineCmd)
}

func runRefine(cmd *cobra.Command, args []string) error {
	criteriaPath := args[0]
	cwd, _ := os.Getwd()

	// Parse criteria
	criteria, err := refine.ParseCriteria(criteriaPath)
	if err != nil {
		ui.Cancel(fmt.Sprintf("Invalid criteria file: %v", err))
		return err
	}

	// Apply flag overrides
	if refineMaxIter > 0 {
		criteria.MaxIterations = refineMaxIter
	}
	if refineIdleTimeout > 0 {
		criteria.IdleTimeout = refineIdleTimeout
	}

	apiKey := refineAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	model := refineModel
	if envModel := os.Getenv("FORGE_MODEL"); envModel != "" && refineModel == "claude-sonnet-4-6" {
		model = envModel
	}

	// Check clean git state
	statusOut, err := util.GitExec(cwd, "status", "--porcelain")
	if err != nil {
		ui.Cancel("Not a git repo or git error.")
		return err
	}
	if strings.TrimSpace(statusOut) != "" {
		ui.Cancel("Working tree is dirty. Commit or stash your changes first.")
		ui.Log.Info(ui.Dim("forge refine uses git commit/reset to keep/discard changes. It needs a clean start."))
		return fmt.Errorf("dirty working tree")
	}

	// Metric names for TSV
	metricNames := make([]string, len(criteria.Metrics))
	for i, m := range criteria.Metrics {
		metricNames[i] = m.Name
	}

	primary := criteria.PrimaryMetric()

	ui.Intro(ui.Bold("forge refine"))
	ui.Log.Info(fmt.Sprintf("Primary metric: %s (%s)", ui.Cyan(primary.Name), primary.Direction))
	ui.Log.Info(fmt.Sprintf("Max iterations: %s", ui.Cyan(fmt.Sprintf("%d", criteria.MaxIterations))))
	ui.Log.Info(fmt.Sprintf("Measure command: %s", ui.Dim(criteria.Measure)))
	if primary.HasTarget {
		ui.Log.Info(fmt.Sprintf("Target: %s", ui.Green(fmt.Sprintf("%.4f", primary.Target))))
	}

	if !refineYes {
		cont, cancelled := ui.Confirm("Start refine loop?", true)
		if cancelled || !cont {
			ui.Cancel("Cancelled.")
			return nil
		}
	}

	// Setup session directory
	sessionID := time.Now().Format("20060102-150405")
	sessionDir := filepath.Join(cwd, ".forge", "refine", sessionID)
	util.EnsureDir(sessionDir)
	resultsPath := filepath.Join(sessionDir, "results.tsv")
	if err := refine.InitResultsFile(resultsPath, metricNames); err != nil {
		return fmt.Errorf("init results: %w", err)
	}

	// Copy criteria for reproducibility
	util.CopyFile(criteriaPath, filepath.Join(sessionDir, "criteria.yaml"))

	// Graceful shutdown
	shuttingDown := false
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		shuttingDown = true
		ui.Log.Warn(ui.Yellow("\n  Shutting down after current iteration..."))
	}()
	defer signal.Stop(sigCh)

	// ─── Baseline ───────────────────────────────────────────────
	ui.Log.Step(ui.Bold("Running baseline measurement..."))

	baseline := refine.RunMeasure(criteria, cwd, time.Duration(criteria.IdleTimeout)*time.Second)
	if baseline.Error != nil {
		ui.Cancel(fmt.Sprintf("Baseline measurement failed: %v", baseline.Error))
		return baseline.Error
	}

	baselineResult := refine.IterationResult{
		Iteration: 0,
		Timestamp: time.Now(),
		Status:    "baseline",
		Values:    baseline.Values,
		DurationS: baseline.Duration.Seconds(),
	}
	// Get current commit SHA for baseline
	if sha, err := util.GitExec(cwd, "rev-parse", "--short", "HEAD"); err == nil {
		baselineResult.CommitSHA = sha
	}
	refine.AppendResult(resultsPath, baselineResult, metricNames)

	ui.Log.Success(fmt.Sprintf("Baseline: %s = %s",
		primary.Name, ui.Cyan(fmt.Sprintf("%.4f", baseline.Values[primary.Name]))))
	for _, m := range criteria.Metrics {
		if m.Name != primary.Name {
			ui.Log.Info(fmt.Sprintf("  %s = %.4f", m.Name, baseline.Values[m.Name]))
		}
	}

	// Check if targets already met
	if criteria.StopWhen.AllTargetsMet && refine.AllTargetsMet(baseline.Values, criteria) {
		ui.Log.Success("All targets already met! Nothing to improve.")
		return nil
	}

	// ─── Hill-climbing loop ─────────────────────────────────────
	bestValues := make(map[string]float64)
	for k, v := range baseline.Values {
		bestValues[k] = v
	}

	var allResults []refine.IterationResult
	allResults = append(allResults, baselineResult)
	stagnation := 0
	improved := 0
	regressed := 0

	idleTimeout := time.Duration(criteria.IdleTimeout) * time.Second

	for iter := 1; iter <= criteria.MaxIterations && !shuttingDown; iter++ {
		ui.Log.Step(fmt.Sprintf("Iteration %s / %d  (best %s = %s, stagnation = %d/%d)",
			ui.Bold(fmt.Sprintf("%d", iter)), criteria.MaxIterations,
			primary.Name, ui.Cyan(fmt.Sprintf("%.4f", bestValues[primary.Name])),
			stagnation, criteria.StopWhen.NoImprovementFor))

		// Build prompt
		prompt := buildRefinePrompt(criteria, bestValues, allResults, metricNames, iter)

		// Write prompt to temp file
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("forge-refine-%d-%d.txt", iter, time.Now().UnixMilli()))
		if err := os.WriteFile(tmpFile, []byte(prompt), 0o644); err != nil {
			ui.Log.Error(fmt.Sprintf("Failed to write prompt: %v", err))
			break
		}

		// Run agent
		claudeArgs := []string{"claude", "-p", "--dangerously-skip-permissions", "--output-format", "json"}
		if model != "" {
			claudeArgs = append(claudeArgs, "--model", model)
		}

		iterStart := time.Now()
		pipeCmd := fmt.Sprintf("cat %q | %s", tmpFile, strings.Join(claudeArgs, " "))
		agentResult := runRefineAgent(pipeCmd, cwd, idleTimeout, apiKey)
		os.Remove(tmpFile)
		iterDuration := time.Since(iterStart)

		result := refine.IterationResult{
			Iteration: iter,
			Timestamp: time.Now(),
			DurationS: iterDuration.Seconds(),
			Values:    make(map[string]float64),
		}

		if agentResult.err != nil {
			// Agent failed — discard and continue
			result.Status = "agent_failed"
			result.AgentError = truncate(agentResult.err.Error(), 200)
			discardChanges(cwd)
			stagnation++
			ui.Log.Warn(ui.Yellow(fmt.Sprintf("  Agent failed: %s", result.AgentError)))
		} else {
			// Measure
			measurement := refine.RunMeasure(criteria, cwd, time.Duration(criteria.IdleTimeout)*time.Second)

			if measurement.Error != nil {
				result.Status = "measure_failed"
				result.AgentError = truncate(measurement.Error.Error(), 200)
				discardChanges(cwd)
				stagnation++
				ui.Log.Warn(ui.Yellow(fmt.Sprintf("  Measure failed: %s", result.AgentError)))
			} else {
				result.Values = measurement.Values
				newPrimary := measurement.Values[primary.Name]
				oldPrimary := bestValues[primary.Name]

				if refine.IsBetter(newPrimary, oldPrimary, primary.Direction) {
					// Keep — commit the improvement
					result.Status = "improved"
					stagnation = 0
					improved++

					addCmd := exec.Command("git", "add", "-A")
					addCmd.Dir = cwd
					_ = addCmd.Run()

					pctChange := ((newPrimary - oldPrimary) / math.Abs(oldPrimary)) * 100
					commitMsg := fmt.Sprintf("forge-refine: iter %d — %s %.4f → %.4f (%+.1f%%)",
						iter, primary.Name, oldPrimary, newPrimary, pctChange)
					commitCmd := exec.Command("git", "commit", "-m", commitMsg, "--allow-empty")
					commitCmd.Dir = cwd
					_ = commitCmd.Run()

					if sha, err := util.GitExec(cwd, "rev-parse", "--short", "HEAD"); err == nil {
						result.CommitSHA = sha
					}

					// Update best
					for k, v := range measurement.Values {
						bestValues[k] = v
					}

					ui.Log.Success(fmt.Sprintf("  %s %s: %.4f → %s (%+.1f%%)",
						ui.Green("KEEP"), primary.Name, oldPrimary,
						ui.Green(fmt.Sprintf("%.4f", newPrimary)), pctChange))
				} else {
					// Discard — revert changes
					result.Status = "regressed"
					stagnation++
					regressed++
					discardChanges(cwd)

					ui.Log.Info(fmt.Sprintf("  %s %s: %.4f → %.4f (no improvement)",
						ui.Dim("TOSS"), primary.Name, oldPrimary, newPrimary))
				}
			}
		}

		allResults = append(allResults, result)
		refine.AppendResult(resultsPath, result, metricNames)

		// Check stop conditions
		if criteria.StopWhen.AllTargetsMet && refine.AllTargetsMet(bestValues, criteria) {
			ui.Log.Success(ui.Bold("All targets met!"))
			break
		}
		if stagnation >= criteria.StopWhen.NoImprovementFor {
			ui.Log.Warn(ui.Yellow(fmt.Sprintf("No improvement for %d iterations — stopping.", stagnation)))
			break
		}
	}

	// ─── Summary ────────────────────────────────────────────────
	baselinePrimary := baseline.Values[primary.Name]
	bestPrimary := bestValues[primary.Name]
	totalChange := ((bestPrimary - baselinePrimary) / math.Abs(baselinePrimary)) * 100

	summaryLines := []string{
		fmt.Sprintf("Iterations:  %d total, %s improved, %s regressed",
			len(allResults)-1, ui.Green(fmt.Sprintf("%d", improved)), ui.Red(fmt.Sprintf("%d", regressed))),
		fmt.Sprintf("Primary:     %s", ui.Cyan(primary.Name)),
		fmt.Sprintf("Baseline:    %.4f", baselinePrimary),
		fmt.Sprintf("Best:        %s (%+.1f%%)", ui.Green(fmt.Sprintf("%.4f", bestPrimary)), totalChange),
		fmt.Sprintf("Results:     %s", ui.Dim(resultsPath)),
	}
	ui.Note(strings.Join(summaryLines, "\n"), "Refine complete")

	ui.Log.Info(fmt.Sprintf("Git log: %s", ui.Dim("git log --oneline --grep='forge-refine'")))
	ui.Outro(ui.Green("Done."))
	return nil
}

// buildRefinePrompt constructs the agent prompt for this iteration.
func buildRefinePrompt(criteria *refine.Criteria, bestValues map[string]float64, results []refine.IterationResult, metricNames []string, iteration int) string {
	primary := criteria.PrimaryMetric()

	// Current metrics
	var metricLines []string
	for _, m := range criteria.Metrics {
		marker := ""
		if m.Name == primary.Name {
			marker = " (PRIMARY)"
		}
		metricLines = append(metricLines, fmt.Sprintf("- %s = %.4f (%s)%s", m.Name, bestValues[m.Name], m.Direction, marker))
	}

	// Scope
	includeStr := "  - (all files)"
	if len(criteria.Scope.Include) > 0 {
		parts := make([]string, len(criteria.Scope.Include))
		for i, p := range criteria.Scope.Include {
			parts[i] = fmt.Sprintf("  - %s", p)
		}
		includeStr = strings.Join(parts, "\n")
	}
	excludeStr := "  - (none)"
	if len(criteria.Scope.Exclude) > 0 {
		parts := make([]string, len(criteria.Scope.Exclude))
		for i, p := range criteria.Scope.Exclude {
			parts[i] = fmt.Sprintf("  - %s", p)
		}
		excludeStr = strings.Join(parts, "\n")
	}

	// Target line
	targetLine := ""
	if primary.HasTarget {
		targetLine = fmt.Sprintf("\nTarget value: %.4f", primary.Target)
	}

	history := refine.FormatHistory(results, metricNames, 5)

	return fmt.Sprintf(`# Refine — Iteration %d of %d

You are an autonomous code improvement agent. Your job: make ONE focused change to improve the primary metric.

## Objective

%s

## Current Best Metrics

%s

## Target

Primary metric: **%s** (%s)%s

## Recent History

%s

## Scope

You may ONLY edit files in these paths:
%s

You must NOT edit:
%s
- The criteria file or measurement scripts (these are tamper-proof)

## Rules

1. Make ONE focused change per iteration. Small, testable, reversible.
2. If the last few iterations regressed, try a different approach — don't repeat what failed.
3. If you're stuck after several similar attempts, try something radically different.
4. Do NOT modify tests or benchmarks to game the metrics.
5. Commit nothing — the harness handles git.
6. Focus on the primary metric. Secondary metrics are tracked but don't drive keep/discard.
7. After making your change, stop. The harness will measure and decide.`,
		iteration, criteria.MaxIterations,
		criteria.Objective,
		strings.Join(metricLines, "\n"),
		primary.Name, primary.Direction, targetLine,
		history,
		includeStr,
		excludeStr,
	)
}

// runRefineAgent runs claude -p and returns the result.
type refineAgentResult struct {
	output string
	err    error
}

func runRefineAgent(command, cwd string, idleTimeout time.Duration, apiKey string) refineAgentResult {
	env := os.Environ()
	if apiKey != "" {
		env = append(env, fmt.Sprintf("ANTHROPIC_API_KEY=%s", apiKey))
	}

	result := runWithIdleTimeout(command, cwd, idleTimeout, env)

	if result.idleKilled {
		return refineAgentResult{output: result.output, err: fmt.Errorf("agent idle-killed after %s", idleTimeout)}
	}
	if result.exitCode != 0 {
		errMsg := result.output
		if errMsg == "" {
			errMsg = fmt.Sprintf("claude exited with code %d", result.exitCode)
		}
		// Check for rate limiting
		if isRateLimited(errMsg) {
			return refineAgentResult{output: errMsg, err: fmt.Errorf("rate limited")}
		}
		return refineAgentResult{output: errMsg, err: fmt.Errorf("%s", errMsg)}
	}

	return refineAgentResult{output: result.output}
}

// discardChanges reverts all uncommitted changes.
func discardChanges(cwd string) {
	checkoutCmd := exec.Command("git", "checkout", "--", ".")
	checkoutCmd.Dir = cwd
	_ = checkoutCmd.Run()

	cleanCmd := exec.Command("git", "clean", "-fd")
	cleanCmd.Dir = cwd
	_ = cleanCmd.Run()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
