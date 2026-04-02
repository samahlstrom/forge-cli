package cmd

import (
	"fmt"
	"sort"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List everything in your toolkit",
		RunE:  runList,
	})
}

func runList(_ *cobra.Command, _ []string) error {
	if !resolve.IsSetup() {
		fmt.Println("Toolkit not installed. Run 'forge setup' first.")
		return nil
	}

	agents := resolve.ListAgents()
	skills := resolve.ListSkills()

	if len(agents) == 0 && len(skills) == 0 {
		fmt.Println("Toolkit is empty.")
		return nil
	}

	if len(agents) > 0 {
		fmt.Println(ui.Bold("Agents"))
		sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
		for _, a := range agents {
			fmt.Printf("  %s\n", a.Name)
		}
		fmt.Println()
	}

	if len(skills) > 0 {
		fmt.Println(ui.Bold("Skills"))
		sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
		for _, s := range skills {
			fmt.Printf("  %s\n", s.Name)
		}
		fmt.Println()
	}

	return nil
}
