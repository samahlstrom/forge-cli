package main

import (
	"io/fs"
	"testing"
)

// TestEmbeddedLibraryShipsNoPersonalContent locks in the pure-engine contract:
// the embedded library/ carries only the .gitkeep placeholder, so a fresh
// `forge setup` seeds no skills, agents, or hooks. Personal content lives in the
// user's toolkit (~/.forge), brought in via the CLI — never shipped by the engine.
func TestEmbeddedLibraryShipsNoPersonalContent(t *testing.T) {
	var shipped []string
	err := fs.WalkDir(starterContent, "library", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == ".gitkeep" {
			return nil
		}
		shipped = append(shipped, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded library must not error: %v", err)
	}
	if len(shipped) > 0 {
		t.Fatalf("engine embed must ship only a placeholder, found personal content: %v", shipped)
	}
}
