package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samahlstrom/forge-cli/internal/resolve"
)

// TestScaffoldToolkitDirsMakesToolkitSetup guards the bring-your-own flow: a
// fresh (empty) toolkit must still register as set up, with content dirs that
// survive git/clone, so `forge skill/agent/hook add` work right after setup.
func TestScaffoldToolkitDirsMakesToolkitSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_HOME", home)

	if resolve.IsSetup() {
		t.Fatal("precondition: a bare temp dir should not look set up")
	}
	if err := scaffoldToolkitDirs(home); err != nil {
		t.Fatalf("scaffoldToolkitDirs: %v", err)
	}
	if !resolve.IsSetup() {
		t.Fatal("after scaffolding, resolve.IsSetup() must be true")
	}
	// .gitkeep keeps the (otherwise empty) dirs tracked so they survive clone.
	for _, d := range []string{"agents", "skills", "hooks"} {
		if _, err := os.Stat(filepath.Join(home, d, ".gitkeep")); err != nil {
			t.Fatalf("%s/.gitkeep missing: %v", d, err)
		}
	}
}

func TestExtractedFileModeMarksShellScriptsExecutable(t *testing.T) {
	cases := []struct {
		rel  string
		exec bool
	}{
		{"hooks/pre-push-validate.sh", true},
		{"hooks/validate-gate.sh", true},
		{"hooks/manifest.json", false},
		{"skills/validate/SKILL.md", false},
		{"agents/forge.md", false},
	}
	for _, c := range cases {
		got := extractedFileMode(c.rel)
		wantExec := got&0o111 != 0
		if wantExec != c.exec {
			t.Fatalf("extractedFileMode(%q) exec=%v, want %v (mode=%v)", c.rel, wantExec, c.exec, got)
		}
	}
}
