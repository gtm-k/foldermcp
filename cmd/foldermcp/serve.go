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

const defaultHTTPPort = 3000

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
	serveCmd.Flags().Int("port", defaultHTTPPort, "HTTP listen port (used in team/production mode)")
	serveCmd.Flags().String("transport", "stdio", "transport protocol: stdio or http")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	mode, _ := cmd.Flags().GetString("mode")
	port, _ := cmd.Flags().GetInt("port")
	transport, _ := cmd.Flags().GetString("transport")

	dir, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	cfg, err := config.Load(dir)
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
	if cfg.ToolRouting.MaxToolsPerContext > 0 {
		fmt.Fprintf(os.Stderr, "Max tools per context: %d\n", cfg.ToolRouting.MaxToolsPerContext)
	}

	// Determine transport: team mode implies HTTP unless explicitly overridden.
	useHTTP := transport == "http" || mode == "team"

	if useHTTP {
		return serveHTTP(mcpServer, stateDir, port)
	}

	return mcpServer.ServeStdio()
}

// serveHTTP starts the MCP server over Streamable HTTP with API key auth and
// self-signed TLS. The API key is persisted to .foldermcp/api.key so it
// survives restarts.
func serveHTTP(mcpServer *server.MCPServer, stateDir string, port int) error {
	// Ensure state directory exists.
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Load or generate API key.
	apiKeyPath := filepath.Join(stateDir, "api.key")
	apiKey, err := loadOrGenerateAPIKey(apiKeyPath)
	if err != nil {
		return fmt.Errorf("api key: %w", err)
	}

	// Generate self-signed TLS certificate.
	certFile, keyFile, err := server.GenerateSelfSignedCert(stateDir)
	if err != nil {
		return fmt.Errorf("generate TLS cert: %w", err)
	}

	addr := fmt.Sprintf(":%d", port)

	fmt.Fprintf(os.Stderr, "\n--- FolderMCP Team Server ---\n")
	fmt.Fprintf(os.Stderr, "URL:     https://localhost%s/mcp\n", addr)
	fmt.Fprintf(os.Stderr, "API Key: %s\n", apiKey)
	fmt.Fprintf(os.Stderr, "TLS:     self-signed (cert=%s)\n", certFile)
	fmt.Fprintf(os.Stderr, "-----------------------------\n\n")

	return mcpServer.ServeHTTP(addr, &server.HTTPConfig{
		APIKey:   apiKey,
		CertFile: certFile,
		KeyFile:  keyFile,
	})
}

// loadOrGenerateAPIKey reads an API key from path, or generates a new one
// and writes it to the file.
func loadOrGenerateAPIKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key := string(data)
		if len(key) > 0 {
			return key, nil
		}
	}

	key := server.GenerateAPIKey()
	if err := os.WriteFile(path, []byte(key), 0600); err != nil {
		return "", fmt.Errorf("write api key: %w", err)
	}
	return key, nil
}
