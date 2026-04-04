package server

import (
	"context"
	"fmt"
	"net/http"
	"os"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

// HTTPConfig holds configuration for the HTTP transport.
type HTTPConfig struct {
	// APIKey, when non-empty, enables Bearer token authentication.
	APIKey string
	// CertFile and KeyFile, when both non-empty, enable TLS.
	CertFile string
	KeyFile  string
}

// ServeHTTP starts a Streamable HTTP MCP server on addr (e.g. ":3000").
// If cfg.APIKey is set, requests must include a valid Authorization header.
// If cfg.CertFile and cfg.KeyFile are set, the server uses TLS.
func (ms *MCPServer) ServeHTTP(addr string, cfg *HTTPConfig) error {
	if cfg == nil {
		cfg = &HTTPConfig{}
	}

	var opts []mcpserver.StreamableHTTPOption

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		opts = append(opts, mcpserver.WithTLSCert(cfg.CertFile, cfg.KeyFile))
	}

	httpServer := mcpserver.NewStreamableHTTPServer(ms.server, opts...)

	mux := http.NewServeMux()

	// MCP endpoint — wrap with auth middleware if configured.
	if cfg.APIKey != "" {
		auth := NewAPIKeyAuth(cfg.APIKey)
		mux.Handle("/mcp", auth.Middleware(httpServer))
	} else {
		mux.Handle("/mcp", httpServer)
	}

	// Health endpoint — always returns OK if the process is running.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Readiness endpoint — reports the number of enabled tools.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ready","tools":%d}`, ms.enabledToolCount)
	})

	// Prometheus metrics stub endpoint.
	totalTools := len(ms.tools)
	enabledTools := ms.enabledToolCount
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# HELP foldermcp_tools_total Total number of tools\n")
		fmt.Fprintf(w, "# TYPE foldermcp_tools_total gauge\n")
		fmt.Fprintf(w, "foldermcp_tools_total %d\n", totalTools)
		fmt.Fprintf(w, "# HELP foldermcp_tools_enabled Number of enabled tools\n")
		fmt.Fprintf(w, "# TYPE foldermcp_tools_enabled gauge\n")
		fmt.Fprintf(w, "foldermcp_tools_enabled %d\n", enabledTools)
	})

	customHTTP := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	ms.httpServer = customHTTP

	scheme := "http"
	if cfg.CertFile != "" {
		scheme = "https"
	}
	fmt.Fprintf(os.Stderr, "FolderMCP HTTP server listening on %s://localhost%s/mcp\n", scheme, addr)

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		return customHTTP.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
	}
	return customHTTP.ListenAndServe()
}

// ShutdownHTTP gracefully shuts down the HTTP server.
func (ms *MCPServer) ShutdownHTTP(ctx context.Context) error {
	if ms.httpServer != nil {
		return ms.httpServer.Shutdown(ctx)
	}
	return nil
}
