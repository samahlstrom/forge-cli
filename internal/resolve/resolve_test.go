package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForgeHome_Default(t *testing.T) {
	os.Unsetenv("FORGE_HOME")
	home, _ := os.UserHomeDir()
	got := ForgeHome()
	want := filepath.Join(home, ".forge")
	if got != want {
		t.Errorf("ForgeHome() = %q, want %q", got, want)
	}
}

func TestForgeHome_EnvOverride(t *testing.T) {
	t.Setenv("FORGE_HOME", "/tmp/test-forge-home")
	got := ForgeHome()
	if got != "/tmp/test-forge-home" {
		t.Errorf("ForgeHome() = %q, want /tmp/test-forge-home", got)
	}
}

func TestResolveFile_GlobalOnly(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("FORGE_HOME", globalDir)
	cwd := t.TempDir()

	// Create global file
	os.MkdirAll(filepath.Join(globalDir, "agents"), 0o755)
	os.WriteFile(filepath.Join(globalDir, "agents", "architect.md"), []byte("global"), 0o644)

	got := ResolveFile(cwd, "agents/architect.md")
	want := filepath.Join(globalDir, "agents", "architect.md")
	if got != want {
		t.Errorf("ResolveFile() = %q, want %q", got, want)
	}
}

func TestResolveFile_LocalOnly(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("FORGE_HOME", globalDir)
	cwd := t.TempDir()

	// Create local file
	os.MkdirAll(filepath.Join(cwd, ".forge", "agents"), 0o755)
	os.WriteFile(filepath.Join(cwd, ".forge", "agents", "architect.md"), []byte("local"), 0o644)

	got := ResolveFile(cwd, "agents/architect.md")
	want := filepath.Join(cwd, ".forge", "agents", "architect.md")
	if got != want {
		t.Errorf("ResolveFile() = %q, want %q", got, want)
	}
}

func TestResolveFile_LocalOverridesGlobal(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("FORGE_HOME", globalDir)
	cwd := t.TempDir()

	// Create both
	os.MkdirAll(filepath.Join(globalDir, "agents"), 0o755)
	os.WriteFile(filepath.Join(globalDir, "agents", "architect.md"), []byte("global"), 0o644)
	os.MkdirAll(filepath.Join(cwd, ".forge", "agents"), 0o755)
	os.WriteFile(filepath.Join(cwd, ".forge", "agents", "architect.md"), []byte("local"), 0o644)

	got := ResolveFile(cwd, "agents/architect.md")
	want := filepath.Join(cwd, ".forge", "agents", "architect.md")
	if got != want {
		t.Errorf("ResolveFile() should prefer local, got %q, want %q", got, want)
	}
}

func TestResolveFile_NeitherExists(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("FORGE_HOME", globalDir)
	cwd := t.TempDir()

	got := ResolveFile(cwd, "agents/nonexistent.md")
	if got != "" {
		t.Errorf("ResolveFile() should return empty for missing file, got %q", got)
	}
}

func TestResolveAgent(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("FORGE_HOME", globalDir)
	cwd := t.TempDir()

	os.MkdirAll(filepath.Join(globalDir, "agents"), 0o755)
	os.WriteFile(filepath.Join(globalDir, "agents", "frontend.md"), []byte("agent"), 0o644)

	got := ResolveAgent(cwd, "frontend")
	want := filepath.Join(globalDir, "agents", "frontend.md")
	if got != want {
		t.Errorf("ResolveAgent() = %q, want %q", got, want)
	}
}

func TestListAgents_MergesGlobalAndLocal(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("FORGE_HOME", globalDir)
	cwd := t.TempDir()

	// Global agents
	os.MkdirAll(filepath.Join(globalDir, "agents"), 0o755)
	os.WriteFile(filepath.Join(globalDir, "agents", "architect.md"), []byte("global"), 0o644)
	os.WriteFile(filepath.Join(globalDir, "agents", "security.md"), []byte("global"), 0o644)

	// Local override for architect + new local agent
	os.MkdirAll(filepath.Join(cwd, ".forge", "agents"), 0o755)
	os.WriteFile(filepath.Join(cwd, ".forge", "agents", "architect.md"), []byte("local"), 0o644)
	os.WriteFile(filepath.Join(cwd, ".forge", "agents", "custom.md"), []byte("local"), 0o644)

	agents := ListAgents(cwd)

	byName := make(map[string]AgentInfo)
	for _, a := range agents {
		byName[a.Name] = a
	}

	if len(agents) != 3 {
		t.Errorf("ListAgents() returned %d agents, want 3", len(agents))
	}

	// architect should be local override
	if a, ok := byName["architect"]; !ok || a.Global {
		t.Error("architect should be local override")
	}

	// security should be global
	if a, ok := byName["security"]; !ok || !a.Global {
		t.Error("security should be global")
	}

	// custom should be local
	if a, ok := byName["custom"]; !ok || a.Global {
		t.Error("custom should be local")
	}
}

func TestIsGlobalSetup(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("FORGE_HOME", globalDir)

	if IsGlobalSetup() {
		t.Error("IsGlobalSetup() should be false before agents/ exists")
	}

	os.MkdirAll(filepath.Join(globalDir, "agents"), 0o755)
	if !IsGlobalSetup() {
		t.Error("IsGlobalSetup() should be true after agents/ created")
	}
}
