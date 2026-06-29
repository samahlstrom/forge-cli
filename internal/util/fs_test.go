package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFilePreservesSourceMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "run.sh")

	if err := CopyFile(src, dst, 0); err != nil { // mode 0 → preserve source
		t.Fatalf("CopyFile: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit not preserved: mode=%v", info.Mode().Perm())
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("content not copied: %q", got)
	}
}

func TestCopyFileExplicitModeWins(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "b.txt")
	if err := CopyFile(src, dst, 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(dst)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("explicit mode not applied: got %v", info.Mode().Perm())
	}
}

func TestCopyTreePreservesExecutableBits(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "skill")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "copy")
	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	md, err := os.Stat(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if md.Mode().Perm()&0o111 != 0 {
		t.Fatalf("SKILL.md should not be executable, got %v", md.Mode().Perm())
	}
	tool, err := os.Stat(filepath.Join(dst, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if tool.Mode().Perm()&0o111 == 0 {
		t.Fatalf("bin/tool executable bit lost: %v", tool.Mode().Perm())
	}
}
