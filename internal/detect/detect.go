package detect

// DetectedStack holds everything we learned about the project.
type DetectedStack struct {
	Language       string      `json:"language"` // typescript, javascript, python, go, unknown
	Framework      string      `json:"framework,omitempty"`
	Preset         string      `json:"preset,omitempty"`
	TestRunner     *ToolInfo   `json:"test_runner,omitempty"`
	Linter         *ToolInfo   `json:"linter,omitempty"`
	TypeChecker    *ToolInfo   `json:"type_checker,omitempty"`
	Formatter      *ToolInfo   `json:"formatter,omitempty"`
	PackageManager string      `json:"package_manager,omitempty"`
	Features       FeatureFlags `json:"features"`
}

// ToolInfo describes a detected tool.
type ToolInfo struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Detect scans cwd and returns detected stack information.
func Detect(cwd string) DetectedStack {
	features := DetectFeatures(cwd)

	if result := DetectNode(cwd); result != nil {
		result.Features = features
		return *result
	}
	if result := DetectPython(cwd); result != nil {
		result.Features = features
		return *result
	}
	if result := DetectGo(cwd); result != nil {
		result.Features = features
		return *result
	}

	return DetectedStack{
		Language: "unknown",
		Features: features,
	}
}
