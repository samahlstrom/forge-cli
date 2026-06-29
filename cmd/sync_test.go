package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// TestSyncRoundTripsAllThreeTypesToRemote proves `forge sync` pushes locally
// committed agents, skills, and hooks to the toolkit remote. Everything runs in
// temp dirs (FORGE_HOME, HOME, CODEX_HOME) against a bare local remote — it never
// touches the real ~/.forge.
func TestSyncRoundTripsAllThreeTypesToRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome) // contain wireAllSkillsGlobal (~/.claude)
	t.Setenv("CODEX_HOME", filepath.Join(tmpHome, "codex"))

	// Bare remote.
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare")

	// Toolkit with an initial commit + upstream.
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)
	gitInit(t, forge)
	for _, d := range []string{"agents", filepath.Join("skills", "demo"), "hooks"} {
		if err := os.MkdirAll(filepath.Join(forge, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(forge, "README.md"), []byte("toolkit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, forge, "add", "-A")
	gitRun(t, forge, "commit", "-m", "init")
	branch := gitOut(t, forge, "rev-parse", "--abbrev-ref", "HEAD")
	gitRun(t, forge, "remote", "add", "origin", remote)
	gitRun(t, forge, "push", "-u", "origin", branch)

	// Commit one of each content type locally (not yet pushed).
	writeFiles(t, forge, map[string]string{
		filepath.Join("agents", "persona.md"):       "# persona\n",
		filepath.Join("skills", "demo", "SKILL.md"): "# demo\n",
		filepath.Join("hooks", "manifest.json"):     `{"hooks":[]}`,
		filepath.Join("hooks", "g.sh"):              "#!/bin/sh\n",
	})
	gitRun(t, forge, "add", "-A")
	gitRun(t, forge, "commit", "-m", "add agents+skills+hooks")

	if err := runSync(nil, nil); err != nil {
		t.Fatalf("runSync: %v", err)
	}

	// Clone the remote fresh and confirm all three rode along.
	co := filepath.Join(t.TempDir(), "co")
	gitRun(t, ".", "clone", remote, co)
	for _, p := range []string{
		filepath.Join("agents", "persona.md"),
		filepath.Join("skills", "demo", "SKILL.md"),
		filepath.Join("hooks", "manifest.json"),
		filepath.Join("hooks", "g.sh"),
	} {
		if _, err := os.Stat(filepath.Join(co, p)); err != nil {
			t.Fatalf("forge sync did not round-trip %s to the remote: %v", p, err)
		}
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
