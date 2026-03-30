package util

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"strings"
	"time"
)

// RunCmd executes a command with a timeout. Returns stdout and any error.
func RunCmd(cwd string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// RunShell executes a shell command string with a timeout.
func RunShell(cwd string, timeout time.Duration, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ShellSafe strips characters that could cause shell injection.
func ShellSafe(s string) string {
	return strings.NewReplacer("`", "", "$", "", "\\", "").Replace(s)
}

// RandomHex generates n random bytes and returns them as a hex string.
func RandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WhichExists checks if a command exists in PATH.
func WhichExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
