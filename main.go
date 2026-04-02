package main

import (
	"embed"

	"github.com/samahlstrom/forge-cli/cmd"
)

//go:embed library/*
var starterContent embed.FS

func main() {
	cmd.StarterContent = starterContent
	cmd.Execute()
}
