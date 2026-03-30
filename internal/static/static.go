package static

import "io/fs"

// TemplatesFS holds the embedded templates filesystem, set from main.go.
var TemplatesFS fs.FS

// Version is injected via ldflags at build time: -ldflags "-X forge-cli/internal/static.Version=0.2.0"
var Version = "0.2.0"
