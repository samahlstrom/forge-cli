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
	Short: "Agent harness scaffolding for Claude Code",
	Long: `forge — Agent harness scaffolding for Claude Code

Forge sets up your project so AI agents can build, test, and deliver
code through a structured pipeline with quality gates.

Getting started:
  forge init                  Set up forge in your project
  forge init --spec spec.pdf  Set up from a spec/PRD document

Once initialized, open Claude Code and tell your agent:
  /forge "add user authentication"     Full pipeline (decompose → review → execute → verify → evaluate)
  /forge --quick "fix typo in README"  Lightweight change (skip decomposition + evaluation)
  /ingest <spec-id>                    Break down a large spec into tasks

Your agent handles everything — you just describe what to build.

Other commands:
  forge doctor    Check harness health
  forge upgrade   Update forge to latest version
  forge status    Show current pipeline state`,
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
