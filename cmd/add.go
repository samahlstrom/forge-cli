package cmd

import (
	"github.com/samahlstrom/forge-cli/internal/addon"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "add <addon>",
		Short: "Install an optional addon (e.g., browser-testing, compliance-hipaa)",
		Args:  cobra.ExactArgs(1),
		RunE:  runAdd,
	})
}

func runAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold(fmt.Sprintf("forge add %s", name)))

	if !addon.IsValid(name) {
		ui.Log.Error(fmt.Sprintf("Unknown addon: %q", name))
		ui.Log.Message(fmt.Sprintf("Available addons: %s", strings.Join(addon.ListAvailable(), ", ")))
		os.Exit(1)
	}

	if !util.Exists(filepath.Join(cwd, "forge.yaml")) {
		ui.Log.Error("No forge.yaml found. Run `forge init` first.")
		os.Exit(1)
	}

	var config map[string]any
	_ = util.ReadYAML(filepath.Join(cwd, "forge.yaml"), &config)
	addons := toStringSlice(config["addons"])
	for _, a := range addons {
		if a == name {
			ui.Log.Warn(fmt.Sprintf("Addon %q is already installed.", name))
			os.Exit(0)
		}
	}

	spinner := ui.NewSpinner()
	spinner.Start(fmt.Sprintf("Installing %s...", name))

	files, err := addon.Install(name, cwd)
	if err != nil {
		spinner.Stop("Installation failed")
		ui.Log.Error(err.Error())
		os.Exit(1)
	}

	for _, f := range files {
		ui.Log.Success(fmt.Sprintf("Created %s", f))
	}
	ui.Log.Success("Updated forge.yaml")

	// Post-install commands
	manifest, _ := addon.GetManifest(name)
	for _, cmdStr := range manifest.PostInstall {
		spinner.SetMessage(fmt.Sprintf("Running: %s", cmdStr))
		c := exec.Command("bash", "-c", cmdStr)
		c.Dir = cwd
		if err := c.Run(); err != nil {
			ui.Log.Warn(fmt.Sprintf("Post-install command failed: %s", cmdStr))
			ui.Log.Message("  You may need to run this manually.")
		} else {
			ui.Log.Success(fmt.Sprintf("Ran: %s", cmdStr))
		}
	}

	spinner.Stop(fmt.Sprintf("%s installed", name))
	ui.Outro(ui.Green("Done!"))
	return nil
}
