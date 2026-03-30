package detect

import (
	"github.com/samahlstrom/forge-cli/internal/util"
	"path/filepath"
	"strings"
)

// DetectGo detects Go projects.
func DetectGo(cwd string) *DetectedStack {
	goModPath := filepath.Join(cwd, "go.mod")
	if !util.Exists(goModPath) {
		return nil
	}

	goMod, err := util.ReadText(goModPath)
	if err != nil {
		return nil
	}

	result := &DetectedStack{
		Language:       "go",
		Preset:         "go",
		TestRunner:     &ToolInfo{"go test", "go test ./..."},
		TypeChecker:    &ToolInfo{"go vet", "go vet ./..."},
		Formatter:      &ToolInfo{"gofmt", "gofmt -w ."},
		PackageManager: "go modules",
	}

	// Framework
	switch {
	case strings.Contains(goMod, "github.com/gin-gonic/gin"):
		result.Framework = "gin"
	case strings.Contains(goMod, "github.com/go-chi/chi"):
		result.Framework = "chi"
	case strings.Contains(goMod, "github.com/gofiber/fiber"):
		result.Framework = "fiber"
	case strings.Contains(goMod, "github.com/labstack/echo"):
		result.Framework = "echo"
	}

	// Linter
	if util.Exists(filepath.Join(cwd, ".golangci.yml")) || util.Exists(filepath.Join(cwd, ".golangci.yaml")) {
		result.Linter = &ToolInfo{"golangci-lint", "golangci-lint run"}
	}

	return result
}
