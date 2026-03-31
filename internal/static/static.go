package static

import (
	"io/fs"
	"runtime/debug"
)

// TemplatesFS holds the embedded templates filesystem, set from main.go.
var TemplatesFS fs.FS

// Version is injected via ldflags at build time:
//
//	-ldflags "-X github.com/samahlstrom/forge-cli/internal/static.Version=0.2.7"
//
// When installed via `go install`, ldflags are not available, so we fall back
// to the version embedded by the Go module system in debug.ReadBuildInfo().
var Version = ""

func init() {
	if Version != "" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	} else {
		Version = "dev"
	}
}
