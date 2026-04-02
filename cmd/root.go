package cmd

import (
	"fmt"
	"os"

	"github.com/samahlstrom/forge-cli/internal/static"
	"github.com/samahlstrom/forge-cli/internal/updater"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "forge",
	Short: "Portable AI agent toolkit for Claude Code",
	Long: `forge — Portable AI agent toolkit for Claude Code

Your personal library of agents, skills, and workflows.
Take it anywhere, use it in any project, add to it anytime.

Getting started:
  forge setup       Install your toolkit to ~/.forge/
  forge sync        Pull latest tools from the forge repo
  forge list        See everything in your toolkit

Managing tools:
  forge agent list              List all agents
  forge agent add <name>        Add a new agent
  forge agent edit <name>       Edit an existing agent
  forge skill <name>            Load a skill into Claude Code

Your toolkit lives in ~/.forge/ — a git clone of your forge repo.
Claude Code reads from it at runtime. Zero footprint in your projects.`,
}

func Execute() {
	rootCmd.Version = static.Version
	updater.RefreshInBackground()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	updater.NotifyIfAvailable(static.Version)
}
