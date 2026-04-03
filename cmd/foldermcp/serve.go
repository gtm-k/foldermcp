package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/foldermcp/foldermcp/internal/audit"
	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/server"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server on stdin/stdout",
	Long: `Starts a Model Context Protocol server that exposes all enabled tools
over the stdio transport. Only tools with state "enabled" or "requires_confirmation"
are served.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().String("mode", "dev", "server mode: dev, team, production")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	mode, _ := cmd.Flags().GetString("mode")

	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	_, err = config.Load(dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Open state store.
	stateDir := filepath.Join(dir, ".foldermcp")
	store, err := state.Open(stateDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer store.Close()

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	// Count enabled tools.
	enabledCount := 0
	for _, t := range tools {
		if t.State == "enabled" || t.State == "requires_confirmation" {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return fmt.Errorf("no enabled tools found; run 'foldermcp review' to approve tools")
	}

	// Create sandbox executor.
	executor := sandbox.NewExecutor(sandbox.ExecutorConfig{
		TimeoutSeconds: 30,
		MaxOutputBytes: 100 * 1024,
	})

	// Create sanitizer.
	sanitizer := sandbox.NewSanitizer(100 * 1024)

	// Create audit logger.
	auditPath := filepath.Join(stateDir, "audit.log")
	logger, err := audit.NewLogger(auditPath)
	if err != nil {
		return fmt.Errorf("create audit logger: %w", err)
	}
	defer logger.Close()

	// Create MCP server.
	mcpServer, err := server.NewMCPServer("foldermcp", "0.1.0", tools, &server.MCPServerConfig{
		Executor:  executor,
		Sanitizer: sanitizer,
		Logger:    logger,
	})
	if err != nil {
		return fmt.Errorf("create MCP server: %w", err)
	}

	// Print status to stderr (stdout is reserved for MCP protocol).
	fmt.Fprintf(os.Stderr, "FolderMCP server starting (mode=%s, tools=%d)\n", mode, enabledCount)

	return mcpServer.ServeStdio()
}
