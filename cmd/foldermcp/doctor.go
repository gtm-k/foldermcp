package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check environment for common issues",
	Long:  `Runs a series of health checks to verify that the environment is properly configured for FolderMCP.`,
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	issues := 0

	// Check 1: Python available.
	pythonOk := false
	pythonName := ""
	for _, name := range []string{"python3", "python"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		// Verify the binary runs (avoids Windows App Alias shims).
		c := exec.Command(p, "--version")
		if err := c.Run(); err == nil {
			pythonOk = true
			pythonName = name
			break
		}
	}
	if pythonOk {
		fmt.Fprintf(os.Stdout, "  ok   Python found (%s)\n", pythonName)
	} else {
		fmt.Fprintln(os.Stdout, "  FAIL Python not found")
		fmt.Fprintln(os.Stdout, "       Fix: Install Python 3.10+ from https://python.org")
		issues++
	}

	// Check 2: uv available.
	if _, err := exec.LookPath("uv"); err == nil {
		fmt.Fprintln(os.Stdout, "  ok   uv found")
	} else {
		fmt.Fprintln(os.Stdout, "  WARN uv not found")
		fmt.Fprintln(os.Stdout, "       Fix: Install uv — https://docs.astral.sh/uv/getting-started/installation/")
		issues++
	}

	// Check 3: foldermcp.yaml exists.
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	configPath := filepath.Join(dir, "foldermcp.yaml")
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintln(os.Stdout, "  ok   foldermcp.yaml exists")
	} else {
		fmt.Fprintln(os.Stdout, "  FAIL foldermcp.yaml not found")
		fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp init' to create it")
		issues++
	}

	// Check 4: State store accessible, count enabled tools.
	stateDir := filepath.Join(dir, ".foldermcp")
	store, storeErr := state.Open(stateDir)
	if storeErr == nil {
		tools, listErr := store.ListTools()
		store.Close()
		if listErr == nil {
			enabledCount := 0
			for _, t := range tools {
				if t.State == "enabled" || t.State == "requires_confirmation" {
					enabledCount++
				}
			}
			if enabledCount > 0 {
				fmt.Fprintf(os.Stdout, "  ok   State store accessible (%d enabled tools)\n", enabledCount)
			} else if len(tools) > 0 {
				fmt.Fprintf(os.Stdout, "  WARN State store accessible (%d tools, none enabled)\n", len(tools))
				fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp review' to approve tools")
				issues++
			} else {
				fmt.Fprintln(os.Stdout, "  WARN State store empty")
				fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp init' to scan for tools")
				issues++
			}
		} else {
			fmt.Fprintln(os.Stdout, "  FAIL State store error")
			issues++
		}
	} else {
		fmt.Fprintln(os.Stdout, "  WARN State store not initialized")
		fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp init'")
		issues++
	}

	// Check 5: Claude Desktop config exists.
	claudeConfigPath := claudeDesktopConfigPath()
	if claudeConfigPath != "" {
		if _, err := os.Stat(claudeConfigPath); err == nil {
			fmt.Fprintln(os.Stdout, "  ok   Claude Desktop config found")
		} else {
			fmt.Fprintln(os.Stdout, "  WARN Claude Desktop config not found")
			fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp connect claude-desktop'")
			issues++
		}
	} else {
		fmt.Fprintln(os.Stdout, "  WARN Claude Desktop config path unknown for this platform")
		issues++
	}

	fmt.Fprintln(os.Stdout, "")
	if issues == 0 {
		fmt.Fprintln(os.Stdout, "All checks passed.")
	} else {
		fmt.Fprintf(os.Stdout, "%d issue(s) found.\n", issues)
	}

	return nil
}

// claudeDesktopConfigPath returns the platform-specific path to Claude Desktop's
// configuration file, or empty string if the platform is unknown.
func claudeDesktopConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return ""
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json")
	case "linux":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
	default:
		return ""
	}
}
