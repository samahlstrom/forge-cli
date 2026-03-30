package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/bd"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// TaskReport represents a single task execution report from all-tasks.json.
type TaskReport struct {
	TaskID          string   `json:"task_id"`
	BeadID          string   `json:"bead_id"`
	Title           string   `json:"title"`
	Phase           string   `json:"phase"`
	Agent           string   `json:"agent"`
	Tier            string   `json:"tier"`
	Status          string   `json:"status"` // success | failure | merge_conflict | timed_out
	StartedAt       string   `json:"started_at"`
	FinishedAt      string   `json:"finished_at"`
	ElapsedMs       int64    `json:"elapsed_ms"`
	WorktreeBranch  string   `json:"worktree_branch"`
	FilesChanged    []string `json:"files_changed"`
	Blockers        []string `json:"blockers"`
	Errors          []string `json:"errors"`
	Summary         string   `json:"summary"`
	Retries         int      `json:"retries"`
	FailureCategory string   `json:"failure_category"`
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "run-status <spec-id>",
		Short: "Show health and progress of a spec run",
		Args:  cobra.ExactArgs(1),
		RunE:  runRunStatus,
	})
}

func runRunStatus(cmd *cobra.Command, args []string) error {
	specID := args[0]
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold("forge run-status") + ui.Dim(fmt.Sprintf(" — %s", specID)))

	specDir := filepath.Join(cwd, ".forge", "specs", specID)
	if !util.Exists(filepath.Join(specDir, "spec.yaml")) {
		ui.Cancel(fmt.Sprintf("No spec.yaml found for %s.", specID))
		os.Exit(1)
	}

	// Get all tasks from bd
	var allTasks []bd.Issue
	tasks, err := bd.List([]string{fmt.Sprintf("spec:%s", specID)}, "task", "", cwd)
	if err != nil {
		ui.Log.Warn("Could not query beads — bd may not be initialized.")
	} else {
		allTasks = tasks
	}

	var openTasks, closedTasks []bd.Issue
	for _, t := range allTasks {
		switch t.Status {
		case "open", "in_progress":
			openTasks = append(openTasks, t)
		case "closed":
			closedTasks = append(closedTasks, t)
		}
	}

	// Read reports if they exist
	reportsFile := filepath.Join(specDir, "reports", "all-tasks.json")
	var reports []TaskReport
	if util.Exists(reportsFile) {
		data, err := util.ReadText(reportsFile)
		if err == nil {
			_ = json.Unmarshal([]byte(data), &reports)
		}
	}

	// Categorize reports
	var succeeded, failed, conflicts, timedOut, salvaged []TaskReport
	totalRetries := 0
	for _, r := range reports {
		switch r.Status {
		case "success":
			succeeded = append(succeeded, r)
		case "failure":
			failed = append(failed, r)
		case "merge_conflict":
			conflicts = append(conflicts, r)
		case "timed_out":
			timedOut = append(timedOut, r)
		}
		if r.FailureCategory == "idle_killed_with_deliverables" {
			salvaged = append(salvaged, r)
		}
		totalRetries += r.Retries
	}

	// ── Overview ──
	total := len(allTasks)
	pct := 0
	if total > 0 {
		pct = int(math.Round(float64(len(closedTasks)) / float64(total) * 100))
	}
	bar := progressBar(pct, 30)

	failStr := ui.Dim("0")
	if len(failed) > 0 {
		failStr = ui.Red(fmt.Sprint(len(failed)))
	}
	timeoutStr := ui.Dim("0")
	if len(timedOut) > 0 {
		timeoutStr = ui.Yellow(fmt.Sprint(len(timedOut)))
	}
	conflictStr := ui.Dim("0")
	if len(conflicts) > 0 {
		conflictStr = ui.Yellow(fmt.Sprint(len(conflicts)))
	}
	openStr := ui.Dim("0")
	if len(openTasks) > 0 {
		openStr = ui.Yellow(fmt.Sprint(len(openTasks)))
	}

	overview := []string{
		fmt.Sprintf("Progress:  %s %d%%", bar, pct),
		fmt.Sprintf("Total:     %s tasks", ui.Cyan(fmt.Sprint(total))),
		fmt.Sprintf("Closed:    %s", ui.Green(fmt.Sprint(len(closedTasks)))),
		fmt.Sprintf("Open:      %s", openStr),
		fmt.Sprintf("Reported:  %s (%s ok, %s fail, %s timeout, %s conflict)",
			ui.Dim(fmt.Sprint(len(reports))),
			ui.Green(fmt.Sprint(len(succeeded))),
			failStr, timeoutStr, conflictStr),
	}
	if len(salvaged) > 0 {
		overview = append(overview, fmt.Sprintf("Salvaged:  %s tasks recovered from timeout", ui.Cyan(fmt.Sprint(len(salvaged)))))
	}
	if totalRetries > 0 {
		overview = append(overview, fmt.Sprintf("Retries:   %s total retry attempts", ui.Yellow(fmt.Sprint(totalRetries))))
	}
	ui.Note(strings.Join(overview, "\n"), "Run Overview")

	// ── Phase breakdown ──
	type phaseStats struct {
		open   int
		closed int
		failed int
	}
	phases := map[string]*phaseStats{}
	for _, t := range allTasks {
		phase := "?"
		for _, l := range t.Labels {
			if strings.HasPrefix(l, "phase:") {
				phase = strings.TrimPrefix(l, "phase:")
				break
			}
		}
		if _, ok := phases[phase]; !ok {
			phases[phase] = &phaseStats{}
		}
		if t.Status == "closed" {
			phases[phase].closed++
		} else {
			phases[phase].open++
		}
	}
	for _, r := range reports {
		if r.Status == "failure" || r.Status == "merge_conflict" {
			phase := strings.TrimPrefix(r.Phase, "phase:")
			if s, ok := phases[phase]; ok {
				s.failed++
			}
		}
	}

	if len(phases) > 0 {
		// Sort phase keys numerically
		phaseKeys := make([]string, 0, len(phases))
		for k := range phases {
			phaseKeys = append(phaseKeys, k)
		}
		sort.Slice(phaseKeys, func(i, j int) bool {
			a, _ := strconv.Atoi(phaseKeys[i])
			b, _ := strconv.Atoi(phaseKeys[j])
			return a < b
		})

		ui.Log.Step(ui.Bold("Phases"))
		for _, phase := range phaseKeys {
			stats := phases[phase]
			phaseTotal := stats.open + stats.closed
			phasePct := 0
			if phaseTotal > 0 {
				phasePct = int(math.Round(float64(stats.closed) / float64(phaseTotal) * 100))
			}
			var status string
			if stats.open == 0 {
				status = ui.Green("done")
			} else if stats.failed > 0 {
				status = ui.Red(fmt.Sprintf("%d failed", stats.failed))
			} else {
				status = ui.Yellow("in progress")
			}
			ui.Log.Message(fmt.Sprintf("  Phase %s: %s %d%% (%d/%d) %s",
				ui.Cyan(phase), progressBar(phasePct, 15), phasePct, stats.closed, phaseTotal, status))
		}
	}

	// ── Failed tasks ──
	if len(failed) > 0 || len(timedOut) > 0 || len(conflicts) > 0 {
		ui.Log.Step(ui.Bold(ui.Red("Problem Tasks")))
		for _, r := range failed {
			errSummary := categorizeError(r.Errors)
			ui.Log.Message(fmt.Sprintf("  %s %s", ui.Red("\u2717"), r.Title))
			ui.Log.Message(fmt.Sprintf("    %s %s", ui.Dim("Error:"), errSummary))
			if r.ElapsedMs > 0 {
				ui.Log.Message(fmt.Sprintf("    %s %s", ui.Dim("Elapsed:"), formatElapsed(r.ElapsedMs)))
			}
		}
		for _, r := range timedOut {
			ui.Log.Message(fmt.Sprintf("  %s %s", ui.Yellow("\u23f1"), r.Title))
			ui.Log.Message(fmt.Sprintf("    %s %s %s", ui.Dim("Timed out after"), formatElapsed(r.ElapsedMs), ui.Dim("(no deliverables)")))
		}
		for _, r := range conflicts {
			ui.Log.Message(fmt.Sprintf("  %s %s", ui.Yellow("\u26a0"), r.Title))
			ui.Log.Message(fmt.Sprintf("    %s %s (needs manual merge)", ui.Dim("Branch:"), ui.Cyan(r.WorktreeBranch)))
		}
	}

	// ── Failure taxonomy ──
	failureCats := map[string]int{}
	for _, r := range reports {
		if r.FailureCategory != "" {
			failureCats[r.FailureCategory]++
		}
	}

	var allErrors []string
	for _, r := range reports {
		for _, e := range r.Errors {
			if e != "" {
				allErrors = append(allErrors, e)
			}
		}
	}

	if len(failureCats) > 0 {
		ui.Log.Step(ui.Bold("Failure Taxonomy"))
		sorted := sortMapDesc(failureCats)
		labels := map[string]string{
			"idle_killed":                  "Idle-killed (no output)",
			"idle_killed_with_deliverables": "Idle-killed (salvaged)",
			"timeout":                      "Timeout (legacy)",
			"rate_limit":                   "Rate limit",
			"merge_conflict":               "Merge conflict",
			"file_not_found":               "File not found",
			"permission_denied":            "Permission denied",
			"api_overloaded":               "API overloaded",
			"worktree_error":               "Worktree error",
		}
		for _, kv := range sorted {
			label := kv.key
			if l, ok := labels[kv.key]; ok {
				label = l
			}
			ui.Log.Message(fmt.Sprintf("  %s %s", ui.Red(fmt.Sprintf("%dx", kv.val)), label))
		}
	} else if len(allErrors) > 0 {
		categories := map[string]int{}
		for _, e := range allErrors {
			cat := categorizeError([]string{e})
			categories[cat]++
		}
		ui.Log.Step(ui.Bold("Error Summary"))
		sorted := sortMapDesc(categories)
		for _, kv := range sorted {
			ui.Log.Message(fmt.Sprintf("  %s %s", ui.Red(fmt.Sprintf("%dx", kv.val)), kv.key))
		}
	}

	// ── Resumability ──
	ui.Log.Step(ui.Bold("Resume"))
	if len(openTasks) == 0 {
		ui.Log.Success("All tasks complete — nothing to resume.")
	} else {
		readyCount := 0
		ready, err := bd.Ready([]string{fmt.Sprintf("spec:%s", specID)}, cwd, "task")
		if err == nil {
			readyCount = len(ready)
		}

		ui.Log.Info(fmt.Sprintf("%s tasks remaining, %s ready to run",
			ui.Cyan(fmt.Sprint(len(openTasks))), ui.Green(fmt.Sprint(readyCount))))
		if len(openTasks) > 0 && readyCount == 0 {
			ui.Log.Warn(ui.Yellow("All remaining tasks are blocked — check dependencies or merge conflicts."))
		}
		ui.Log.Message(fmt.Sprintf("  %s forge run %s --yes", ui.Dim("Resume with:"), specID))
	}

	// ── Muda report pointer ──
	mudaPath := filepath.Join(specDir, "reports", "muda-analysis.md")
	if util.Exists(mudaPath) {
		ui.Log.Info(fmt.Sprintf("Full analysis: %s", ui.Dim(fmt.Sprintf(".forge/specs/%s/reports/muda-analysis.md", specID))))
	}

	ui.Outro("")
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────

func progressBar(pct int, width int) string {
	filled := int(math.Round(float64(pct) / 100 * float64(width)))
	empty := width - filled
	if filled < 0 {
		filled = 0
	}
	if empty < 0 {
		empty = 0
	}
	return ui.Green(strings.Repeat("\u2588", filled)) + ui.Dim(strings.Repeat("\u2591", empty))
}

func categorizeError(errors []string) string {
	joined := strings.Join(errors, " ")

	patterns := []struct {
		re    *regexp.Regexp
		label string
	}{
		{regexp.MustCompile(`(?i)hit your limit|rate.?limit|too many requests|429`), "Rate limit / usage cap exceeded"},
		{regexp.MustCompile(`(?i)timed? ?out|timeout|ETIMEDOUT`), "Timeout (task exceeded 10min)"},
		{regexp.MustCompile(`(?i)merge conflict`), "Merge conflict"},
		{regexp.MustCompile(`(?i)ENOENT|not found|no such file`), "File not found"},
		{regexp.MustCompile(`(?i)permission denied|EACCES`), "Permission denied"},
		{regexp.MustCompile(`(?i)overloaded`), "API overloaded"},
		{regexp.MustCompile(`(?i)worktree`), "Git worktree error"},
	}

	for _, p := range patterns {
		if p.re.MatchString(joined) {
			return p.label
		}
	}

	first := "Unknown error"
	if len(errors) > 0 && errors[0] != "" {
		first = errors[0]
	}
	if len(first) > 80 {
		first = first[:77] + "..."
	}
	return first
}

func formatElapsed(ms int64) string {
	secs := ms / 1000
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	remSecs := secs % 60
	if mins < 60 {
		return fmt.Sprintf("%dm %ds", mins, remSecs)
	}
	hrs := mins / 60
	remMins := mins % 60
	return fmt.Sprintf("%dh %dm", hrs, remMins)
}

type kv struct {
	key string
	val int
}

func sortMapDesc(m map[string]int) []kv {
	s := make([]kv, 0, len(m))
	for k, v := range m {
		s = append(s, kv{k, v})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].val > s[j].val })
	return s
}
