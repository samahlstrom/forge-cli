package cmd

import "testing"

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
