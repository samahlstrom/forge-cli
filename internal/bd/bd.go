package bd

import (
	"encoding/json"
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/util"
	"os/exec"
	"strings"
	"time"
)

// Issue represents a bd issue/task/epic.
type Issue struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	Labels      []string       `json:"labels"`
	Priority    int            `json:"priority"`
	Parent      string         `json:"parent,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// CreateOpts are options for creating a bd issue.
type CreateOpts struct {
	Title       string
	Description string
	Type        string
	Labels      []string
	Parent      string
	Metadata    map[string]any
	Deps        []string
}

const timeout = 15 * time.Second

// Create creates a new bd issue and returns its ID.
func Create(opts CreateOpts, cwd string) (string, error) {
	args := []string{"create", util.ShellSafe(opts.Title)}
	if opts.Description != "" {
		args = append(args, "-d", util.ShellSafe(opts.Description))
	}
	if opts.Type != "" {
		args = append(args, "-t", opts.Type)
	}
	if len(opts.Labels) > 0 {
		args = append(args, "-l", strings.Join(opts.Labels, ","))
	}
	if opts.Parent != "" {
		args = append(args, "--parent", opts.Parent)
	}
	if opts.Metadata != nil {
		meta, _ := json.Marshal(opts.Metadata)
		args = append(args, "--metadata", string(meta))
	}
	if len(opts.Deps) > 0 {
		args = append(args, "--deps", strings.Join(opts.Deps, ","))
	}
	args = append(args, "--json")

	out, err := runBd(cwd, args...)
	if err != nil {
		return "", fmt.Errorf("bd create: %w", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return strings.TrimSpace(out), nil
	}
	for _, key := range []string{"id", "ID", "issue_id"} {
		if v, ok := parsed[key]; ok {
			return fmt.Sprint(v), nil
		}
	}
	return strings.TrimSpace(out), nil
}

// Link creates a dependency link between two issues.
func Link(from, to, linkType, cwd string) error {
	_, err := runBd(cwd, "link", from, to, "--type", linkType)
	return err
}

// Ready returns issues that are ready to be worked on.
func Ready(labels []string, cwd, issueType string) ([]Issue, error) {
	args := []string{"ready", "--json", "-n", "100"}
	if issueType != "" {
		args = append(args, "-t", issueType)
	}
	if len(labels) > 0 {
		args = append(args, "-l", strings.Join(labels, ","))
	}
	return listQuery(cwd, args...)
}

// Close marks an issue as closed.
func Close(id, cwd string) error {
	_, err := runBd(cwd, "close", id)
	return err
}

// Update modifies an issue's fields.
func Update(id string, status string, cwd string) error {
	args := []string{"update", id}
	if status != "" {
		args = append(args, "-s", status)
	}
	args = append(args, "--json")
	_, err := runBd(cwd, args...)
	return err
}

// List returns issues matching the given filters.
func List(labels []string, issueType, status, cwd string) ([]Issue, error) {
	args := []string{"list", "--json"}
	if len(labels) > 0 {
		args = append(args, "-l", strings.Join(labels, ","))
	}
	if issueType != "" {
		args = append(args, "-t", issueType)
	}
	if status != "" {
		args = append(args, "-s", status)
	}
	return listQuery(cwd, args...)
}

// Show returns a single issue by ID.
func Show(id, cwd string) (Issue, error) {
	out, err := runBd(cwd, "show", id, "--json")
	if err != nil {
		return Issue{}, err
	}
	var issue Issue
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		return Issue{}, err
	}
	return issue, nil
}

// Count returns the number of matching issues.
func Count(labels []string, cwd string) (int, error) {
	args := []string{"count"}
	if len(labels) > 0 {
		args = append(args, "-l", strings.Join(labels, ","))
	}
	out, err := runBd(cwd, args...)
	if err != nil {
		return 0, err
	}
	var n int
	fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n, nil
}

func runBd(cwd string, args ...string) (string, error) {
	cmd := exec.Command("bd", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	return string(out), err
}

func listQuery(cwd string, args ...string) ([]Issue, error) {
	out, err := runBd(cwd, args...)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	// Try array first
	var issues []Issue
	if err := json.Unmarshal([]byte(trimmed), &issues); err == nil {
		return issues, nil
	}
	// Try single object
	var single Issue
	if err := json.Unmarshal([]byte(trimmed), &single); err == nil {
		return []Issue{single}, nil
	}
	return nil, nil
}
