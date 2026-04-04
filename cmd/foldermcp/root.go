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
	Long: `FolderMCP scans directories containing Python scripts and OpenAPI specifications,
then serves them as MCP (Model Context Protocol) tools. It handles discovery,
introspection, dependency management, sandboxed execution, and protocol translation
so that any folder of scripts becomes a plug-and-play AI tool server.`,
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
