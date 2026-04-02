package cmd

import (
	"fmt"
	"os"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "skill [name]",
		Short: "Output a skill's content for Claude Code consumption",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkill,
	})
}

func runSkill(_ *cobra.Command, args []string) error {
	path := resolve.ResolveSkill(args[0])
	if path == "" {
		return fmt.Errorf("skill %q not found", args[0])
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}
