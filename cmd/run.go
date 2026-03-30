package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/samahlstrom/forge-cli/internal/bd"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

// ─── Flags ───────────────────────────────────────────────────────

var (
	runDryRun     bool
	runPhase      string
	runConcurrency int
	runBudget     string
	runNoReview   bool
	runYes        bool
	runAPIKey     string
	runModel      string
	runIdleTimeout int
)

func init() {
	runCmd := &cobra.Command{
		Use:   "run <spec-id>",
		Short: "Auto-pilot: orchestrate task execution from a seeded spec",
		Args:  cobra.ExactArgs(1),
		RunE:  runRun,
	}
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "Show execution plan without running")
	runCmd.Flags().StringVar(&runPhase, "phase", "", "Only execute tasks in a specific phase")
	runCmd.Flags().IntVar(&runConcurrency, "concurrency", 1, "Max parallel tasks")
	runCmd.Flags().StringVar(&runBudget, "budget", "", "Max USD spend per task via claude -p")
	runCmd.Flags().BoolVar(&runNoReview, "no-review", false, "Skip review gates between phases")
	runCmd.Flags().BoolVar(&runYes, "yes", false, "Skip all confirmation prompts (for non-interactive use)")
	runCmd.Flags().StringVar(&runAPIKey, "api-key", "", "Anthropic API key (bypasses Claude Code subscription limits)")
	runCmd.Flags().StringVar(&runModel, "model", "claude-sonnet-4-6", "Model to use for worker agents")
	runCmd.Flags().IntVar(&runIdleTimeout, "idle-timeout", 300, "Kill task after N seconds of no output")
	rootCmd.AddCommand(runCmd)
}

// ─── Report Types ────────────────────────────────────────────────

// TaskReport is declared in runstatus.go (shared between run and run-status).

// PhaseReport captures the result of executing an entire phase.
type PhaseReport struct {
	Phase        string       `json:"phase"`
	Tasks        []TaskReport `json:"tasks"`
	StartedAt    string       `json:"started_at"`
	FinishedAt   string       `json:"finished_at"`
	SuccessCount int          `json:"success_count"`
	FailureCount int          `json:"failure_count"`
}

// ─── Circuit Breaker ─────────────────────────────────────────────

type circuitBreakerState struct {
	mu                    sync.Mutex
	consecutiveRateLimits int
	lastRateLimitAt       time.Time
	cooldownUntil         time.Time
	threshold             int
	cooldownDuration      time.Duration
}

func newCircuitBreaker() *circuitBreakerState {
	return &circuitBreakerState{
		threshold:        3,
		cooldownDuration: 2 * time.Minute,
	}
}

func (cb *circuitBreakerState) recordRateLimit() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveRateLimits++
	cb.lastRateLimitAt = time.Now()
	if cb.consecutiveRateLimits >= cb.threshold {
		cb.cooldownUntil = time.Now().Add(cb.cooldownDuration)
	}
}

func (cb *circuitBreakerState) reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveRateLimits = 0
}

func (cb *circuitBreakerState) isTripped() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return time.Now().Before(cb.cooldownUntil)
}

func (cb *circuitBreakerState) waitDuration() time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if time.Now().Before(cb.cooldownUntil) {
		return time.Until(cb.cooldownUntil)
	}
	return 0
}

// ─── Merge Lock ──────────────────────────────────────────────────

type mergeMutex struct {
	mu sync.Mutex
}

func (m *mergeMutex) Lock()   { m.mu.Lock() }
func (m *mergeMutex) Unlock() { m.mu.Unlock() }

// ─── Ring Buffer ─────────────────────────────────────────────────

const maxOutputBytes = 512 * 1024 // 512 KB

type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newRingBuffer(maxSize int) *ringBuffer {
	return &ringBuffer{size: maxSize}
}

func (rb *ringBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.buf = append(rb.buf, p...)
	if len(rb.buf) > rb.size*2 {
		rb.buf = rb.buf[len(rb.buf)-rb.size:]
	}
	return len(p), nil
}

func (rb *ringBuffer) String() string {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.buf) > rb.size {
		return string(rb.buf[len(rb.buf)-rb.size:])
	}
	return string(rb.buf)
}

// ─── Tier Estimates (dry-run only) ───────────────────────────────

var tierEstimates = map[string]int64{
	"T1": 5 * 60 * 1000,
	"T2": 15 * 60 * 1000,
	"T3": 30 * 60 * 1000,
}

// ─── Idle-Timeout Process Result ─────────────────────────────────

type idleRunResult struct {
	exitCode   int
	output     string
	idleKilled bool
}

// ─── Main Command ────────────────────────────────────────────────

func runRun(cmd *cobra.Command, args []string) error {
	specID := args[0]
	cwd, _ := os.Getwd()

	apiKey := runAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	model := runModel
	if envModel := os.Getenv("FORGE_MODEL"); envModel != "" && runModel == "claude-sonnet-4-6" {
		model = envModel
	}
	idleTimeoutDuration := time.Duration(runIdleTimeout) * time.Second
	reviewEnabled := !runNoReview && !runYes

	cb := newCircuitBreaker()
	mlock := &mergeMutex{}

	// Graceful shutdown
	shuttingDown := false
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		shuttingDown = true
		ui.Log.Warn(ui.Yellow(fmt.Sprintf("\n  Received %s — saving reports and cleaning up...", sig)))
	}()
	defer signal.Stop(sigCh)

	ui.Intro(ui.Bold("forge run") + ui.Dim(fmt.Sprintf(" — %s", specID)))

	// Verify spec exists
	specDir := filepath.Join(cwd, ".forge", "specs", specID)
	if !util.Exists(filepath.Join(specDir, "spec.yaml")) {
		ui.Cancel(fmt.Sprintf("No spec.yaml found. Run /ingest %s first.", specID))
		os.Exit(1)
	}

	// Verify beads exist
	allTasks, err := bd.List([]string{fmt.Sprintf("spec:%s", specID)}, "task", "", cwd)
	if err != nil {
		ui.Cancel("Could not query beads. Is bd initialized? Run: forge seed " + specID)
		os.Exit(1)
	}
	if len(allTasks) == 0 {
		ui.Cancel(fmt.Sprintf("No task beads found for %s. Run: forge seed %s", specID, specID))
		os.Exit(1)
	}

	openCount := 0
	for _, t := range allTasks {
		if t.Status == "open" || t.Status == "in_progress" {
			openCount++
		}
	}
	ui.Log.Info(fmt.Sprintf("%s tasks remaining out of %d total", ui.Cyan(strconv.Itoa(openCount)), len(allTasks)))

	// Dry run
	if runDryRun {
		showDryRun(specID, cwd)
		ui.Outro(ui.Dim("Dry run complete — no tasks executed."))
		return nil
	}

	// Warn about permissions and API key
	ui.Log.Warn(ui.Yellow("Auto-pilot uses --dangerously-skip-permissions and git worktrees for isolation."))
	if apiKey != "" {
		ui.Log.Info(fmt.Sprintf("Using API key %s", ui.Green("(bypasses subscription limits)")))
	} else {
		ui.Log.Warn(ui.Yellow("No ANTHROPIC_API_KEY set — workers use your Claude Code subscription limit."))
		ui.Log.Info(ui.Dim("Set ANTHROPIC_API_KEY or use --api-key to avoid daily caps."))
	}
	if model != "" {
		ui.Log.Info(fmt.Sprintf("Model: %s", ui.Cyan(model)))
	}
	if runConcurrency > 1 {
		ui.Log.Info(fmt.Sprintf("Concurrency: %s parallel worktrees", ui.Cyan(strconv.Itoa(runConcurrency))))
	}
	if !runYes {
		cont, cancelled := ui.Confirm("Continue?", true)
		if cancelled || !cont {
			ui.Cancel("Run cancelled.")
			os.Exit(0)
		}
	}

	// Set up reports directory
	reportsDir := filepath.Join(cwd, ".forge", "specs", specID, "reports")
	util.EnsureDir(reportsDir)

	// Main orchestration loop
	completed := 0
	failed := 0
	currentPhase := ""
	startTime := time.Now()
	var allReports []TaskReport
	var phaseReports []PhaseReport
	var currentPhaseReport *PhaseReport

	for !shuttingDown {
		// Get ready tasks
		labelFilters := []string{fmt.Sprintf("spec:%s", specID)}
		if runPhase != "" {
			labelFilters = append(labelFilters, fmt.Sprintf("phase:%s", runPhase))
		}

		readyTasks, err := bd.Ready(labelFilters, cwd, "task")
		if err != nil {
			readyTasks = nil
		}

		if len(readyTasks) == 0 {
			// Check if there are still open tasks (blocked)
			remaining, _ := bd.List([]string{fmt.Sprintf("spec:%s", specID)}, "task", "open", cwd)
			if len(remaining) == 0 {
				break // All done
			}

			// Try closing eligible epics to unblock next phase
			epicCmd := exec.Command("bd", "epic", "close-eligible")
			epicCmd.Dir = cwd
			_ = epicCmd.Run()

			// Re-check ready tasks
			readyTasks, err = bd.Ready(labelFilters, cwd, "task")
			if err != nil {
				readyTasks = nil
			}

			if len(readyTasks) == 0 {
				// Finalize current phase report
				if currentPhaseReport != nil {
					currentPhaseReport.FinishedAt = time.Now().UTC().Format(time.RFC3339)
					phaseReports = append(phaseReports, *currentPhaseReport)
					writePhaseReport(reportsDir, currentPhaseReport)
				}

				if runYes {
					ui.Log.Info(ui.Dim(fmt.Sprintf("Phase transition — %d tasks unblocking...", len(remaining))))
				} else if reviewEnabled {
					elapsed := formatElapsed(time.Since(startTime).Milliseconds())
					ui.Log.Info(fmt.Sprintf("\n%s — %d completed, %d failed, %d remaining (%s)",
						ui.Bold("Phase checkpoint"), completed, failed, len(remaining), elapsed))

					if currentPhaseReport != nil && len(currentPhaseReport.Tasks) > 0 {
						showPhaseRetro(currentPhaseReport)
					}

					cont, cancelled := ui.Confirm(fmt.Sprintf("%d tasks blocked. Continue to next phase?", len(remaining)), true)
					if cancelled || !cont {
						break
					}
				} else {
					break
				}

				currentPhaseReport = nil
			}
		}

		if len(readyTasks) == 0 {
			continue
		}

		// Sort ready tasks by phase number — process lowest phase first
		sort.Slice(readyTasks, func(i, j int) bool {
			return extractPhaseNum(readyTasks[i]) < extractPhaseNum(readyTasks[j])
		})

		// Only process tasks from the lowest available phase
		lowestPhase := extractPhaseLabel(readyTasks[0])
		filtered := make([]bd.Issue, 0, len(readyTasks))
		for _, t := range readyTasks {
			if extractPhaseLabel(t) == lowestPhase {
				filtered = append(filtered, t)
			}
		}
		readyTasks = filtered

		// Detect phase transition
		taskPhase := lowestPhase
		if taskPhase != currentPhase {
			if currentPhase != "" && currentPhaseReport != nil {
				currentPhaseReport.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				phaseReports = append(phaseReports, *currentPhaseReport)
				writePhaseReport(reportsDir, currentPhaseReport)

				if reviewEnabled {
					ui.Log.Success(fmt.Sprintf("\n%s %s", ui.Bold("Phase complete:"), currentPhase))
					showPhaseRetro(currentPhaseReport)

					cont, cancelled := ui.Confirm(fmt.Sprintf("Start %s?", taskPhase), true)
					if cancelled || !cont {
						break
					}
				}
			}
			currentPhase = taskPhase
			currentPhaseReport = &PhaseReport{
				Phase:     currentPhase,
				StartedAt: time.Now().UTC().Format(time.RFC3339),
			}
			ui.Log.Step(ui.Bold(fmt.Sprintf("\nStarting %s", currentPhase)))
		}

		// Pick tasks up to concurrency limit
		batch := readyTasks
		if len(batch) > runConcurrency {
			batch = batch[:runConcurrency]
		}

		// Circuit breaker: pause if tripped before launching batch
		if cb.isTripped() {
			waitDur := cb.waitDuration()
			ui.Log.Warn(ui.Yellow(fmt.Sprintf("  Circuit breaker tripped — cooling down %s...", formatElapsed(waitDur.Milliseconds()))))
			time.Sleep(waitDur)
		}

		// Execute batch in parallel worktrees
		type taskResult struct {
			report TaskReport
			err    error
			task   bd.Issue
		}
		results := make([]taskResult, len(batch))
		var wg sync.WaitGroup

		for i, task := range batch {
			wg.Add(1)
			go func(idx int, t bd.Issue) {
				defer wg.Done()
				report, execErr := executeTaskInWorktree(t, specID, cwd, runBudget, apiKey, model, idleTimeoutDuration, cb, mlock)
				results[idx] = taskResult{report: report, err: execErr, task: t}
			}(i, task)
		}
		wg.Wait()

		for _, res := range results {
			task := res.task
			if res.err != nil {
				failed++
				if currentPhaseReport != nil {
					currentPhaseReport.FailureCount++
				}
				report := TaskReport{
					TaskID:          getSpecTaskID(task),
					BeadID:          task.ID,
					Title:           task.Title,
					Phase:           currentPhase,
					Agent:           extractLabel(task, "agent:"),
					Tier:            extractLabel(task, "tier:"),
					Status:          "failure",
					StartedAt:       time.Now().UTC().Format(time.RFC3339),
					FinishedAt:      time.Now().UTC().Format(time.RFC3339),
					Blockers:        []string{fmt.Sprintf("Unhandled error: %v", res.err)},
					Errors:          []string{fmt.Sprintf("%v", res.err)},
					Summary:         fmt.Sprintf("Task failed with unhandled error: %v", res.err),
					FailureCategory: categorizeFailure(fmt.Sprintf("%v", res.err)),
				}
				allReports = append(allReports, report)
				if currentPhaseReport != nil {
					currentPhaseReport.Tasks = append(currentPhaseReport.Tasks, report)
				}
				ui.Log.Error(fmt.Sprintf("%s %s: %v", ui.Red("x"), task.Title, res.err))
				_ = bd.Update(task.ID, "open", cwd)
				continue
			}

			report := res.report
			allReports = append(allReports, report)
			if currentPhaseReport != nil {
				currentPhaseReport.Tasks = append(currentPhaseReport.Tasks, report)
			}

			switch report.Status {
			case "success":
				completed++
				if currentPhaseReport != nil {
					currentPhaseReport.SuccessCount++
				}
				salvageNote := ""
				if report.FailureCategory == "idle_killed_with_deliverables" {
					salvageNote = ui.Cyan(" (salvaged from idle-kill)")
				}
				ui.Log.Success(fmt.Sprintf("%s %s %s%s", ui.Green("done"), task.Title,
					ui.Dim(fmt.Sprintf("(%s)", formatElapsed(report.ElapsedMs))), salvageNote))
				if err := bd.Close(task.ID, cwd); err != nil {
					ui.Log.Warn(fmt.Sprintf("Failed to close %s: %v", task.ID, err))
				}
			case "merge_conflict":
				failed++
				if currentPhaseReport != nil {
					currentPhaseReport.FailureCount++
				}
				ui.Log.Warn(fmt.Sprintf("%s %s: merge conflict — branch %s preserved",
					ui.Yellow("conflict"), task.Title, ui.Cyan(report.WorktreeBranch)))
				_ = bd.Update(task.ID, "open", cwd)
			case "timed_out":
				failed++
				if currentPhaseReport != nil {
					currentPhaseReport.FailureCount++
				}
				ui.Log.Warn(fmt.Sprintf("%s %s: timed out after %s (no deliverables found)",
					ui.Yellow("timeout"), task.Title, formatElapsed(report.ElapsedMs)))
				_ = bd.Update(task.ID, "open", cwd)
			default: // failure
				failed++
				if currentPhaseReport != nil {
					currentPhaseReport.FailureCount++
				}
				errSummary := ""
				if len(report.Errors) > 0 {
					errSummary = report.Errors[0]
				}
				ui.Log.Error(fmt.Sprintf("%s %s: %s", ui.Red("x"), task.Title, errSummary))
				_ = bd.Update(task.ID, "open", cwd)
			}
		}

		// Write incremental reports
		writeAllReports(filepath.Join(reportsDir, "all-tasks.json"), allReports)
	}

	// Save incremental reports before finalizing (covers interrupted runs)
	if len(allReports) > 0 {
		writeAllReports(filepath.Join(reportsDir, "all-tasks.json"), allReports)
	}

	// Finalize last phase
	if currentPhaseReport != nil && currentPhaseReport.FinishedAt == "" {
		currentPhaseReport.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		phaseReports = append(phaseReports, *currentPhaseReport)
		writePhaseReport(reportsDir, currentPhaseReport)
	}

	// Generate muda analysis
	elapsed := formatElapsed(time.Since(startTime).Milliseconds())
	generateMudaAnalysis(reportsDir, allReports, phaseReports, elapsed)

	summaryLines := []string{
		fmt.Sprintf("Completed:  %s", ui.Green(strconv.Itoa(completed))),
		fmt.Sprintf("Failed:     %s", failStr(failed)),
		fmt.Sprintf("Elapsed:    %s", ui.Cyan(elapsed)),
		fmt.Sprintf("Reports:    %s", ui.Dim(fmt.Sprintf(".forge/specs/%s/reports/", specID))),
	}
	ui.Note(strings.Join(summaryLines, "\n"), "Run complete")

	if failed > 0 {
		ui.Log.Info(fmt.Sprintf("Review blockers: %s", ui.Cyan(fmt.Sprintf(".forge/specs/%s/reports/muda-analysis.md", specID))))
	}

	ui.Outro(ui.Green("Done."))
	return nil
}

// ─── Worktree Execution ──────────────────────────────────────────

func executeTaskInWorktree(
	task bd.Issue,
	specID, cwd, budget, apiKey, model string,
	idleTimeout time.Duration,
	cb *circuitBreakerState,
	mlock *mergeMutex,
) (TaskReport, error) {
	startedAt := time.Now()
	tier := extractLabel(task, "tier:")
	if tier == "" {
		tier = "T2"
	}
	agent := extractLabel(task, "agent:")
	phase := extractLabel(task, "phase:")
	specTaskID := getSpecTaskID(task)

	var filesLikely []string
	if fl, ok := task.Metadata["files_likely"]; ok {
		if arr, ok := fl.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					filesLikely = append(filesLikely, s)
				}
			}
		}
	}

	branchName := fmt.Sprintf("forge/%s/%s", specID, task.ID)
	worktreePath := filepath.Join(cwd, ".forge", "worktrees", task.ID)

	report := TaskReport{
		TaskID:         specTaskID,
		BeadID:         task.ID,
		Title:          task.Title,
		Phase:          fmt.Sprintf("phase:%s", phase),
		Agent:          agent,
		Tier:           tier,
		Status:         "failure",
		StartedAt:      startedAt.UTC().Format(time.RFC3339),
		WorktreeBranch: branchName,
		FilesChanged:   []string{},
		Blockers:       []string{},
		Errors:         []string{},
	}

	defer func() {
		finishedAt := time.Now()
		report.FinishedAt = finishedAt.UTC().Format(time.RFC3339)
		report.ElapsedMs = finishedAt.Sub(startedAt).Milliseconds()
	}()

	// Clean up any stale worktree/branch from a previous failed run
	cleanupStaleWorktree(cwd, worktreePath, branchName)

	// Create worktree
	if _, err := gitExec(cwd, 30*time.Second, "worktree", "add", "-b", branchName, worktreePath, "HEAD"); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("worktree create: %v", err))
		report.Summary = fmt.Sprintf("Worktree setup failed: %v", err)
		report.FailureCategory = "worktree_error"
		return report, nil
	}

	// Ensure worktree cleanup on exit (keep branch if merge failed)
	defer func() {
		rmCmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
		rmCmd.Dir = cwd
		_ = rmCmd.Run()

		if report.Status == "success" {
			delCmd := exec.Command("git", "branch", "-D", branchName)
			delCmd.Dir = cwd
			_ = delCmd.Run()
		}
	}()

	// Validate worktree completeness before launching agent
	validateWorktree(worktreePath, task.Title)

	// Build task prompt
	filesStr := ""
	if len(filesLikely) > 0 {
		filesStr = fmt.Sprintf(" Target files: %s.", strings.Join(filesLikely, ", "))
	}
	reportPath := filepath.Join(worktreePath, ".forge", fmt.Sprintf("task-report-%s.json", task.ID))
	prompt := buildTaskPrompt(task, specID, filesStr, tier, agent, specTaskID, reportPath)

	// Write prompt to temp file
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("forge-run-%s-%d.txt", task.ID, time.Now().UnixMilli()))
	if err := os.WriteFile(tmpFile, []byte(prompt), 0o644); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("write prompt: %v", err))
		report.Summary = "Failed to write prompt temp file"
		return report, nil
	}
	defer os.Remove(tmpFile)

	// Build claude args
	claudeArgs := []string{"claude", "-p", "--dangerously-skip-permissions", "--output-format", "json"}
	if budget != "" {
		claudeArgs = append(claudeArgs, "--max-budget-usd", budget)
	}
	if model != "" {
		claudeArgs = append(claudeArgs, "--model", model)
	}

	// Retry loop with rate-limit backoff
	const maxRetries = 5
	var lastError error
	timedOut := false

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Circuit breaker check
		if cb.isTripped() {
			waitDur := cb.waitDuration()
			ui.Log.Warn(ui.Yellow(fmt.Sprintf("  Circuit breaker active — waiting %s...", formatElapsed(waitDur.Milliseconds()))))
			time.Sleep(waitDur)
		}

		env := os.Environ()
		if apiKey != "" {
			env = append(env, fmt.Sprintf("ANTHROPIC_API_KEY=%s", apiKey))
		}

		pipeCmd := fmt.Sprintf("cat %q | %s", tmpFile, strings.Join(claudeArgs, " "))
		result := runWithIdleTimeout(pipeCmd, worktreePath, idleTimeout, env)

		if result.exitCode != 0 && isRateLimited(result.output) {
			report.Retries++
			cb.recordRateLimit()
			waitMs := getRateLimitWait(result.output, attempt)
			ui.Log.Warn(ui.Yellow(fmt.Sprintf("  Rate limited on %q — waiting %s before retry %d/%d",
				task.Title, formatElapsed(waitMs), attempt, maxRetries)))
			time.Sleep(time.Duration(waitMs) * time.Millisecond)
			continue
		}

		// Non-rate-limit result: reset circuit breaker
		cb.reset()

		if result.idleKilled {
			timedOut = true
			lastError = fmt.Errorf("task idle-killed after %s of no output", formatElapsed(idleTimeout.Milliseconds()))
			break
		}

		if result.exitCode != 0 {
			output := result.output
			if output == "" {
				output = fmt.Sprintf("claude exited with code %d", result.exitCode)
			}
			lastError = fmt.Errorf("%s", output)
			break
		}

		lastError = nil
		break // Success
	}

	// Check for deliverables (even on timeout/error)
	hasDeliverables := checkDeliverables(worktreePath)

	if timedOut && hasDeliverables {
		ui.Log.Info(ui.Cyan(fmt.Sprintf("  %q was idle-killed but has deliverables — salvaging work", task.Title)))
		lastError = nil
		report.FailureCategory = "idle_killed_with_deliverables"
	} else if timedOut {
		report.Status = "timed_out"
		report.FailureCategory = "idle_killed"
	}

	if lastError != nil {
		report.Errors = append(report.Errors, lastError.Error())
		report.Summary = fmt.Sprintf("Claude execution failed: %v", lastError)
		if report.FailureCategory == "" {
			report.FailureCategory = categorizeFailure(lastError.Error())
		}
		return report, nil
	}

	// Read agent's self-report if it wrote one
	if agentData, err := os.ReadFile(reportPath); err == nil {
		var agentReport struct {
			Blockers []string `json:"blockers"`
			Errors   []string `json:"errors"`
			Summary  string   `json:"summary"`
		}
		if json.Unmarshal(agentData, &agentReport) == nil {
			report.Blockers = agentReport.Blockers
			report.Errors = agentReport.Errors
			report.Summary = agentReport.Summary
		}
	} else {
		report.Summary = "Task completed (no agent report generated)"
	}

	// Get list of files changed
	if diffOut, err := gitExec(worktreePath, 10*time.Second, "diff", "--name-only", "HEAD"); err == nil && diffOut != "" {
		report.FilesChanged = strings.Split(strings.TrimSpace(diffOut), "\n")
	}

	// Commit changes in worktree (use temp file for commit message to avoid injection)
	commitMsg := fmt.Sprintf("forge: %s [%s]", task.Title, specTaskID)
	commitMsgFile := filepath.Join(os.TempDir(), fmt.Sprintf("forge-commit-%s-%d.txt", task.ID, time.Now().UnixMilli()))
	if os.WriteFile(commitMsgFile, []byte(commitMsg), 0o644) == nil {
		defer os.Remove(commitMsgFile)
		addCmd := exec.Command("git", "add", "-A")
		addCmd.Dir = worktreePath
		_ = addCmd.Run()

		commitCmd := exec.Command("git", "commit", "-F", commitMsgFile, "--allow-empty")
		commitCmd.Dir = worktreePath
		_ = commitCmd.Run()
	}

	// Merge worktree branch back to main — serialized via lock
	mlock.Lock()
	defer mlock.Unlock()

	mainBranch := util.GetMainBranch(cwd)

	checkoutCmd := exec.Command("git", "checkout", mainBranch)
	checkoutCmd.Dir = cwd
	if err := checkoutCmd.Run(); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("checkout main: %v", err))
		report.Status = "failure"
		return report, nil
	}

	mergeCmd := exec.Command("git", "merge", branchName, "--no-edit")
	mergeCmd.Dir = cwd
	if err := mergeCmd.Run(); err != nil {
		// Merge conflict — try auto-resolving harness-owned files
		if tryAutoResolveMerge(cwd) {
			report.Status = "success"
		} else {
			abortCmd := exec.Command("git", "merge", "--abort")
			abortCmd.Dir = cwd
			_ = abortCmd.Run()
			report.Status = "merge_conflict"
			report.FailureCategory = "merge_conflict"
			report.Blockers = append(report.Blockers, fmt.Sprintf("Merge conflict on branch %s", branchName))
			report.Errors = append(report.Errors, fmt.Sprintf("%v", err))
		}
	} else {
		report.Status = "success"
	}

	return report, nil
}

// ─── Idle-Timeout Process Runner ─────────────────────────────────

func runWithIdleTimeout(command, cwd string, idleTimeout time.Duration, env []string) idleRunResult {
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = cwd
	cmd.Env = env

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return idleRunResult{exitCode: 1, output: fmt.Sprintf("stdout pipe: %v", err)}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return idleRunResult{exitCode: 1, output: fmt.Sprintf("stderr pipe: %v", err)}
	}

	if err := cmd.Start(); err != nil {
		return idleRunResult{exitCode: 1, output: fmt.Sprintf("start: %v", err)}
	}

	stdoutBuf := newRingBuffer(maxOutputBytes)
	stderrBuf := newRingBuffer(maxOutputBytes)

	// Channel to signal output activity
	activity := make(chan struct{}, 1)

	// Read stdout in goroutine
	var readerWg sync.WaitGroup
	readerWg.Add(2)

	go func() {
		defer readerWg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 {
				stdoutBuf.Write(buf[:n])
				select {
				case activity <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Read stderr in goroutine
	go func() {
		defer readerWg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				stderrBuf.Write(buf[:n])
				select {
				case activity <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Idle timeout watcher
	idleKilled := false
	done := make(chan struct{})

	go func() {
		timer := time.NewTimer(idleTimeout)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idleTimeout)
			case <-timer.C:
				idleKilled = true
				// SIGTERM first
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGTERM)
				}
				// Force SIGKILL after 5s
				time.AfterFunc(5*time.Second, func() {
					if cmd.Process != nil {
						_ = cmd.Process.Signal(syscall.SIGKILL)
					}
				})
				return
			}
		}
	}()

	// Wait for readers to finish, then wait for process
	readerWg.Wait()
	waitErr := cmd.Wait()
	close(done)

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	output := stdoutBuf.String()
	if output == "" {
		output = stderrBuf.String()
	}

	return idleRunResult{
		exitCode:   exitCode,
		output:     output,
		idleKilled: idleKilled,
	}
}

// ─── Worktree Validation ─────────────────────────────────────────

func validateWorktree(worktreePath, taskTitle string) {
	hasPackageJSON := util.Exists(filepath.Join(worktreePath, "package.json"))
	hasGoMod := util.Exists(filepath.Join(worktreePath, "go.mod"))
	hasPyproject := util.Exists(filepath.Join(worktreePath, "pyproject.toml"))
	hasRequirements := util.Exists(filepath.Join(worktreePath, "requirements.txt"))

	// Node/TS project: ensure node_modules exist
	if hasPackageJSON {
		if !util.Exists(filepath.Join(worktreePath, "node_modules", ".package-lock.json")) {
			ui.Log.Info(ui.Dim(fmt.Sprintf("  Installing dependencies in worktree for %q...", taskTitle)))
			npmCmd := exec.Command("bash", "-c",
				"npm install --prefer-offline --no-audit --no-fund 2>/dev/null || yarn install --frozen-lockfile 2>/dev/null || true")
			npmCmd.Dir = worktreePath
			_ = npmCmd.Run()
		}
	}

	// Go project: ensure modules are downloaded
	if hasGoMod {
		goCmd := exec.Command("go", "mod", "download")
		goCmd.Dir = worktreePath
		_ = goCmd.Run()
	}

	// Python project: check for venv
	if hasPyproject || hasRequirements {
		hasVenv := util.Exists(filepath.Join(worktreePath, ".venv", "bin", "python")) ||
			util.Exists(filepath.Join(worktreePath, "venv", "bin", "python"))
		if !hasVenv {
			ui.Log.Warn(ui.Yellow(fmt.Sprintf("  Worktree for %q: no Python venv detected — agent may need to create one", taskTitle)))
		}
	}

	// Warn if no recognizable project manifest
	if !hasPackageJSON && !hasGoMod && !hasPyproject && !hasRequirements {
		ui.Log.Warn(ui.Yellow(fmt.Sprintf("  Worktree for %q: no recognized project manifest found", taskTitle)))
	}
}

// ─── Deliverable Check ───────────────────────────────────────────

func checkDeliverables(worktreePath string) bool {
	var allChanges []string

	if out, err := gitExec(worktreePath, 10*time.Second, "diff", "--name-only", "HEAD"); err == nil && out != "" {
		allChanges = append(allChanges, strings.Split(strings.TrimSpace(out), "\n")...)
	}
	if out, err := gitExec(worktreePath, 10*time.Second, "diff", "--cached", "--name-only"); err == nil && out != "" {
		allChanges = append(allChanges, strings.Split(strings.TrimSpace(out), "\n")...)
	}
	if out, err := gitExec(worktreePath, 10*time.Second, "ls-files", "--others", "--exclude-standard"); err == nil && out != "" {
		allChanges = append(allChanges, strings.Split(strings.TrimSpace(out), "\n")...)
	}

	// Filter out report files — we want real deliverables
	for _, f := range allChanges {
		if f != "" && !strings.Contains(f, "task-report") {
			return true
		}
	}
	return false
}

// ─── Merge Conflict Auto-Resolution ──────────────────────────────

func tryAutoResolveMerge(cwd string) bool {
	out, err := gitExec(cwd, 10*time.Second, "diff", "--name-only", "--diff-filter=U")
	if err != nil || out == "" {
		return false
	}
	conflicted := strings.Split(strings.TrimSpace(out), "\n")
	if len(conflicted) == 0 {
		return false
	}

	// Harness-owned patterns that can be safely auto-resolved (accept theirs)
	harnessOwned := []string{".forge/task-report", ".forge/specs/", "task-report.json"}
	for _, f := range conflicted {
		isOwned := false
		for _, pattern := range harnessOwned {
			if strings.Contains(f, pattern) {
				isOwned = true
				break
			}
		}
		if !isOwned {
			return false
		}
	}

	// Accept incoming (theirs) for all harness files
	for _, f := range conflicted {
		theirsCmd := exec.Command("git", "checkout", "--theirs", f)
		theirsCmd.Dir = cwd
		if err := theirsCmd.Run(); err != nil {
			return false
		}
		addCmd := exec.Command("git", "add", f)
		addCmd.Dir = cwd
		if err := addCmd.Run(); err != nil {
			return false
		}
	}

	commitCmd := exec.Command("git", "commit", "--no-edit")
	commitCmd.Dir = cwd
	return commitCmd.Run() == nil
}

// ─── Task Prompt Builder ─────────────────────────────────────────

func buildTaskPrompt(task bd.Issue, specID, filesStr, tier, agent, specTaskID, reportPath string) string {
	desc := task.Description
	return fmt.Sprintf(`/deliver "%s — %s.%s Risk: %s. Agent: %s. Spec ref: %s"

IMPORTANT: Before you finish, write a JSON report to "%s" with this structure:
{
  "summary": "1-2 sentence summary of what you accomplished",
  "blockers": ["list of things that blocked progress or required workarounds"],
  "errors": ["list of errors encountered during execution"],
  "decisions": ["key decisions you made and why"],
  "suggestions": ["improvements for future similar tasks"]
}
If everything went smoothly, blockers and errors should be empty arrays. Always write this file before finishing.`,
		task.Title, desc, filesStr, tier, agent, specTaskID, reportPath)
}

// ─── Dry Run ─────────────────────────────────────────────────────

func showDryRun(specID, cwd string) {
	allTasks, err := bd.List([]string{fmt.Sprintf("spec:%s", specID)}, "task", "", cwd)
	if err != nil {
		ui.Log.Error(fmt.Sprintf("Could not list tasks: %v", err))
		return
	}
	ready, _ := bd.Ready([]string{fmt.Sprintf("spec:%s", specID)}, cwd, "task")
	readyIDs := make(map[string]bool)
	for _, r := range ready {
		readyIDs[r.ID] = true
	}

	if len(allTasks) == 0 {
		ui.Log.Info("No tasks found.")
		return
	}

	// Scoping analysis
	var warnings []string
	for _, task := range allTasks {
		tier := extractLabel(task, "tier:")
		if tier == "" {
			tier = "?"
		}
		var filesLikely []string
		if fl, ok := task.Metadata["files_likely"]; ok {
			if arr, ok := fl.([]any); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						filesLikely = append(filesLikely, s)
					}
				}
			}
		}
		desc := strings.ToLower(task.Description)

		scopeSignals := 0
		// Multiple conjunctions in title
		conjRe := regexp.MustCompile(`\b(and|,)\b.*\b(and|,)\b`)
		if conjRe.MatchString(task.Title) {
			scopeSignals++
		}
		if len(filesLikely) > 6 {
			scopeSignals++
		}
		compRe := regexp.MustCompile(`comprehensive|full|complete|all\s+(crud|endpoints|tests)`)
		if compRe.MatchString(desc) {
			scopeSignals++
		}
		multiRe := regexp.MustCompile(`multiple (entities|models|tables|endpoints)`)
		if multiRe.MatchString(desc) {
			scopeSignals++
		}
		stateRe := regexp.MustCompile(`state\s*machine`)
		if stateRe.MatchString(desc) {
			scopeSignals++
		}

		if scopeSignals >= 2 && tier != "T3" {
			warnings = append(warnings, fmt.Sprintf("%s %s (%s but has %d complexity signals — consider T3 or decompose further)",
				ui.Yellow("Under-scoped?"), task.Title, ui.Cyan(tier), scopeSignals))
		}
	}

	// Phase summary
	phases := make(map[string][]bd.Issue)
	for _, t := range allTasks {
		p := extractLabel(t, "phase:")
		if p == "" {
			p = "?"
		}
		phases[p] = append(phases[p], t)
	}

	// Sort phase keys
	phaseKeys := make([]string, 0, len(phases))
	for k := range phases {
		phaseKeys = append(phaseKeys, k)
	}
	sort.Slice(phaseKeys, func(i, j int) bool {
		a, _ := strconv.Atoi(phaseKeys[i])
		b, _ := strconv.Atoi(phaseKeys[j])
		return a < b
	})

	for _, phase := range phaseKeys {
		tasks := phases[phase]
		tierCounts := make(map[string]int)
		for _, t := range tasks {
			tier := extractLabel(t, "tier:")
			if tier == "" {
				tier = "?"
			}
			tierCounts[tier]++
		}
		var tierParts []string
		for t, c := range tierCounts {
			tierParts = append(tierParts, fmt.Sprintf("%dx%s", c, t))
		}
		sort.Strings(tierParts)
		ui.Log.Step(fmt.Sprintf("Phase %s: %d tasks (%s)", ui.Cyan(phase), len(tasks), strings.Join(tierParts, ", ")))

		for _, task := range tasks {
			tier := extractLabel(task, "tier:")
			if tier == "" {
				tier = "?"
			}
			est := tierEstimates[tier]
			if est == 0 {
				est = tierEstimates["T2"]
			}
			marker := ui.Dim("blocked")
			if readyIDs[task.ID] {
				marker = ui.Green("ready")
			}
			ui.Log.Message(fmt.Sprintf("  %s %s %s %s", marker, ui.Cyan(tier), task.Title,
				ui.Dim(fmt.Sprintf("(~%s est)", formatElapsed(est)))))
		}
	}

	// Scope warnings
	if len(warnings) > 0 {
		ui.Log.Step(ui.Bold(ui.Yellow("Scoping Warnings")))
		for _, w := range warnings {
			ui.Log.Message(fmt.Sprintf("  %s", w))
		}
	}

	// Time estimate
	tierCounts := make(map[string]int)
	for _, t := range allTasks {
		tier := extractLabel(t, "tier:")
		if tier == "" {
			tier = "T2"
		}
		tierCounts[tier]++
	}
	var estTimeMs int64
	for tier, count := range tierCounts {
		est := tierEstimates[tier]
		if est == 0 {
			est = tierEstimates["T2"]
		}
		estTimeMs += est * int64(count)
	}
	ui.Log.Info(fmt.Sprintf("Estimated serial time: %s (without retries)", ui.Cyan(formatElapsed(estTimeMs))))
}

// ─── Reporting ───────────────────────────────────────────────────

func showPhaseRetro(pr *PhaseReport) {
	var failedTasks []TaskReport
	var blockers []string
	for _, t := range pr.Tasks {
		if t.Status != "success" {
			failedTasks = append(failedTasks, t)
		}
		blockers = append(blockers, t.Blockers...)
	}

	if len(failedTasks) == 0 && len(blockers) == 0 {
		ui.Log.Info(ui.Dim("  No issues in this phase."))
		return
	}

	if len(failedTasks) > 0 {
		ui.Log.Warn(fmt.Sprintf("  %s", ui.Yellow(fmt.Sprintf("%d failed tasks:", len(failedTasks)))))
		for _, t := range failedTasks {
			errMsg := t.Status
			if len(t.Errors) > 0 {
				errMsg = t.Errors[0]
			}
			ui.Log.Message(fmt.Sprintf("    %s %s: %s", ui.Red("x"), t.Title, errMsg))
		}
	}

	if len(blockers) > 0 {
		unique := uniqueStrings(blockers)
		ui.Log.Warn(fmt.Sprintf("  %s", ui.Yellow(fmt.Sprintf("%d blockers identified:", len(unique)))))
		for _, b := range unique {
			ui.Log.Message(fmt.Sprintf("    %s %s", ui.Dim("-"), b))
		}
	}
}

func writePhaseReport(reportsDir string, pr *PhaseReport) {
	filename := strings.ReplaceAll(pr.Phase, ":", "-") + ".json"
	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return
	}
	_ = util.WriteText(filepath.Join(reportsDir, filename), string(data))
}

func writeAllReports(path string, reports []TaskReport) {
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return
	}
	_ = util.WriteText(path, string(data))
}

func generateMudaAnalysis(reportsDir string, allReports []TaskReport, phaseReports []PhaseReport, totalElapsed string) {
	totalTasks := len(allReports)
	successCount := 0
	failCount := 0
	conflictCount := 0
	timeoutCount := 0
	salvagedCount := 0
	totalRetries := 0

	for _, r := range allReports {
		switch r.Status {
		case "success":
			successCount++
		case "failure":
			failCount++
		case "merge_conflict":
			conflictCount++
		case "timed_out":
			timeoutCount++
		}
		if r.FailureCategory == "idle_killed_with_deliverables" {
			salvagedCount++
		}
		totalRetries += r.Retries
	}

	// Collect blockers, errors, suggestions
	var allBlockers, allErrors, allSuggestions []string
	for _, r := range allReports {
		allBlockers = append(allBlockers, r.Blockers...)
		allErrors = append(allErrors, r.Errors...)
	}

	blockerFreq := freqMap(allBlockers, 80)
	errorFreq := freqMap(allErrors, 80)

	// Slowest tasks
	sorted := make([]TaskReport, len(allReports))
	copy(sorted, allReports)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ElapsedMs > sorted[j].ElapsedMs
	})
	slowest := sorted
	if len(slowest) > 5 {
		slowest = slowest[:5]
	}

	// Agent performance
	type agentStat struct {
		success int
		fail    int
		totalMs int64
	}
	agentStats := make(map[string]*agentStat)
	for _, r := range allReports {
		if _, ok := agentStats[r.Agent]; !ok {
			agentStats[r.Agent] = &agentStat{}
		}
		agentStats[r.Agent].totalMs += r.ElapsedMs
		if r.Status == "success" {
			agentStats[r.Agent].success++
		} else {
			agentStats[r.Agent].fail++
		}
	}

	successRate := 0
	if totalTasks > 0 {
		successRate = int(math.Round(float64(successCount) / float64(totalTasks) * 100))
	}

	lines := []string{
		"# Muda Analysis — Waste & Blocker Report",
		"",
		fmt.Sprintf("Generated: %s", time.Now().UTC().Format(time.RFC3339)),
		fmt.Sprintf("Total elapsed: %s", totalElapsed),
		"",
		"## Summary",
		"",
		"| Metric | Count |",
		"|--------|-------|",
		fmt.Sprintf("| Total tasks | %d |", totalTasks),
		fmt.Sprintf("| Succeeded | %d |", successCount),
		fmt.Sprintf("| Failed | %d |", failCount),
		fmt.Sprintf("| Timed out (no deliverables) | %d |", timeoutCount),
		fmt.Sprintf("| Merge conflicts | %d |", conflictCount),
		fmt.Sprintf("| Salvaged from timeout | %d |", salvagedCount),
		fmt.Sprintf("| Total retries | %d |", totalRetries),
		fmt.Sprintf("| Success rate | %d%% |", successRate),
		"",
	}

	if len(blockerFreq) > 0 {
		lines = append(lines, "## Blockers (Muda — Waiting Waste)", "",
			"Recurring blockers indicate systemic issues that slow the pipeline.", "")
		for _, kv := range sortedFreq(blockerFreq) {
			lines = append(lines, fmt.Sprintf("- **%dx** %s", kv.count, kv.key))
		}
		lines = append(lines, "")
	}

	if len(errorFreq) > 0 {
		lines = append(lines, "## Errors (Muda — Defect Waste)", "",
			"Recurring errors suggest missing prerequisites, bad assumptions, or spec gaps.", "")
		for _, kv := range sortedFreq(errorFreq) {
			lines = append(lines, fmt.Sprintf("- **%dx** %s", kv.count, kv.key))
		}
		lines = append(lines, "")
	}

	if len(slowest) > 0 {
		lines = append(lines, "## Slowest Tasks (Muda — Processing Waste)", "",
			"Tasks taking disproportionately long may need decomposition or better context.", "")
		for _, t := range slowest {
			lines = append(lines, fmt.Sprintf("- **%s** %s (%s, %s)", formatElapsed(t.ElapsedMs), t.Title, t.Tier, t.Agent))
		}
		lines = append(lines, "")
	}

	if len(agentStats) > 0 {
		lines = append(lines, "## Agent Performance", "",
			"| Agent | Success | Failed | Avg Time |",
			"|-------|---------|--------|----------|")
		for agent, stats := range agentStats {
			total := stats.success + stats.fail
			avg := "-"
			if total > 0 {
				avg = formatElapsed(stats.totalMs / int64(total))
			}
			lines = append(lines, fmt.Sprintf("| %s | %d | %d | %s |", agent, stats.success, stats.fail, avg))
		}
		lines = append(lines, "")
	}

	// Merge conflicts
	var conflicts []TaskReport
	for _, r := range allReports {
		if r.Status == "merge_conflict" {
			conflicts = append(conflicts, r)
		}
	}
	if len(conflicts) > 0 {
		lines = append(lines, "## Merge Conflicts (Muda — Motion Waste)", "",
			"Conflicts indicate tasks touching overlapping files. Consider:",
			"- Reducing concurrency for tightly coupled epics",
			"- Reordering tasks to serialize shared-file work", "")
		for _, c := range conflicts {
			lines = append(lines, fmt.Sprintf("- **%s** — branch `%s` preserved for manual merge", c.Title, c.WorktreeBranch))
		}
		lines = append(lines, "")
	}

	// Failure taxonomy
	failureCats := make(map[string]int)
	for _, r := range allReports {
		if r.FailureCategory != "" {
			failureCats[r.FailureCategory]++
		}
	}
	if len(failureCats) > 0 {
		lines = append(lines, "## Failure Taxonomy", "",
			"Failures grouped by root cause — different causes need different remediation.", "")
		catLabels := map[string]string{
			"idle_killed":                  "Idle-killed (no output, no deliverables)",
			"idle_killed_with_deliverables": "Idle-killed (salvaged — deliverables found)",
			"timeout":                      "Timeout (legacy)",
			"rate_limit":                   "Rate limit / usage cap",
			"merge_conflict":               "Merge conflict",
			"file_not_found":               "File not found",
			"permission_denied":            "Permission denied",
			"api_overloaded":               "API overloaded",
			"worktree_error":               "Git worktree error",
			"unknown":                      "Unknown",
		}
		for _, kv := range sortedFreqMap(failureCats) {
			label := kv.key
			if l, ok := catLabels[kv.key]; ok {
				label = l
			}
			lines = append(lines, fmt.Sprintf("- **%dx** %s", kv.count, label))
		}
		lines = append(lines, "")
	}

	// Cost & efficiency
	if totalRetries > 0 || totalTasks > 0 {
		var totalElapsedMs int64
		for _, r := range allReports {
			totalElapsedMs += r.ElapsedMs
		}
		lines = append(lines, "## Cost & Efficiency", "",
			fmt.Sprintf("- Total task-time: %s", formatElapsed(totalElapsedMs)),
			fmt.Sprintf("- Total retries: %d", totalRetries))
		if salvagedCount > 0 {
			lines = append(lines, fmt.Sprintf("- Salvaged from timeout: %d tasks (would have been marked failed without deliverable check)", salvagedCount))
		}
		avgMs := int64(0)
		if totalTasks > 0 {
			avgMs = totalElapsedMs / int64(totalTasks)
		}
		lines = append(lines, fmt.Sprintf("- Avg task time: %s", formatElapsed(avgMs)), "")
	}

	// Suggestions from agents
	if len(allSuggestions) > 0 {
		unique := uniqueStrings(allSuggestions)
		lines = append(lines, "## Agent Suggestions (Kaizen — Continuous Improvement)", "")
		for _, s := range unique {
			lines = append(lines, fmt.Sprintf("- %s", s))
		}
		lines = append(lines, "")
	}

	// Phase breakdown
	if len(phaseReports) > 0 {
		lines = append(lines, "## Phase Breakdown", "")
		for _, pr := range phaseReports {
			phaseElapsed := "?"
			if pr.FinishedAt != "" && pr.StartedAt != "" {
				start, e1 := time.Parse(time.RFC3339, pr.StartedAt)
				end, e2 := time.Parse(time.RFC3339, pr.FinishedAt)
				if e1 == nil && e2 == nil {
					phaseElapsed = formatElapsed(end.Sub(start).Milliseconds())
				}
			}
			lines = append(lines, fmt.Sprintf("### %s (%s)", pr.Phase, phaseElapsed), "",
				fmt.Sprintf("- %d succeeded, %d failed", pr.SuccessCount, pr.FailureCount))
			var phaseBlockers []string
			for _, t := range pr.Tasks {
				phaseBlockers = append(phaseBlockers, t.Blockers...)
			}
			if len(phaseBlockers) > 0 {
				unique := uniqueStrings(phaseBlockers)
				lines = append(lines, fmt.Sprintf("- Blockers: %s", strings.Join(unique, "; ")))
			}
			lines = append(lines, "")
		}
	}

	_ = util.WriteText(filepath.Join(reportsDir, "muda-analysis.md"), strings.Join(lines, "\n"))
}

// ─── Failure Categorization ──────────────────────────────────────

func categorizeFailure(errStr string) string {
	lower := strings.ToLower(errStr)
	patterns := []struct {
		re       *regexp.Regexp
		category string
	}{
		{regexp.MustCompile(`hit your limit|rate.?limit|too many requests|429`), "rate_limit"},
		{regexp.MustCompile(`timed? ?out|timeout|etimedout`), "timeout"},
		{regexp.MustCompile(`merge conflict`), "merge_conflict"},
		{regexp.MustCompile(`enoent|not found|no such file`), "file_not_found"},
		{regexp.MustCompile(`permission denied|eacces`), "permission_denied"},
		{regexp.MustCompile(`overloaded`), "api_overloaded"},
		{regexp.MustCompile(`worktree`), "worktree_error"},
	}
	for _, p := range patterns {
		if p.re.MatchString(lower) {
			return p.category
		}
	}
	return "unknown"
}

// ─── Rate Limit Helpers ──────────────────────────────────────────

var rateLimitRe = regexp.MustCompile(`(?i)hit your limit|rate.?limit|too many requests|429|overloaded`)

func isRateLimited(output string) bool {
	return rateLimitRe.MatchString(output)
}

// getRateLimitWait parses "resets Xam/pm" or falls back to exponential backoff.
// Returns wait time in milliseconds.
func getRateLimitWait(output string, attempt int) int64 {
	resetRe := regexp.MustCompile(`(?i)resets\s+(\d{1,2})(am|pm)`)
	if m := resetRe.FindStringSubmatch(output); m != nil {
		hour, _ := strconv.Atoi(m[1])
		isPM := strings.ToLower(m[2]) == "pm"
		resetHour := hour
		if isPM && hour != 12 {
			resetHour = hour + 12
		} else if !isPM && hour == 12 {
			resetHour = 0
		}

		now := time.Now()
		reset := time.Date(now.Year(), now.Month(), now.Day(), resetHour, 0, 0, 0, now.Location())
		if !reset.After(now) {
			reset = reset.Add(24 * time.Hour)
		}
		waitMs := reset.Sub(now).Milliseconds()
		// Cap at 2 hours
		maxWait := int64(2 * 60 * 60 * 1000)
		if waitMs > maxWait {
			waitMs = maxWait
		}
		return waitMs
	}

	// Exponential backoff: 30s, 60s, 120s, 240s, 480s
	wait := int64(30000) * int64(math.Pow(2, float64(attempt-1)))
	if wait > 480000 {
		wait = 480000
	}
	return wait
}

// ─── Stale Worktree Cleanup ─────────────────────────────────────

func cleanupStaleWorktree(cwd, worktreePath, branchName string) {
	rmCmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
	rmCmd.Dir = cwd
	_ = rmCmd.Run()

	delCmd := exec.Command("git", "branch", "-D", branchName)
	delCmd.Dir = cwd
	_ = delCmd.Run()

	pruneCmd := exec.Command("git", "worktree", "prune")
	pruneCmd.Dir = cwd
	_ = pruneCmd.Run()
}

// ─── Utilities ───────────────────────────────────────────────────

// formatElapsed is declared in runstatus.go (shared between run and run-status).

func extractLabel(task bd.Issue, prefix string) string {
	for _, l := range task.Labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}

func extractPhaseLabel(task bd.Issue) string {
	for _, l := range task.Labels {
		if strings.HasPrefix(l, "phase:") {
			return l
		}
	}
	return ""
}

func extractPhaseNum(task bd.Issue) int {
	s := extractLabel(task, "phase:")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 99
	}
	return n
}

func getSpecTaskID(task bd.Issue) string {
	if task.Metadata != nil {
		if v, ok := task.Metadata["spec_task_id"]; ok {
			return fmt.Sprint(v)
		}
	}
	return task.ID
}

func gitExec(cwd string, timeout time.Duration, args ...string) (string, error) {
	return util.RunCmd(cwd, timeout, "git", args...)
}

func failStr(n int) string {
	if n > 0 {
		return ui.Red(strconv.Itoa(n))
	}
	return ui.Dim("0")
}

func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

type freqEntry struct {
	key   string
	count int
}

func freqMap(items []string, maxKeyLen int) map[string]int {
	freq := make(map[string]int)
	for _, item := range items {
		if item == "" {
			continue
		}
		key := item
		if len(key) > maxKeyLen {
			key = key[:maxKeyLen]
		}
		freq[key]++
	}
	return freq
}

func sortedFreq(freq map[string]int) []freqEntry {
	return sortedFreqMap(freq)
}

func sortedFreqMap(freq map[string]int) []freqEntry {
	entries := make([]freqEntry, 0, len(freq))
	for k, v := range freq {
		entries = append(entries, freqEntry{key: k, count: v})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})
	return entries
}

// Ensure io is used (for the ring buffer reader interface compliance).
var _ io.Writer = (*ringBuffer)(nil)
