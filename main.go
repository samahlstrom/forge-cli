package main

import (
	"embed"

	"github.com/samahlstrom/forge-cli/cmd"
)

// The engine ships an empty library/ (only a .gitkeep placeholder) — personal
// skills, agents, and hooks live in the user's toolkit (~/.forge), not here. The
// all: prefix is required so the dotfile placeholder satisfies the embed.
//
//go:embed all:library
var starterContent embed.FS

func main() {
	cmd.StarterContent = starterContent
	cmd.Execute()
}
