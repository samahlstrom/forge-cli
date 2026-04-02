package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "paths",
		Short: "Print resolved paths for the toolkit",
		RunE:  runPaths,
	})
}

func runPaths(_ *cobra.Command, _ []string) error {
	paths := map[string]string{
		"forge_home":   resolve.ForgeHome(),
		"agents_dir":   resolve.AgentsDir(),
		"skills_dir":   resolve.SkillsDir(),
		"pipeline_dir": resolve.PipelineDir(),
	}

	out, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
