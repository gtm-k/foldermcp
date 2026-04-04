package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var connectMode string

var connectCmd = &cobra.Command{
	Use:   "connect <client>",
	Short: "Configure a client to use this FolderMCP server",
	Long: `Adds this FolderMCP workspace as an MCP server in the target client's
configuration. Currently supports: claude-desktop, claude-code, cursor.`,
	Args: cobra.ExactArgs(1),
	RunE: runConnect,
}

func init() {
	connectCmd.Flags().StringVar(&connectMode, "mode", "dev", "Server mode to use (dev, team, production)")
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
	client := args[0]

	switch client {
	case "claude-desktop":
		return connectClaudeDesktop()
	case "claude-code":
		return connectClaudeCode()
	case "cursor":
		return connectCursor()
	default:
		return fmt.Errorf("unsupported client %q; supported: claude-desktop, claude-code, cursor", client)
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
		"args":    []string{"serve", "--mode=" + connectMode},
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

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, output, 0644); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("renaming config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Updated %s\n", configPath)
	fmt.Fprintln(os.Stderr, "Added 'foldermcp' to mcpServers.")
	fmt.Fprintln(os.Stderr, "Restart Claude Desktop to pick up the change.")

	return nil
}

func connectClaudeCode() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	configPath := filepath.Join(home, ".claude.json")

	// Read existing config or start with empty object.
	var configMap map[string]interface{}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read Claude Code config: %w", err)
		}
		configMap = make(map[string]interface{})
	} else {
		if err := json.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("parse Claude Code config: %w", err)
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

	// Build the mcpServers entry.
	serverEntry := map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve", "--mode=" + connectMode},
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

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, output, 0644); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("renaming config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Updated %s\n", configPath)
	fmt.Fprintln(os.Stderr, "Added 'foldermcp' to mcpServers.")
	fmt.Fprintln(os.Stderr, "Claude Code will pick up the change automatically.")

	return nil
}

func connectCursor() error {
	configPath := cursorConfigPath()
	if configPath == "" {
		return fmt.Errorf("could not determine Cursor config path for this platform")
	}

	// Read existing config or start with empty object.
	var configMap map[string]interface{}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read Cursor config: %w", err)
		}
		configMap = make(map[string]interface{})
	} else {
		if err := json.Unmarshal(data, &configMap); err != nil {
			return fmt.Errorf("parse Cursor config: %w", err)
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

	// Build the mcpServers entry. Cursor uses a project-based key.
	cwd, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	projectName := filepath.Base(cwd)
	serverKey := "foldermcp-" + projectName

	serverEntry := map[string]interface{}{
		"command": binaryPath,
		"args":    []string{"serve", "--mode=" + connectMode},
	}

	// Ensure mcpServers key exists.
	mcpServers, ok := configMap["mcpServers"].(map[string]interface{})
	if !ok {
		mcpServers = make(map[string]interface{})
	}
	mcpServers[serverKey] = serverEntry
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

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, output, 0644); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("renaming config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Updated %s\n", configPath)
	fmt.Fprintf(os.Stderr, "Added %q to mcpServers.\n", serverKey)
	fmt.Fprintln(os.Stderr, "Restart Cursor to pick up the change.")

	return nil
}

// cursorConfigPath returns the platform-specific path to Cursor's MCP
// configuration file, or empty string if the platform is unknown.
func cursorConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cursor", "mcp.json")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return ""
		}
		return filepath.Join(appData, "Cursor", "mcp.json")
	case "linux":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "cursor", "mcp.json")
	default:
		return ""
	}
}
