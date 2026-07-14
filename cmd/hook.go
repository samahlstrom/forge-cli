package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"

	"github.com/spf13/cobra"
)

var (
	hookFile     string
	hookScaffold bool
	hookGitHook  string
	hookEvent    string
	hookMatcher  string
	hookDefault  bool
	hookScope    string
)

func init() {
	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage hooks in your toolkit",
	}

	hookCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the hooks declared in your toolkit manifest",
		RunE:  runHookList,
	})

	addCmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a hook to your toolkit (upload a script and register it in the manifest)",
		Long: `Add a hook to your toolkit. Upload a script you already wrote with --file,
or scaffold a blank one with --scaffold, then register it in the manifest so
'forge init' installs it.

Examples:
  forge hook add my-gate --file ./my-gate.sh --git-hook pre-push --default
  forge hook add pr-check --file ./pr-check.sh --event PreToolUse --matcher Bash
  forge hook add new-gate --scaffold --git-hook pre-push`,
		Args: cobra.ExactArgs(1),
		RunE: runHookAdd,
	}
	addCmd.Flags().StringVar(&hookFile, "file", "", "Path to an existing hook script to upload")
	addCmd.Flags().BoolVar(&hookScaffold, "scaffold", false, "Create a blank hook script instead of uploading one")
	addCmd.Flags().StringVar(&hookGitHook, "git-hook", "", "Install as a git hook of this type (e.g. pre-push)")
	addCmd.Flags().StringVar(&hookEvent, "event", "", "Install as a Claude settings hook on this event (e.g. PreToolUse)")
	addCmd.Flags().StringVar(&hookMatcher, "matcher", "", "Matcher for a Claude settings hook (e.g. Bash)")
	addCmd.Flags().BoolVar(&hookDefault, "default", false, "Install automatically during forge init/sync (default: opt-in)")
	addCmd.Flags().StringVar(&hookScope, "scope", "repo", "Hook scope")
	hookCmd.AddCommand(addCmd)

	hookCmd.AddCommand(&cobra.Command{
		Use:   "remove [name]",
		Short: "Remove a hook from your toolkit",
		Args:  cobra.ExactArgs(1),
		RunE:  runHookRemove,
	})

	rootCmd.AddCommand(hookCmd)
}

func runHookList(_ *cobra.Command, _ []string) error {
	hooks := resolve.ListHooks()
	if len(hooks) == 0 {
		fmt.Println("No hooks found.")
		return nil
	}
	sort.Slice(hooks, func(i, j int) bool { return hooks[i].Name < hooks[j].Name })
	for _, h := range hooks {
		detail := h.GitHook
		if h.Kind == "claude-settings-hook" {
			detail = fmt.Sprintf("%s(%s)", h.Event, h.Matcher)
		}
		mode := "opt-in"
		if h.Default {
			mode = "default"
		}
		fmt.Printf("  %s  %s %s  %s\n", h.Name, h.Kind, detail, ui.Dim(mode))
	}
	return nil
}

func runHookAdd(_ *cobra.Command, args []string) error {
	name := args[0]

	if !resolve.IsSetup() {
		return fmt.Errorf("forge not set up — run 'forge setup' first")
	}

	// Kind is derived from the flags — the installer switches on it later.
	kind, gitHook, event, matcher := "", "", "", ""
	switch {
	case hookGitHook != "":
		kind, gitHook = "git-hook", hookGitHook
	case hookEvent != "":
		if hookMatcher == "" {
			return fmt.Errorf("--event requires --matcher (e.g. --event PreToolUse --matcher Bash)")
		}
		kind, event, matcher = "claude-settings-hook", hookEvent, hookMatcher
	default:
		return fmt.Errorf("specify the hook kind: --git-hook <type> (e.g. pre-push) or --event <event> --matcher <matcher>")
	}

	if hookFile == "" && !hookScaffold {
		return fmt.Errorf("provide a script with --file <path>, or --scaffold to create a blank one")
	}
	if hookFile != "" && hookScaffold {
		return fmt.Errorf("--file and --scaffold are mutually exclusive")
	}

	// The script filename under the toolkit's hooks/ dir (the manifest's "script").
	script := name + ".sh"
	if hookFile != "" {
		script = filepath.Base(hookFile)
	}
	dst := resolve.HookScriptPath(script)
	if util.Exists(dst) {
		return fmt.Errorf("hook script %q already exists in your toolkit — remove it first or rename", script)
	}

	if hookFile != "" {
		if !util.Exists(hookFile) {
			return fmt.Errorf("file not found: %s", hookFile)
		}
		if err := util.CopyFile(hookFile, dst, 0o755); err != nil { // hooks must be executable
			return fmt.Errorf("failed to copy hook script: %w", err)
		}
	} else {
		if err := util.WriteText(dst, scaffoldHookScript(name)); err != nil {
			return fmt.Errorf("failed to write hook script: %w", err)
		}
		if err := os.Chmod(dst, 0o755); err != nil {
			return err
		}
	}

	if err := resolve.UpsertHook(resolve.HookInfo{
		Name: name, Kind: kind, GitHook: gitHook, Event: event, Matcher: matcher,
		Script: script, Scope: hookScope, Default: hookDefault,
	}); err != nil {
		return fmt.Errorf("failed to update manifest: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Added hook %q (%s) → hooks/%s", name, kind, script))
	if !hookDefault {
		ui.Log.Info(fmt.Sprintf("Opt-in hook — install it per repo with: forge init --enable-hook %s", name))
	}

	commitAndPushN(
		fmt.Sprintf("feat: add %s hook", name),
		filepath.Join("hooks", script),
		filepath.Join("hooks", "manifest.json"),
	)
	return nil
}

func runHookRemove(_ *cobra.Command, args []string) error {
	name := args[0]

	if !resolve.IsSetup() {
		return fmt.Errorf("forge not set up — run 'forge setup' first")
	}

	removed, ok, err := resolve.RemoveHookFromManifest(name)
	if err != nil {
		return fmt.Errorf("failed to update manifest: %w", err)
	}
	if !ok {
		return fmt.Errorf("hook %q not found in manifest", name)
	}

	staged := []string{filepath.Join("hooks", "manifest.json")}
	// Delete the script too, unless another manifest entry still references it.
	if removed.Script != "" && !hookScriptStillReferenced(removed.Script) {
		_ = os.Remove(resolve.HookScriptPath(removed.Script))
		staged = append(staged, filepath.Join("hooks", removed.Script))
	}

	ui.Log.Success(fmt.Sprintf("Removed hook: %s", name))
	commitAndPushN(fmt.Sprintf("feat: remove %s hook", name), staged...)
	return nil
}

// hookScriptStillReferenced reports whether any remaining manifest entry uses script.
func hookScriptStillReferenced(script string) bool {
	for _, h := range resolve.ListHooks() {
		if h.Script == script {
			return true
		}
	}
	return false
}

func scaffoldHookScript(name string) string {
	return "#!/usr/bin/env bash\n" +
		"# " + name + " — forge toolkit hook. Edit this script, then run 'forge sync' to share it.\n" +
		"set -euo pipefail\n\n" +
		"exit 0\n"
}
