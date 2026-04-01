package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "paths",
		Short: "Print resolved paths for the current project",
		Long:  "Outputs JSON with all resolved paths (global library, project state, runs, context). Used by the /forge skill to locate files.",
		RunE:  runPaths,
	})
}

func runPaths(_ *cobra.Command, _ []string) error {
	cwd, _ := os.Getwd()

	projectDir := resolve.ProjectDir(cwd)
	forgeYAML := filepath.Join(projectDir, "forge.yaml")

	paths := map[string]string{
		"forge_home":   resolve.ForgeHome(),
		"project_id":   resolve.ProjectID(cwd),
		"project_dir":  projectDir,
		"forge_yaml":   forgeYAML,
		"runs_dir":     resolve.ProjectRunsDir(cwd),
		"context_dir":  resolve.ProjectContextDir(cwd),
		"agents_dir":   resolve.GlobalDir("agents"),
		"pipeline_dir": resolve.GlobalDir("pipeline"),
		"hooks_dir":    resolve.GlobalDir("hooks"),
		"skills_dir":   resolve.GlobalDir("skills"),
	}

	out, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
