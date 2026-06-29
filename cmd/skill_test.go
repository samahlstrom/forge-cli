package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillAddUploadsExistingDirectoryPreservingExecBits(t *testing.T) {
	forge := newToolkit(t)

	// A multi-file skill the user already wrote, with an executable bin/ script.
	src := filepath.Join(t.TempDir(), "slacker")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: slacker\n---\n# slacker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "post"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	skillBody, skillFile = "", src
	defer func() { skillBody, skillFile = "", "" }()

	if err := runSkillAdd(nil, []string{"slacker"}); err != nil {
		t.Fatalf("runSkillAdd --file dir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(forge, "skills", "slacker", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md not copied: %v", err)
	}
	bin, err := os.Stat(filepath.Join(forge, "skills", "slacker", "bin", "post"))
	if err != nil {
		t.Fatalf("bin/post not copied: %v", err)
	}
	if bin.Mode().Perm()&0o111 == 0 {
		t.Fatalf("bin/post lost its executable bit: %v", bin.Mode().Perm())
	}
}

func TestSkillAddUploadsSingleFile(t *testing.T) {
	forge := newToolkit(t)

	src := filepath.Join(t.TempDir(), "whatever.md")
	if err := os.WriteFile(src, []byte("---\nname: solo\n---\n# solo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	skillBody, skillFile = "", src
	defer func() { skillBody, skillFile = "", "" }()

	if err := runSkillAdd(nil, []string{"solo"}); err != nil {
		t.Fatalf("runSkillAdd --file file: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(forge, "skills", "solo", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "" {
		t.Fatal("SKILL.md empty after single-file upload")
	}
}

func TestSkillAddBodyStillScaffolds(t *testing.T) {
	forge := newToolkit(t)

	skillBody, skillFile = "", ""
	defer func() { skillBody, skillFile = "", "" }()

	if err := runSkillAdd(nil, []string{"fresh"}); err != nil {
		t.Fatalf("runSkillAdd scaffold: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forge, "skills", "fresh", "SKILL.md")); err != nil {
		t.Fatalf("scaffolded SKILL.md missing: %v", err)
	}
}

func TestAgentAddUploadsExistingFile(t *testing.T) {
	forge := newToolkit(t)

	src := filepath.Join(t.TempDir(), "persona.md")
	body := "---\nid: myhero\n---\n# myhero\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	agentBody, agentFile = "", src
	defer func() { agentBody, agentFile = "", "" }()

	if err := runAgentAdd(nil, []string{"myhero"}); err != nil {
		t.Fatalf("runAgentAdd --file: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(forge, "agents", "myhero.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("agent content not copied verbatim: %q", got)
	}
}
