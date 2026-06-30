package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samahlstrom/forge-cli/internal/resolve"
)

// skillWith creates a toolkit skill dir <forge>/skills/<name>/SKILL.md with the
// given frontmatter and returns its SkillInfo.
func skillWith(t *testing.T, skillsDir, name, frontmatter string) resolve.SkillInfo {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(frontmatter), 0o644); err != nil {
		t.Fatal(err)
	}
	return resolve.SkillInfo{Name: name, Path: p}
}

const manifestFixture = `# Forge Toolkit

## Scope
Keep this hand-written directive.

---

## Skills (invoke with /skill-name)
- ` + "`/old-stale`" + ` — a stale hand-maintained line that must be replaced
- ` + "`/gone`" + ` — a skill that no longer exists

## Bead Tracking
- bd show <id> — view your assigned bead
`

func TestInjectSkillsListReplacesListAlphabeticallyWithSummaries(t *testing.T) {
	sk := t.TempDir()
	skills := []resolve.SkillInfo{
		skillWith(t, sk, "zebra", "---\nname: zebra\nsummary: last alphabetically\n---\n"),
		skillWith(t, sk, "alpha", "---\nname: alpha\nsummary: first alphabetically\n---\n"),
		skillWith(t, sk, "mid", "---\nname: mid\ndescription: Falls back to this clause. Plus dropped trigger text.\n---\n"),
	}

	got := injectSkillsList(manifestFixture, skills)

	// All three skills present as compact lines.
	for _, want := range []string{
		"- `/alpha` — first alphabetically",
		"- `/mid` — Falls back to this clause",
		"- `/zebra` — last alphabetically",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated list missing %q:\n%s", want, got)
		}
	}

	// Alphabetical order.
	ia, im, iz := strings.Index(got, "/alpha"), strings.Index(got, "/mid"), strings.Index(got, "/zebra")
	if !(ia < im && im < iz) {
		t.Fatalf("skills not in alphabetical order (alpha=%d mid=%d zebra=%d):\n%s", ia, im, iz, got)
	}

	// Stale hand-maintained lines are gone.
	if strings.Contains(got, "/old-stale") || strings.Contains(got, "/gone") {
		t.Fatalf("stale list lines not replaced:\n%s", got)
	}

	// Hand-written directives outside the section are preserved.
	for _, keep := range []string{"## Scope", "Keep this hand-written directive.", "## Bead Tracking", "bd show <id>"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("regeneration clobbered hand-written content %q:\n%s", keep, got)
		}
	}

	// Markers bound the generated list, inside the ## Skills section.
	b, e := strings.Index(got, forgeSkillsBegin), strings.Index(got, forgeSkillsEnd)
	if b < 0 || e < 0 || b > e {
		t.Fatalf("skills markers missing/disordered:\n%s", got)
	}
	if hdr := strings.Index(got, "## Skills"); !(hdr < b) {
		t.Fatalf("markers must sit inside the ## Skills section:\n%s", got)
	}
	if bead := strings.Index(got, "## Bead Tracking"); !(e < bead) {
		t.Fatalf("generated list must end before the next section:\n%s", got)
	}
}

func TestInjectSkillsListIsIdempotent(t *testing.T) {
	sk := t.TempDir()
	skills := []resolve.SkillInfo{
		skillWith(t, sk, "alpha", "---\nname: alpha\nsummary: a\n---\n"),
		skillWith(t, sk, "beta", "---\nname: beta\nsummary: b\n---\n"),
	}
	once := injectSkillsList(manifestFixture, skills)
	twice := injectSkillsList(once, skills)
	if once != twice {
		t.Fatalf("not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	if n := strings.Count(twice, forgeSkillsBegin); n != 1 {
		t.Fatalf("expected exactly one skills block after re-run, got %d", n)
	}
}

func TestInjectSkillsListNoHeadingLeavesContentUntouched(t *testing.T) {
	sk := t.TempDir()
	skills := []resolve.SkillInfo{skillWith(t, sk, "alpha", "---\nname: alpha\nsummary: a\n---\n")}
	content := "# Some doc\n\nNo skills section here.\n"
	if got := injectSkillsList(content, skills); got != content {
		t.Fatalf("content without a ## Skills heading must be left untouched:\n%s", got)
	}
}

// End-to-end through FORGE_HOME: regenerateToolkitSkills rewrites ~/.forge/AGENTS.md
// from the installed skills dir.
func TestRegenerateToolkitSkillsRewritesManifest(t *testing.T) {
	forge := t.TempDir()
	t.Setenv("FORGE_HOME", forge)

	if err := os.WriteFile(filepath.Join(forge, "AGENTS.md"), []byte(manifestFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(forge, "skills")
	skillWith(t, skillsDir, "validate", "---\nname: validate\nsummary: shot-list and verify UI states\n---\n")
	skillWith(t, skillsDir, "edgar", "---\nname: edgar\ndescription: Adversarial edge case analysis. And more.\n---\n")

	regenerateToolkitSkills()

	got := readFile(t, filepath.Join(forge, "AGENTS.md"))
	if !strings.Contains(got, "- `/edgar` — Adversarial edge case analysis") {
		t.Fatalf("manifest missing edgar (first-sentence fallback):\n%s", got)
	}
	if !strings.Contains(got, "- `/validate` — shot-list and verify UI states") {
		t.Fatalf("manifest missing validate (summary):\n%s", got)
	}
	if strings.Contains(got, "/old-stale") {
		t.Fatalf("stale line survived regeneration:\n%s", got)
	}
	if !strings.Contains(got, "## Scope") {
		t.Fatalf("regeneration clobbered hand-written directives:\n%s", got)
	}
}
