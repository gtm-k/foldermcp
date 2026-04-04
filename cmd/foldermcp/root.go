package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags.
var version = "dev"

// jsonOutput controls whether commands emit JSON to stdout.
var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "foldermcp",
	Short: "Turn folders into secure MCP tool servers",
	Long: `FolderMCP scans directories containing Python, TypeScript/JavaScript, OpenAPI specs,
shell scripts, and documents (PDF, images, CSV, Markdown), then serves them as
MCP (Model Context Protocol) tools and resources. It handles discovery, introspection,
dependency management, sandboxed execution, and protocol translation so that any
folder becomes a plug-and-play AI tool server.

Supported: .py, .ts, .js, .sh, .yaml/.json (OpenAPI), .pdf, .md, .csv, .png, .jpg`,
}

func init() {
	rootCmd.Version = version
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
