package refine

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// IterationResult records what happened in one iteration.
type IterationResult struct {
	Iteration  int
	Timestamp  time.Time
	Status     string // "baseline", "improved", "regressed", "measure_failed", "agent_failed"
	Values     map[string]float64
	CommitSHA  string
	DurationS  float64
	AgentError string
}

// InitResultsFile creates the TSV file with headers.
func InitResultsFile(path string, metricNames []string) error {
	headers := []string{"iteration", "timestamp", "status"}
	headers = append(headers, metricNames...)
	headers = append(headers, "commit_sha", "duration_s", "error")
	return os.WriteFile(path, []byte(strings.Join(headers, "\t")+"\n"), 0o644)
}

// AppendResult adds a row to the TSV file.
func AppendResult(path string, r IterationResult, metricNames []string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	fields := []string{
		fmt.Sprintf("%d", r.Iteration),
		r.Timestamp.UTC().Format(time.RFC3339),
		r.Status,
	}
	for _, name := range metricNames {
		if v, ok := r.Values[name]; ok {
			fields = append(fields, fmt.Sprintf("%.6f", v))
		} else {
			fields = append(fields, "")
		}
	}
	sha := r.CommitSHA
	if sha == "" {
		sha = "(reverted)"
	}
	fields = append(fields, sha, fmt.Sprintf("%.1f", r.DurationS), r.AgentError)

	_, err = f.WriteString(strings.Join(fields, "\t") + "\n")
	return err
}

// FormatHistory returns a human-readable summary of the last N results for the agent prompt.
func FormatHistory(results []IterationResult, metricNames []string, lastN int) string {
	if len(results) == 0 {
		return "No iterations yet."
	}

	start := 0
	if len(results) > lastN {
		start = len(results) - lastN
	}

	var sb strings.Builder
	for _, r := range results[start:] {
		sb.WriteString(fmt.Sprintf("Iteration %d [%s]: ", r.Iteration, r.Status))
		parts := make([]string, 0, len(metricNames))
		for _, name := range metricNames {
			if v, ok := r.Values[name]; ok {
				parts = append(parts, fmt.Sprintf("%s=%.4f", name, v))
			}
		}
		sb.WriteString(strings.Join(parts, ", "))
		if r.AgentError != "" {
			sb.WriteString(fmt.Sprintf(" (error: %s)", r.AgentError))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
