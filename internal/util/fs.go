package util

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Exists checks if a path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// WriteText writes text to a file, creating parent dirs as needed.
func WriteText(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// CopyFile copies src to dst, creating parent dirs. When mode is 0 it preserves
// src's permission bits (so an executable stays executable); otherwise mode is
// applied. The mode is forced with Chmod so it survives umask and overwrites.
func CopyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
		if info, err := os.Stat(src); err == nil {
			mode = info.Mode().Perm()
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// CopyTree recursively copies the src directory to dst, preserving each file's
// permission bits — so executables (e.g. a skill's bin/ scripts or a hook .sh)
// stay executable.
func CopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return CopyFile(path, target, info.Mode().Perm())
	})
}
