package detect

import (
	"github.com/samahlstrom/forge-cli/internal/util"
	"path/filepath"
	"strings"
)

// DetectPython detects Python projects.
func DetectPython(cwd string) *DetectedStack {
	hasPyproject := util.Exists(filepath.Join(cwd, "pyproject.toml"))
	hasRequirements := util.Exists(filepath.Join(cwd, "requirements.txt"))
	if !hasPyproject && !hasRequirements {
		return nil
	}

	result := &DetectedStack{Language: "python"}

	var depsText string
	if hasPyproject {
		if t, err := util.ReadText(filepath.Join(cwd, "pyproject.toml")); err == nil {
			depsText = t
		}
	}
	if hasRequirements {
		if t, err := util.ReadText(filepath.Join(cwd, "requirements.txt")); err == nil {
			depsText += "\n" + t
		}
	}

	has := func(name string) bool { return strings.Contains(depsText, name) }

	// Framework
	switch {
	case has("fastapi"):
		result.Framework = "fastapi"
		result.Preset = "python-fastapi"
	case has("django"):
		result.Framework = "django"
		result.Preset = "python-django"
	case has("flask"):
		result.Framework = "flask"
		result.Preset = "python-flask"
	}

	// Test runner
	if has("pytest") {
		result.TestRunner = &ToolInfo{"pytest", "pytest"}
	} else {
		result.TestRunner = &ToolInfo{"unittest", "python -m unittest discover"}
	}

	// Linter
	switch {
	case has("ruff"):
		result.Linter = &ToolInfo{"ruff", "ruff check ."}
	case has("flake8"):
		result.Linter = &ToolInfo{"flake8", "flake8"}
	case has("pylint"):
		result.Linter = &ToolInfo{"pylint", "pylint src/"}
	}

	// Type checker
	switch {
	case has("mypy"):
		result.TypeChecker = &ToolInfo{"mypy", "mypy ."}
	case has("pyright"):
		result.TypeChecker = &ToolInfo{"pyright", "pyright"}
	}

	// Formatter
	switch {
	case has("ruff"):
		result.Formatter = &ToolInfo{"ruff", "ruff format ."}
	case has("black"):
		result.Formatter = &ToolInfo{"black", "black ."}
	}

	// Package manager
	switch {
	case util.Exists(filepath.Join(cwd, "poetry.lock")):
		result.PackageManager = "poetry"
	case util.Exists(filepath.Join(cwd, "pdm.lock")):
		result.PackageManager = "pdm"
	case util.Exists(filepath.Join(cwd, "uv.lock")):
		result.PackageManager = "uv"
	default:
		result.PackageManager = "pip"
	}

	return result
}
