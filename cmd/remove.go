package cmd

import (
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/addon"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "remove <addon>",
		Short: "Remove an installed addon",
		Args:  cobra.ExactArgs(1),
		RunE:  runRemove,
	})
}

func runRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold(fmt.Sprintf("forge remove %s", name)))

	if !addon.IsValid(name) {
		ui.Log.Error(fmt.Sprintf("Unknown addon: %q", name))
		os.Exit(1)
	}

	if !util.Exists(filepath.Join(cwd, "forge.yaml")) {
		ui.Log.Error("No forge.yaml found. Run `forge init` first.")
		os.Exit(1)
	}

	var config map[string]any
	_ = util.ReadYAML(filepath.Join(cwd, "forge.yaml"), &config)
	addons := toStringSlice(config["addons"])
	found := false
	for _, a := range addons {
		if a == name {
			found = true
			break
		}
	}
	if !found {
		ui.Log.Warn(fmt.Sprintf("Addon %q is not installed.", name))
		os.Exit(0)
	}

	spinner := ui.NewSpinner()
	spinner.Start(fmt.Sprintf("Removing %s...", name))

	files, err := addon.Uninstall(name, cwd)
	if err != nil {
		spinner.Stop("Removal failed")
		ui.Log.Error(err.Error())
		os.Exit(1)
	}

	for _, f := range files {
		ui.Log.Success(fmt.Sprintf("Deleted %s", f))
	}
	ui.Log.Success("Updated forge.yaml")

	spinner.Stop(fmt.Sprintf("%s removed", name))
	ui.Outro(ui.Green("Done!"))
	return nil
}
