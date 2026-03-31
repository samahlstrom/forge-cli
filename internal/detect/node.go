package detect

import (
	"encoding/json"
	"github.com/samahlstrom/forge-cli/internal/util"
	"path/filepath"
)

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func hasDep(pkg packageJSON, name string) bool {
	if _, ok := pkg.Dependencies[name]; ok {
		return true
	}
	if _, ok := pkg.DevDependencies[name]; ok {
		return true
	}
	return false
}

// DetectNode detects Node.js/TypeScript projects.
func DetectNode(cwd string) *DetectedStack {
	pkgPath := filepath.Join(cwd, "package.json")
	if !util.Exists(pkgPath) {
		return nil
	}

	data, err := util.ReadText(pkgPath)
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if err := json.Unmarshal([]byte(data), &pkg); err != nil {
		return nil
	}

	lang := "javascript"
	if hasDep(pkg, "typescript") || util.Exists(filepath.Join(cwd, "tsconfig.json")) {
		lang = "typescript"
	}

	result := &DetectedStack{Language: lang}

	// Framework
	switch {
	case hasDep(pkg, "@sveltejs/kit"):
		result.Framework = "sveltekit"
		result.Preset = "sveltekit-ts"
	case hasDep(pkg, "next"):
		result.Framework = "next"
		result.Preset = "react-next-ts"
	case hasDep(pkg, "nuxt"):
		result.Framework = "nuxt"
		result.Preset = "vue-nuxt-ts"
	case hasDep(pkg, "vue"):
		result.Framework = "vue"
		result.Preset = "vue-nuxt-ts"
	case hasDep(pkg, "express"):
		result.Framework = "express"
		result.Preset = "node-express"
	case hasDep(pkg, "fastify"):
		result.Framework = "fastify"
		result.Preset = "node-express"
	case hasDep(pkg, "hono"):
		result.Framework = "hono"
		result.Preset = "react-next-ts"
	case hasDep(pkg, "drizzle-orm"):
		result.Framework = "node-api"
		result.Preset = "react-next-ts"
	case hasDep(pkg, "@trpc/server"):
		result.Framework = "node-api"
		result.Preset = "react-next-ts"
	}

	// Fallback: if we detected language + tools but no framework, still assign a preset
	if result.Preset == "" {
		if lang == "typescript" {
			result.Preset = "react-next-ts"
		} else {
			result.Preset = "react-next-ts"
		}
	}

	// Test runner
	switch {
	case hasDep(pkg, "vitest"):
		result.TestRunner = &ToolInfo{"vitest", "npx vitest run"}
	case hasDep(pkg, "jest"):
		result.TestRunner = &ToolInfo{"jest", "npx jest"}
	case hasDep(pkg, "mocha"):
		result.TestRunner = &ToolInfo{"mocha", "npx mocha"}
	}

	// Linter
	switch {
	case hasDep(pkg, "eslint"):
		result.Linter = &ToolInfo{"eslint", "npx eslint ."}
	case hasDep(pkg, "@biomejs/biome"):
		result.Linter = &ToolInfo{"biome", "npx biome check ."}
	}

	// Type checker
	if util.Exists(filepath.Join(cwd, "tsconfig.json")) {
		if result.Framework == "sveltekit" {
			result.TypeChecker = &ToolInfo{"svelte-check", "npm run check"}
		} else {
			result.TypeChecker = &ToolInfo{"tsc", "npx tsc --noEmit"}
		}
	}

	// Formatter
	switch {
	case hasDep(pkg, "prettier"):
		result.Formatter = &ToolInfo{"prettier", "npx prettier --write"}
	case hasDep(pkg, "@biomejs/biome"):
		result.Formatter = &ToolInfo{"biome", "npx biome format --write ."}
	}

	// Package manager
	switch {
	case util.Exists(filepath.Join(cwd, "pnpm-lock.yaml")):
		result.PackageManager = "pnpm"
	case util.Exists(filepath.Join(cwd, "yarn.lock")):
		result.PackageManager = "yarn"
	case util.Exists(filepath.Join(cwd, "bun.lockb")):
		result.PackageManager = "bun"
	default:
		result.PackageManager = "npm"
	}

	return result
}
