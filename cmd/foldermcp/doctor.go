package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/foldermcp/foldermcp/internal/workspace"
	"github.com/spf13/cobra"
)

var doctorFixFlag bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check environment for common issues",
	Long: `Runs a series of health checks to verify that the environment is properly
configured for FolderMCP. Pass --fix to automatically resolve fixable issues.`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFixFlag, "fix", false, "Automatically fix issues where possible")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	issues := 0
	fixed := 0

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
		_, _ = fmt.Fprintf(os.Stdout, "  ok   Python found (%s)\n", pythonName)
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "  FAIL Python not found")
		_, _ = fmt.Fprintln(os.Stdout, "       Fix: Install Python 3.10+ from https://python.org")
		issues++
	}

	// Check 2: uv available.
	if _, err := exec.LookPath("uv"); err == nil {
		_, _ = fmt.Fprintln(os.Stdout, "  ok   uv found")
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "  WARN uv not found")
		if doctorFixFlag {
			_, _ = fmt.Fprintln(os.Stdout, "       --fix: Install uv with one of:")
			_, _ = fmt.Fprintln(os.Stdout, "         pip install uv")
			_, _ = fmt.Fprintln(os.Stdout, "         curl -LsSf https://astral.sh/uv/install.sh | sh")
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "       Fix: Install uv — https://docs.astral.sh/uv/getting-started/installation/")
		}
		issues++
	}

	// Check 3: foldermcp.yaml exists.
	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	ws, wsErr := workspace.Open(dir)

	configPath := filepath.Join(dir, "foldermcp.yaml")
	if _, err := os.Stat(configPath); err == nil {
		_, _ = fmt.Fprintln(os.Stdout, "  ok   foldermcp.yaml exists")
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "  FAIL foldermcp.yaml not found")
		if doctorFixFlag {
			cfg := config.DefaultConfig()
			if saveErr := config.Save(dir, cfg); saveErr != nil {
				_, _ = fmt.Fprintf(os.Stdout, "       --fix: Failed to create config: %v\n", saveErr)
			} else {
				_, _ = fmt.Fprintln(os.Stdout, "       --fix: Created foldermcp.yaml with defaults")
				fixed++
			}
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp init' to create it")
		}
		issues++
	}

	// Check 4: Filesystem type and workspace paths.
	if wsErr == nil {
		if ws.IsNetworkFS {
			_, _ = fmt.Fprintln(os.Stdout, "  info Filesystem: network")
			_, _ = fmt.Fprintf(os.Stdout, "       State stored locally at %s\n", ws.LocalDir)
			_, _ = fmt.Fprintf(os.Stdout, "       Approvals shared at %s\n", ws.ApprovalsPath())
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "  ok   Filesystem: local")
			_, _ = fmt.Fprintf(os.Stdout, "       Workspace: %s\n", ws.LocalDir)
		}
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "  WARN Cannot determine filesystem type: %v\n", wsErr)
		issues++
	}

	// Check 5: State store accessible, count enabled tools.
	var stateDir string
	if wsErr == nil {
		stateDir = ws.LocalDir
	} else {
		stateDir = filepath.Join(dir, ".foldermcp")
	}
	store, storeErr := state.Open(stateDir)
	if storeErr == nil {
		tools, listErr := store.ListTools()
		_ = store.Close()
		if listErr == nil {
			enabledCount := 0
			for _, t := range tools {
				if t.State == "enabled" || t.State == "requires_confirmation" {
					enabledCount++
				}
			}
			if enabledCount > 0 {
				_, _ = fmt.Fprintf(os.Stdout, "  ok   State store accessible (%d enabled tools)\n", enabledCount)
			} else if len(tools) > 0 {
				_, _ = fmt.Fprintf(os.Stdout, "  WARN State store accessible (%d tools, none enabled)\n", len(tools))
				if doctorFixFlag {
					_, _ = fmt.Fprintln(os.Stdout, "       --fix: Run 'foldermcp review' to approve tools")
				} else {
					_, _ = fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp review' to approve tools")
				}
				issues++
			} else {
				_, _ = fmt.Fprintln(os.Stdout, "  WARN State store empty")
				_, _ = fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp init' to scan for tools")
				issues++
			}
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "  FAIL State store error")
			issues++
		}
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "  WARN State store not initialized")
		_, _ = fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp init'")
		issues++
	}

	// Check 6: Claude Desktop config exists.
	claudeConfigPath := claudeDesktopConfigPath()
	if claudeConfigPath != "" {
		if _, err := os.Stat(claudeConfigPath); err == nil {
			_, _ = fmt.Fprintln(os.Stdout, "  ok   Claude Desktop config found")
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "  WARN Claude Desktop config not found")
			if doctorFixFlag {
				if connectErr := connectClaudeDesktop(); connectErr != nil {
					_, _ = fmt.Fprintf(os.Stdout, "       --fix: Failed to configure Claude Desktop: %v\n", connectErr)
				} else {
					_, _ = fmt.Fprintln(os.Stdout, "       --fix: Configured Claude Desktop")
					fixed++
				}
			} else {
				_, _ = fmt.Fprintln(os.Stdout, "       Fix: Run 'foldermcp connect claude-desktop'")
			}
			issues++
		}
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "  WARN Claude Desktop config path unknown for this platform")
		issues++
	}

	_, _ = fmt.Fprintln(os.Stdout, "")
	if issues == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "All checks passed.")
	} else if doctorFixFlag && fixed > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "%d issue(s) found, %d fixed automatically.\n", issues, fixed)
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "%d issue(s) found.\n", issues)
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
