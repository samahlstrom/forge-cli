package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
	"github.com/samahlstrom/forge-cli/internal/util"


	"github.com/spf13/cobra"
)

var skillBody string

func init() {
	skillCmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage skills in your toolkit",
	}

	skillCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all skills",
		RunE:  runSkillList,
	})

	skillCmd.AddCommand(&cobra.Command{
		Use:   "show [name]",
		Short: "Output a skill's content",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkillShow,
	})

	skillCmd.AddCommand(&cobra.Command{
		Use:   "edit [name]",
		Short: "Open a skill in your editor",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkillEdit,
	})

	addCmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a new skill to your toolkit",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkillAdd,
	}
	addCmd.Flags().StringVar(&skillBody, "body", "", "Full markdown body for the skill")
	skillCmd.AddCommand(addCmd)

	skillCmd.AddCommand(&cobra.Command{
		Use:   "remove [name]",
		Short: "Remove a skill from your toolkit",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkillRemove,
	})

	rootCmd.AddCommand(skillCmd)
}

func runSkillList(_ *cobra.Command, _ []string) error {
	skills := resolve.ListSkills()
	if len(skills) == 0 {
		fmt.Println("No skills found. Run 'forge setup' first.")
		return nil
	}

	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })

	for _, s := range skills {
		fmt.Printf("  %s  %s\n", s.Name, ui.Dim(s.Path))
	}
	return nil
}

func runSkillShow(_ *cobra.Command, args []string) error {
	path := resolve.ResolveSkill(args[0])
	if path == "" {
		return fmt.Errorf("skill %q not found — add it with: forge skill add %s --body '...'", args[0], args[0])
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func runSkillEdit(_ *cobra.Command, args []string) error {
	name := args[0]
	path := resolve.ResolveSkill(name)
	if path == "" {
		return fmt.Errorf("skill %q not found — add it with: forge skill add %s --body '...'", name, name)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runSkillAdd(_ *cobra.Command, args []string) error {
	name := args[0]

	if !resolve.IsSetup() {
		return fmt.Errorf("forge not set up — run 'forge setup' first")
	}

	skillDir := filepath.Join(resolve.SkillsDir(), name)
	skillFile := filepath.Join(skillDir, "SKILL.md")

	if util.Exists(skillFile) {
		return fmt.Errorf("skill %q already exists — use 'forge skill edit %s' instead", name, name)
	}

	content := skillBody
	if content == "" {
		content = scaffoldSkill(name)
	}

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", skillDir, err)
	}

	if err := util.WriteText(skillFile, content); err != nil {
		return fmt.Errorf("failed to write skill: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Created %s", skillFile))

	commitAndPush(
		filepath.Join("skills", name, "SKILL.md"),
		fmt.Sprintf("feat: add %s skill", name),
	)

	wireSkill(name)
	return nil
}

func runSkillRemove(_ *cobra.Command, args []string) error {
	name := args[0]

	if !resolve.IsSetup() {
		return fmt.Errorf("forge not set up — run 'forge setup' first")
	}

	skillDir := filepath.Join(resolve.SkillsDir(), name)
	if !util.Exists(filepath.Join(skillDir, "SKILL.md")) {
		return fmt.Errorf("skill %q not found", name)
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("failed to remove skill: %w", err)
	}

	ui.Log.Success(fmt.Sprintf("Removed skill: %s", name))

	// Remove symlink from current project if wired
	unwireSkill(name)

	commitAndPush(
		filepath.Join("skills", name),
		fmt.Sprintf("feat: remove %s skill", name),
	)
	return nil
}

func scaffoldSkill(name string) string {
	return "---\nname: " + name + "\ndescription: Describe when this skill should trigger — what user phrases or contexts activate it.\n---\n\n# " + name + "\n\n> Describe what this skill does.\n\n## Trigger\n\nUser runs `/" + name + "` or describes a task matching this skill's purpose.\n\n## Process\n\n1. Understand the user's intent.\n2. Execute the workflow.\n3. Report the result.\n\n## Constraints\n\n- Follow all patterns in the project's conventions\n"
}
