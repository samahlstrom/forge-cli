package refine

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// MeasureResult holds the output of running the measure command.
type MeasureResult struct {
	Values    map[string]float64
	RawOutput string
	Duration  time.Duration
	Error     error
}

// RunMeasure executes the criteria's measure command and parses JSON output.
func RunMeasure(criteria *Criteria, cwd string, timeout time.Duration) *MeasureResult {
	start := time.Now()

	cmd := exec.Command("bash", "-c", criteria.Measure)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	result := &MeasureResult{
		RawOutput: strings.TrimSpace(string(out)),
		Duration:  elapsed,
	}

	if err != nil {
		result.Error = fmt.Errorf("measure command failed: %w\nOutput: %s", err, result.RawOutput)
		return result
	}

	// Parse JSON from output — find the last JSON object in the output
	jsonStr := extractJSON(result.RawOutput)
	if jsonStr == "" {
		result.Error = fmt.Errorf("measure command produced no JSON output.\nRaw output:\n%s", result.RawOutput)
		return result
	}

	var values map[string]float64
	if err := json.Unmarshal([]byte(jsonStr), &values); err != nil {
		// Try parsing as map[string]any and convert numbers
		var raw map[string]any
		if err2 := json.Unmarshal([]byte(jsonStr), &raw); err2 != nil {
			result.Error = fmt.Errorf("measure output is not valid JSON: %w\nJSON: %s", err, jsonStr)
			return result
		}
		values = make(map[string]float64)
		for k, v := range raw {
			switch n := v.(type) {
			case float64:
				values[k] = n
			case int:
				values[k] = float64(n)
			case json.Number:
				f, _ := n.Float64()
				values[k] = f
			}
		}
	}

	// Validate all required metrics are present
	for _, m := range criteria.Metrics {
		if _, ok := values[m.Name]; !ok {
			result.Error = fmt.Errorf("measure output missing metric %q. Got: %v", m.Name, values)
			return result
		}
	}

	result.Values = values
	return result
}

// extractJSON finds the last JSON object in a string (handles noisy output).
func extractJSON(s string) string {
	// Try each line from the end looking for JSON
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			return line
		}
	}

	// Try finding the last { ... } block in the entire string
	lastBrace := strings.LastIndex(s, "}")
	if lastBrace < 0 {
		return ""
	}
	firstBrace := strings.LastIndex(s[:lastBrace], "{")
	if firstBrace < 0 {
		return ""
	}
	candidate := s[firstBrace : lastBrace+1]
	if json.Valid([]byte(candidate)) {
		return candidate
	}

	return ""
}

// IsBetter returns true if newVal is better than oldVal for the given direction.
func IsBetter(newVal, oldVal float64, direction string) bool {
	if direction == "minimize" {
		return newVal < oldVal
	}
	return newVal > oldVal
}

// TargetMet returns true if the value meets the target for the given metric.
func TargetMet(value float64, metric Metric) bool {
	if !metric.HasTarget {
		return false
	}
	if metric.Direction == "minimize" {
		return value <= metric.Target
	}
	return value >= metric.Target
}

// AllTargetsMet checks if all metrics with targets have been met.
func AllTargetsMet(values map[string]float64, criteria *Criteria) bool {
	anyTarget := false
	for _, m := range criteria.Metrics {
		if !m.HasTarget {
			continue
		}
		anyTarget = true
		v, ok := values[m.Name]
		if !ok || !TargetMet(v, m) {
			return false
		}
	}
	return anyTarget
}
