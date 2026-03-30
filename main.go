package main

import (
	"embed"
	"github.com/samahlstrom/forge-cli/cmd"
	"github.com/samahlstrom/forge-cli/internal/static"
)

//go:embed all:templates
var templatesFS embed.FS

func main() {
	static.TemplatesFS = templatesFS
	cmd.Execute()
}
