package cmd

import (
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/static"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "forge",
	Short: "Agent harness scaffolding for Claude Code",
}

func Execute() {
	rootCmd.Version = static.Version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
