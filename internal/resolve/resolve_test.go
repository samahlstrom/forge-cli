package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolkitManifestPath(t *testing.T) {
	t.Run("reads uppercase AGENTS.md", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("FORGE_HOME", home)
		if err := os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte("UPPER\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(ToolkitManifestPath())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != "UPPER\n" {
			t.Fatalf("want UPPER content, got %q", got)
		}
	})

	t.Run("falls back to legacy lowercase agents.md", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("FORGE_HOME", home)
		if err := os.WriteFile(filepath.Join(home, "agents.md"), []byte("lower\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(ToolkitManifestPath())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != "lower\n" {
			t.Fatalf("want lower content, got %q", got)
		}
	})

	t.Run("defaults to AGENTS.md path when neither exists", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("FORGE_HOME", home)
		if base := filepath.Base(ToolkitManifestPath()); base != "AGENTS.md" {
			t.Fatalf("want AGENTS.md default, got %q", base)
		}
	})

	t.Run("prefers uppercase when both exist (case-sensitive FS only)", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("FORGE_HOME", home)
		if err := os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte("UPPER\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// On a case-insensitive filesystem (macOS default) AGENTS.md and
		// agents.md are the same inode, so "both exist" can't be built — skip.
		if _, err := os.Stat(filepath.Join(home, "agents.md")); err == nil {
			t.Skip("case-insensitive filesystem: AGENTS.md and agents.md are the same file")
		}
		if err := os.WriteFile(filepath.Join(home, "agents.md"), []byte("lower\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(ToolkitManifestPath())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != "UPPER\n" {
			t.Fatalf("want UPPER preferred, got %q", got)
		}
	})
}
