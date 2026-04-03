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
  forge setup       Create your personal toolkit at ~/.forge/
  forge init        Wire skills into the current project
  forge init -g     Install skills globally (all sessions)
  forge list        See everything in your toolkit

Managing tools:
  forge agent list              List all agents
  forge agent add <name>        Add a new agent
  forge agent edit <name>       Edit an existing agent
  forge skill list              List all skills
  forge skill add <name>        Add a new skill
  forge skill edit <name>       Edit an existing skill

Your toolkit lives in ~/.forge/ — a git repo you own.
Add a remote to sync across machines. Zero footprint in your projects.`,
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
