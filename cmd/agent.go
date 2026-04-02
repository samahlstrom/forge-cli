package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

var agentBody string

func init() {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agents in your toolkit",
	}

	agentCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all agents",
		RunE:  runAgentList,
	})

	agentCmd.AddCommand(&cobra.Command{
		Use:   "show [name]",
		Short: "Print an agent's full definition",
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentShow,
	})

	agentCmd.AddCommand(&cobra.Command{
		Use:   "edit [name]",
		Short: "Open an agent in your editor",
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentEdit,
	})

	addCmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a new agent to your toolkit",
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentAdd,
	}
	addCmd.Flags().StringVar(&agentBody, "body", "", "Full markdown body for the agent")
	agentCmd.AddCommand(addCmd)

	rootCmd.AddCommand(agentCmd)
}

func runAgentList(_ *cobra.Command, _ []string) error {
	agents := resolve.ListAgents()
	if len(agents) == 0 {
		fmt.Println("No agents found. Run 'forge setup' first.")
		return nil
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })

	for _, a := range agents {
		fmt.Printf("  %s  %s\n", a.Name, ui.Dim(a.Path))
	}
	return nil
}

func runAgentShow(_ *cobra.Command, args []string) error {
	path := resolve.ResolveAgent(args[0])
	if path == "" {
		return fmt.Errorf("agent %q not found — add it with: forge agent add %s --body '...'", args[0], args[0])
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func runAgentEdit(_ *cobra.Command, args []string) error {
	name := args[0]
	path := resolve.ResolveAgent(name)
	if path == "" {
		return fmt.Errorf("agent %q not found — add it with: forge agent add %s --body '...'", name, name)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runAgentAdd(_ *cobra.Command, args []string) error {
	name := args[0]

	if !resolve.IsSetup() {
		return fmt.Errorf("forge not set up — run 'forge setup' first")
	}

	agentPath := filepath.Join(resolve.AgentsDir(), name+".md")

	if util.Exists(agentPath) {
		return fmt.Errorf("agent %q already exists — use 'forge agent edit %s' instead", name, name)
	}

	content := agentBody
	if content == "" {
		content = scaffoldAgent(name)
	}

	if err := util.WriteText(agentPath, content); err != nil {
		return fmt.Errorf("failed to write agent: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Created %s", agentPath))

	commitAndPush(
		filepath.Join("agents", name+".md"),
		fmt.Sprintf("feat: add %s agent", name),
	)
	return nil
}

func scaffoldAgent(name string) string {
	return fmt.Sprintf(`---
id: %s
name: %s
type: builder
specializes: ""
good_at: ""
files: ""
report_format: json
---

# %s

> Describe what this agent does.

## Agent Contract

**You MUST follow this lifecycle. No exceptions.**

1. **OPEN**: Announce what you are about to do.
2. **WORK**: Execute your instructions.
3. **REPORT**: Output a structured report.
4. **CLOSE**: State explicitly: "Agent complete. Returning control to dispatcher."

## Role

You are the %s agent. Describe your role here.

## Process

1. Read the subtask.
2. Implement the work.
3. Verify using the project's verification commands.

## Constraints

- Follow all patterns in the project's stack conventions
`, name, name, name, name)
}
