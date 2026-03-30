package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show installed preset, active addons, and pipeline health",
		RunE:  runStatus,
	})
}

func runStatus(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()

	ui.Intro(ui.Bold("forge status"))

	if !util.Exists(filepath.Join(cwd, "forge.yaml")) {
		ui.Log.Error("No forge.yaml found. Run `forge init` first.")
		os.Exit(1)
	}

	var config map[string]any
	if err := util.ReadYAML(filepath.Join(cwd, "forge.yaml"), &config); err != nil {
		return err
	}

	project, _ := config["project"].(map[string]any)
	agents := toStringSlice(config["agents"])
	addons := toStringSlice(config["addons"])
	hashes, _ := util.ReadHashes(cwd)

	var openCount, closedCount int
	bdAvailable := false
	if out, err := exec.Command("bd", "list", "--status", "open", "--json").Output(); err == nil {
		bdAvailable = true
		var items []any
		if json.Unmarshal(out, &items) == nil {
			openCount = len(items)
		}
	}
	if bdAvailable {
		if out, err := exec.Command("bd", "list", "--status", "closed", "--json").Output(); err == nil {
			var items []any
			if json.Unmarshal(out, &items) == nil {
				closedCount = len(items)
			}
		}
	}

	name, _ := project["name"].(string)
	preset, _ := project["preset"].(string)
	if name == "" {
		name = "unknown"
	}
	if preset == "" {
		preset = "unknown"
	}

	trackingStr := ui.Yellow("bd not installed")
	if bdAvailable {
		trackingStr = ui.Green("bd (beads)")
	}
	openStr := ui.Green("0")
	if openCount > 0 {
		openStr = ui.Yellow(fmt.Sprint(openCount))
	}

	lines := []string{
		fmt.Sprintf("Project:     %s", ui.Cyan(name)),
		fmt.Sprintf("Preset:      %s", ui.Cyan(preset)),
		fmt.Sprintf("Version:     %s", ui.Cyan(hashes.Version)),
		fmt.Sprintf("Agents:      %s", ui.Cyan(strings.Join(agents, ", "))),
		fmt.Sprintf("Addons:      %s", addonDisplay(addons)),
		"",
		fmt.Sprintf("Tracking:    %s", trackingStr),
		fmt.Sprintf("Open tasks:  %s", openStr),
		fmt.Sprintf("Closed:      %s", ui.Dim(fmt.Sprint(closedCount))),
	}

	ui.Note(strings.Join(lines, "\n"), "Harness Status")
	ui.Outro("")
	return nil
}

func addonDisplay(addons []string) string {
	if len(addons) == 0 {
		return ui.Dim("none")
	}
	return ui.Cyan(strings.Join(addons, ", "))
}

func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]any); ok {
		var result []string
		for _, s := range ss {
			result = append(result, fmt.Sprint(s))
		}
		return result
	}
	return nil
}
