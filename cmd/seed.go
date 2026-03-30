package cmd

import (
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/bd"
	"github.com/samahlstrom/forge-cli/internal/seedbeads"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var seedForce bool

func init() {
	seedCmd := &cobra.Command{
		Use:   "seed <spec-id>",
		Short: "Create beads (bd tasks) from an approved spec.yaml decomposition",
		Args:  cobra.ExactArgs(1),
		RunE:  runSeed,
	}
	seedCmd.Flags().BoolVar(&seedForce, "force", false, "Re-create beads even if they already exist")
	rootCmd.AddCommand(seedCmd)
}

func runSeed(cmd *cobra.Command, args []string) error {
	specID := args[0]
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold("forge seed") + ui.Dim(fmt.Sprintf(" — %s", specID)))

	specDir := filepath.Join(cwd, ".forge", "specs", specID)
	if !util.Exists(filepath.Join(specDir, "spec.yaml")) {
		ui.Cancel(fmt.Sprintf("No spec.yaml found at %s. Run /ingest %s first.", filepath.Join(specDir, "spec.yaml"), specID))
		os.Exit(1)
	}

	// Check existing beads
	if !seedForce {
		count, err := bd.Count([]string{fmt.Sprintf("spec:%s", specID)}, cwd)
		if err == nil && count > 0 {
			overwrite, cancelled := ui.Confirm(fmt.Sprintf("%d beads already exist for %s. Re-create?", count, specID), false)
			if cancelled || !overwrite {
				ui.Cancel("Seed cancelled.")
				os.Exit(0)
			}
		}
	}

	spinner := ui.NewSpinner()
	spinner.Start("Creating beads from spec.yaml...")

	result, err := seedbeads.SeedBeads(specDir, specID, cwd)
	if err != nil {
		spinner.Stop("Seeding failed")
		ui.Log.Error(err.Error())
		os.Exit(1)
	}

	spinner.Stop("Beads created")

	lines := []string{
		fmt.Sprintf("Phases:  %s epics", ui.Cyan(fmt.Sprint(result.Phases))),
		fmt.Sprintf("Epics:   %s epics", ui.Cyan(fmt.Sprint(result.Epics))),
		fmt.Sprintf("Tasks:   %s tasks", ui.Cyan(fmt.Sprint(result.Tasks))),
		fmt.Sprintf("Deps:    %s blocking links", ui.Cyan(fmt.Sprint(result.Links))),
	}
	ui.Note(strings.Join(lines, "\n"), "Beads seeded")

	ui.Log.Step("Next:")
	ui.Log.Message(fmt.Sprintf("  Run %s to start auto-pilot execution", ui.Cyan(fmt.Sprintf("forge run %s", specID))))
	ui.Log.Message(fmt.Sprintf("  Or %s to see what tasks are available", ui.Cyan("bd ready")))

	ui.Outro(ui.Green("Ready to execute."))
	return nil
}
