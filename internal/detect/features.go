package detect

import (
	"github.com/samahlstrom/forge-cli/internal/util"
	"path/filepath"
)

// FeatureFlags indicates which CI/CD and tooling features are present.
type FeatureFlags struct {
	Git        bool   `json:"git"`
	CI         string `json:"ci,omitempty"` // github-actions, gitlab-ci, jenkins, or ""
	Docker     bool   `json:"docker"`
	Playwright bool   `json:"playwright"`
	Semgrep    bool   `json:"semgrep"`
	Firebase   bool   `json:"firebase"`
	Vercel     bool   `json:"vercel"`
}

// DetectFeatures checks for CI, Docker, and other tooling.
func DetectFeatures(cwd string) FeatureFlags {
	f := FeatureFlags{
		Git:        util.Exists(filepath.Join(cwd, ".git")),
		Docker:     util.Exists(filepath.Join(cwd, "Dockerfile")) || util.Exists(filepath.Join(cwd, "docker-compose.yml")),
		Playwright: util.Exists(filepath.Join(cwd, "playwright.config.ts")) || util.Exists(filepath.Join(cwd, "playwright.config.js")),
		Semgrep:    util.Exists(filepath.Join(cwd, ".semgrep.yml")),
		Firebase:   util.Exists(filepath.Join(cwd, "firebase.json")),
		Vercel:     util.Exists(filepath.Join(cwd, "vercel.json")),
	}

	switch {
	case util.Exists(filepath.Join(cwd, ".github", "workflows")):
		f.CI = "github-actions"
	case util.Exists(filepath.Join(cwd, ".gitlab-ci.yml")):
		f.CI = "gitlab-ci"
	case util.Exists(filepath.Join(cwd, "Jenkinsfile")):
		f.CI = "jenkins"
	}

	return f
}
