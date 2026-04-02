package static

import "runtime/debug"

// Version is injected via ldflags at build time:
//
//	-ldflags "-X github.com/samahlstrom/forge-cli/internal/static.Version=0.2.7"
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
