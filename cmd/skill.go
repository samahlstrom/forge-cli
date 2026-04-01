package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/static"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "skill [name]",
		Short: "Output a skill's SKILL.md content for agent consumption",
		Long: `Prints the full SKILL.md content for a named skill.
Use this to load forge skills into any agent session:

  forge skill forge     — outputs the /forge pipeline orchestrator
  forge skill ingest    — outputs the /ingest spec decomposer`,
		Args: cobra.ExactArgs(1),
		RunE: runSkillOutput,
	}
	rootCmd.AddCommand(cmd)
}

func runSkillOutput(_ *cobra.Command, args []string) error {
	name := args[0]

	// Check ~/.forge/skills/<name>/SKILL.md first
	globalPath := filepath.Join(resolve.ForgeHome(), "skills", name, "SKILL.md")
	if data, err := os.ReadFile(globalPath); err == nil {
		fmt.Print(string(data))
		return nil
	}

	// Fall back to embedded template
	templatePath := fmt.Sprintf("templates/core/skill-%s.md.hbs", name)
	if data, err := fs.ReadFile(static.TemplatesFS, templatePath); err == nil {
		fmt.Print(string(data))
		return nil
	}

	return fmt.Errorf("skill %q not found", name)
}
