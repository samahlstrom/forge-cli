package cmd

import (
	"os"
	"path/filepath"
	"strings"
)

// isForgeSelfRepo returns true if cwd is the forge-cli source repository.
// Detects by checking if go.mod declares the forge-cli module.
func isForgeSelfRepo(cwd string) bool {
	data, err := os.ReadFile(filepath.Join(cwd, "go.mod"))
	if err != nil {
		return false
	}
	firstLine := strings.SplitN(string(data), "\n", 2)[0]
	return strings.Contains(firstLine, "forge-cli")
}
