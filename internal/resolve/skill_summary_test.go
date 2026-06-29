package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, frontmatter string) SkillInfo {
	t.Helper()
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(frontmatter), 0o644); err != nil {
		t.Fatal(err)
	}
	return SkillInfo{Name: filepath.Base(dir), Path: p}
}

func TestSkillSummaryPrefersSummaryFrontmatter(t *testing.T) {
	dir := t.TempDir()
	s := writeSkill(t, dir, "---\nname: foo\nsummary: short curated line\ndescription: A very long description. With many sentences. That should be ignored.\n---\n\n# Foo\n")
	if got := s.Summary(); got != "short curated line" {
		t.Fatalf("Summary() = %q, want curated summary", got)
	}
}

func TestSkillSummaryFallsBackToFirstSentenceOfDescription(t *testing.T) {
	dir := t.TempDir()
	s := writeSkill(t, dir, "---\nname: bar\ndescription: Does the first thing well. Then a lot more trigger text the compact line should drop.\n---\n\n# Bar\n")
	if got := s.Summary(); got != "Does the first thing well" {
		t.Fatalf("Summary() = %q, want first sentence of description", got)
	}
}

func TestSkillSummaryNoTrailingPeriodWholeDescriptionWhenSingleSentence(t *testing.T) {
	dir := t.TempDir()
	s := writeSkill(t, dir, "---\nname: baz\ndescription: One clause only with no terminal period\n---\n")
	if got := s.Summary(); got != "One clause only with no terminal period" {
		t.Fatalf("Summary() = %q, want whole description", got)
	}
}

// YAML-quoted scalar values must have their surrounding quotes stripped, else
// the compact line leaks a leading `"` (real SKILL.md files quote descriptions).
func TestSkillSummaryStripsYAMLQuotes(t *testing.T) {
	dir := t.TempDir()
	s := writeSkill(t, dir, "---\nname: dq\ndescription: \"Use this skill to do the thing. And more trigger text.\"\n---\n")
	if got := s.Summary(); got != "Use this skill to do the thing" {
		t.Fatalf("Summary() = %q, want quote-stripped first sentence", got)
	}
	dir2 := t.TempDir()
	s2 := writeSkill(t, dir2, "---\nname: sq\nsummary: 'single quoted curated line'\n---\n")
	if got := s2.Summary(); got != "single quoted curated line" {
		t.Fatalf("Summary() = %q, want quote-stripped summary", got)
	}
}

func TestSkillSummaryEmptyWhenFileMissing(t *testing.T) {
	s := SkillInfo{Name: "gone", Path: filepath.Join(t.TempDir(), "nope", "SKILL.md")}
	if got := s.Summary(); got != "" {
		t.Fatalf("Summary() = %q, want empty for missing file", got)
	}
}

// description: itself may contain a colon-keyed word later; the reader must only
// pick fields from the frontmatter block, matched at line start.
func TestSkillSummaryIgnoresBodyAndInlineColons(t *testing.T) {
	dir := t.TempDir()
	s := writeSkill(t, dir, "---\nname: q\ndescription: Lead clause here. summary: not this one\n---\n\nsummary: definitely not this body line\n")
	if got := s.Summary(); got != "Lead clause here" {
		t.Fatalf("Summary() = %q, want lead clause from description", got)
	}
}
