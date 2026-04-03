package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <client>",
	Short: "Configure a client to use this FolderMCP server",
	Long: `Adds this FolderMCP workspace as an MCP server in the target client's
configuration. Currently supports: claude-desktop.`,
	Args: cobra.ExactArgs(1),
	RunE: runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
	client := args[0]

	switch client {
	case "claude-desktop":
		return connectClaudeDesktop()
	default:
		return fmt.Errorf("unsupported client %q; supported: claude-desktop", client)
	}
}

func connectClaudeDesktop() error {
	configPath := claudeDesktopConfigPath()
	if configPath == "" {
		return fmt.Errorf("could not determine Claude Desktop config path for this platform")
	}

	// Read existing config or start with empty object.
	var configMap map[string]interface{}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read Claude Desktop config: %w", err)
		}
		configMap = make(map[string]interface{})
	} else {
		if err := json.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("parse Claude Desktop config: %w", err)
		}
	}

	// Find foldermcp binary path.
	binaryPath, err := exec.LookPath("foldermcp")
	if err != nil {
		// Fall back to the current executable.
		binaryPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("determine foldermcp binary path: %w", err)
		}
	}

	// Get current working directory for cwd.
	cwd, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}

	// Build the mcpServers entry.
	serverEntry := map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve", "--mode=dev"},
		"cwd":     cwd,
	}

	// Ensure mcpServers key exists.
	mcpServers, ok := configMap["mcpServers"].(map[string]interface{})
	if !ok {
		mcpServers = make(map[string]interface{})
	}
	mcpServers["foldermcp"] = serverEntry
	configMap["mcpServers"] = mcpServers

	// Write config back.
	output, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return fmt.Errorf("write Claude Desktop config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Updated %s\n", configPath)
	fmt.Fprintln(os.Stderr, "Added 'foldermcp' to mcpServers.")
	fmt.Fprintln(os.Stderr, "Restart Claude Desktop to pick up the change.")

	return nil
}
