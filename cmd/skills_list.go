package cmd

import (
	"os"
	"sort"
	"strings"

	"github.com/samahlstrom/forge-cli/internal/resolve"
	"github.com/samahlstrom/forge-cli/internal/ui"
)

// The auto-generated skills list lives between these markers, INSIDE the
// hand-written "## Skills" section of the toolkit AGENTS.md. Regeneration
// replaces only the marked region, so the surrounding directives are preserved.
const forgeSkillsBegin = "<!-- BEGIN FORGE SKILLS -->"
const forgeSkillsEnd = "<!-- END FORGE SKILLS -->"

// renderSkillsBlock builds the marker-bounded list of compact skill lines,
// sorted alphabetically by name so the output never churns on map/dir order.
func renderSkillsBlock(skills []resolve.SkillInfo) string {
	sorted := append([]resolve.SkillInfo(nil), skills...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	b.WriteString(forgeSkillsBegin)
	for _, s := range sorted {
		b.WriteString("\n- `/")
		b.WriteString(s.Name)
		b.WriteString("`")
		if sum := s.Summary(); sum != "" {
			b.WriteString(" — ")
			b.WriteString(sum)
		}
	}
	b.WriteString("\n")
	b.WriteString(forgeSkillsEnd)
	return b.String()
}

// injectSkillsList returns content with the auto-generated skills list refreshed
// from skills. If the markers already exist it replaces only between them;
// otherwise it locates the "## Skills" heading and replaces that section's body
// (up to the next "## " heading or EOF) with the marked block, establishing the
// markers. Content without a "## Skills" heading is returned untouched.
func injectSkillsList(content string, skills []resolve.SkillInfo) string {
	block := renderSkillsBlock(skills)

	// Markers present — replace only the marked region (idempotent on re-run).
	if b := strings.Index(content, forgeSkillsBegin); b >= 0 {
		if e := strings.Index(content, forgeSkillsEnd); e > b {
			return content[:b] + block + content[e+len(forgeSkillsEnd):]
		}
	}

	// First run — find the "## Skills" heading and replace its body.
	lines := strings.Split(content, "\n")
	heading := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "## Skills") {
			heading = i
			break
		}
	}
	if heading < 0 {
		return content
	}
	end := len(lines)
	for i := heading + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	rebuilt := strings.Join(lines[:heading+1], "\n") + "\n" + block + "\n"
	if tail := strings.Join(lines[end:], "\n"); tail != "" {
		rebuilt += "\n" + tail
	}
	return rebuilt
}

// regenerateToolkitSkills rewrites the auto-generated skills list in the toolkit
// manifest (~/.forge/AGENTS.md) from the installed skills, so the list never
// drifts as skills are added or removed. Run on forge init/sync before the
// manifest is embedded/imported elsewhere. No-op if the manifest can't be read
// or the regenerated content is unchanged.
func regenerateToolkitSkills() {
	path := resolve.ToolkitManifestPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return // no toolkit manifest yet — forge sync will create it
	}
	updated := injectSkillsList(string(data), resolve.ListSkills())
	if updated == string(data) {
		return
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		ui.Log.Warn("Could not regenerate AGENTS.md skills list: " + err.Error())
		return
	}
	ui.Log.Success("Regenerated AGENTS.md skills list")
}
