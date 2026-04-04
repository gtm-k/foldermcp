package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/foldermcp/foldermcp/internal/audit"
	"github.com/foldermcp/foldermcp/internal/config"
	"github.com/foldermcp/foldermcp/internal/introspect"
	"github.com/foldermcp/foldermcp/internal/sandbox"
	"github.com/foldermcp/foldermcp/internal/server"
	"github.com/foldermcp/foldermcp/internal/state"
	"github.com/foldermcp/foldermcp/internal/watcher"
	"github.com/foldermcp/foldermcp/internal/workspace"
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
	serveCmd.Flags().String("profile", "", "Only serve tools in this profile (from tool_routing.profiles in config)")
	serveCmd.Flags().Bool("watch", false, "Watch for file changes and reload tools")
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

	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}

	cfg, err := config.Load(ws.ProjectDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Open state store (SQLite always on local disk).
	store, err := state.Open(ws.LocalDir)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer func() { _ = store.Close() }()

	profile, _ := cmd.Flags().GetString("profile")

	tools, err := store.ListTools()
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	resources, err := store.ListResources()
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}

	// Filter tools by profile if specified.
	if profile != "" {
		profileTools, ok := cfg.ToolRouting.Profiles[profile]
		if !ok {
			return fmt.Errorf("profile %q not found in tool_routing.profiles", profile)
		}
		allowed := make(map[string]bool, len(profileTools))
		for _, name := range profileTools {
			allowed[name] = true
		}
		var filtered []state.Tool
		for _, t := range tools {
			if allowed[t.Name] {
				filtered = append(filtered, t)
			}
		}
		tools = filtered
		fmt.Fprintf(os.Stderr, "Profile %q: filtered to %d tools\n", profile, len(tools))
	}

	// Count enabled tools and resources.
	enabledCount := 0
	for _, t := range tools {
		if t.State == "enabled" || t.State == "requires_confirmation" {
			enabledCount++
		}
	}
	enabledResCount := 0
	for _, r := range resources {
		if r.State == "enabled" {
			enabledResCount++
		}
	}
	if enabledCount == 0 && enabledResCount == 0 {
		return fmt.Errorf("no enabled tools or resources found; run 'foldermcp review' to approve tools")
	}

	// Create sandbox executor with workspace root for path traversal validation.
	executor := sandbox.NewExecutor(sandbox.ExecutorConfig{
		TimeoutSeconds: 30,
		MaxOutputBytes: 100 * 1024,
		WorkspaceRoot:  ws.ProjectDir,
	})

	// Create sanitizer.
	sanitizer := sandbox.NewSanitizer(100 * 1024)

	// Create audit logger.
	logger, err := audit.NewLogger(ws.AuditLogPath())
	if err != nil {
		return fmt.Errorf("create audit logger: %w", err)
	}
	defer func() { _ = logger.Close() }()

	// Create MCP server.
	mcpServer, err := server.NewMCPServer("foldermcp", "0.1.0", tools, resources, store, &server.MCPServerConfig{
		Executor:  executor,
		Sanitizer: sanitizer,
		Logger:    logger,
		Transport: transport,
	})
	if err != nil {
		return fmt.Errorf("create MCP server: %w", err)
	}

	// Check for .env file and log if found.
	dotEnv, dotEnvErr := sandbox.LoadDotEnv(ws.ProjectDir)
	if dotEnvErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to read .env: %v\n", dotEnvErr)
	} else if len(dotEnv) > 0 {
		fmt.Fprintf(os.Stderr, "Loaded %d variable(s) from .env\n", len(dotEnv))
	}

	// Print status to stderr (stdout is reserved for MCP protocol).
	fmt.Fprintf(os.Stderr, "FolderMCP server starting (mode=%s, tools=%d, resources=%d)\n", mode, enabledCount, enabledResCount)
	if cfg.ToolRouting.MaxToolsPerContext > 0 {
		fmt.Fprintf(os.Stderr, "Max tools per context: %d\n", cfg.ToolRouting.MaxToolsPerContext)
	}

	// Determine transport: team mode implies HTTP unless explicitly overridden.
	useHTTP := transport == "http" || mode == "team"

	if useHTTP {
		return serveHTTP(mcpServer, ws, port)
	}

	// Start file watcher if --watch is set.
	watchEnabled, _ := cmd.Flags().GetBool("watch")
	if watchEnabled {
		interval := 2 * time.Second
		if ws.IsNetworkFS {
			interval = 5 * time.Second
		}
		fw := watcher.New(ws.ProjectDir, interval, cfg.Scan.Include, cfg.Scan.Exclude)
		events := fw.Start()
		defer fw.Stop()

		registry := introspect.NewRegistry()
		go func() {
			for ev := range events {
				if ev.Type == "deleted" {
					fmt.Fprintf(os.Stderr, "[watch] %s deleted\n", ev.Path)
					continue
				}
				tools, err := registry.ScanDirectory(context.Background(), ws.ProjectDir, cfg.Scan.Include, cfg.Scan.Exclude)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[watch] re-scan error: %v\n", err)
					continue
				}
				fmt.Fprintf(os.Stderr, "[watch] %s %s — re-scanned %d tools\n", ev.Path, ev.Type, len(tools))
			}
		}()
		fmt.Fprintf(os.Stderr, "File watching enabled (interval=%s)\n", interval)
	}

	// Set up signal handling for stdio mode.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nShutting down gracefully...\n")
		_ = os.Stdin.Close()
	}()

	return mcpServer.ServeStdio()
}

// serveHTTP starts the MCP server over Streamable HTTP with API key auth and
// self-signed TLS. The API key and TLS certs are stored in the local workspace
// directory so they never end up on a network share.
func serveHTTP(mcpServer *server.MCPServer, ws *workspace.Workspace, port int) error {
	// Load or generate API key.
	apiKey, err := loadOrGenerateAPIKey(ws.APIKeyPath())
	if err != nil {
		return fmt.Errorf("api key: %w", err)
	}

	// Ensure TLS directory exists.
	if err := os.MkdirAll(ws.TLSDir(), 0700); err != nil {
		return fmt.Errorf("create TLS dir: %w", err)
	}

	// Generate self-signed TLS certificate.
	certFile, keyFile, err := server.GenerateSelfSignedCert(ws.TLSDir())
	if err != nil {
		return fmt.Errorf("generate TLS cert: %w", err)
	}

	addr := fmt.Sprintf(":%d", port)

	// Mask the API key: show prefix (first 7 chars) and suffix (last 4 chars),
	// replace the middle with asterisks.
	maskedKey := apiKey
	if len(apiKey) > 11 {
		maskedKey = apiKey[:7] + strings.Repeat("*", len(apiKey)-11) + apiKey[len(apiKey)-4:]
	}

	fmt.Fprintf(os.Stderr, "\n--- FolderMCP Team Server ---\n")
	fmt.Fprintf(os.Stderr, "URL:     https://localhost%s/mcp\n", addr)
	fmt.Fprintf(os.Stderr, "API Key: %s\n", maskedKey)
	fmt.Fprintf(os.Stderr, "TLS:     self-signed (cert=%s)\n", certFile)
	fmt.Fprintf(os.Stderr, "-----------------------------\n\n")

	// Set up signal handling for graceful HTTP shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- mcpServer.ServeHTTP(addr, &server.HTTPConfig{
			APIKey:   apiKey,
			CertFile: certFile,
			KeyFile:  keyFile,
		})
	}()

	select {
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "\nReceived %v, shutting down gracefully...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return mcpServer.ShutdownHTTP(ctx)
	case err := <-errCh:
		return err
	}
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
