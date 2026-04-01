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

func init() {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage your agent library",
	}

	agentCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all agents (global + local overrides)",
		RunE:  runAgentList,
	})

	agentCmd.AddCommand(&cobra.Command{
		Use:   "edit [name]",
		Short: "Open an agent definition in your editor",
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentEdit,
	})

	agentCmd.AddCommand(&cobra.Command{
		Use:   "create [name]",
		Short: "Scaffold a new agent in ~/.forge/agents/",
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentCreate,
	})

	agentCmd.AddCommand(&cobra.Command{
		Use:   "promote [path]",
		Short: "Copy a local agent override to the global library",
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentPromote,
	})

	rootCmd.AddCommand(agentCmd)
}

func runAgentList(_ *cobra.Command, _ []string) error {
	cwd, _ := os.Getwd()
	agents := resolve.ListAgents(cwd)

	if len(agents) == 0 {
		fmt.Println("No agents found. Run `forge setup` to bootstrap your library.")
		return nil
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })

	for _, a := range agents {
		source := ui.Dim("global")
		if !a.Global {
			source = ui.Yellow("local override")
		}
		fmt.Printf("  %s  %s  %s\n", a.Name, source, ui.Dim(a.Path))
	}
	return nil
}

func runAgentEdit(_ *cobra.Command, args []string) error {
	name := args[0]
	cwd, _ := os.Getwd()

	path := resolve.ResolveAgent(cwd, name)
	if path == "" {
		return fmt.Errorf("agent %q not found — create it with: forge agent create %s", name, name)
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

func runAgentCreate(_ *cobra.Command, args []string) error {
	name := args[0]
	agentPath := filepath.Join(resolve.ForgeHome(), "agents", name+".md")

	if util.Exists(agentPath) {
		return fmt.Errorf("agent %q already exists at %s — use `forge agent edit %s` instead", name, agentPath, name)
	}

	scaffold := fmt.Sprintf(`---
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
2. Read context from forge.yaml and .forge/context/.
3. Implement the work.
4. Verify using the project's verification commands (see forge.yaml).

## Constraints

- Follow all patterns in the project's stack conventions
`, name, name, name, name)

	if err := util.WriteText(agentPath, scaffold); err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Created %s", agentPath))
	ui.Log.Step(fmt.Sprintf("Edit with: forge agent edit %s", name))
	return nil
}

func runAgentPromote(_ *cobra.Command, args []string) error {
	sourcePath := args[0]

	if !util.Exists(sourcePath) {
		return fmt.Errorf("file not found: %s", sourcePath)
	}

	name := filepath.Base(sourcePath)
	destPath := filepath.Join(resolve.ForgeHome(), "agents", name)

	if err := util.CopyFile(sourcePath, destPath); err != nil {
		return fmt.Errorf("failed to promote agent: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Promoted %s → %s", sourcePath, destPath))
	return nil
}
