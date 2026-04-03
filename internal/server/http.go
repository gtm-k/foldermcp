package server

import (
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

	// If API key auth is configured, wrap the handler with middleware.
	if cfg.APIKey != "" {
		auth := NewAPIKeyAuth(cfg.APIKey)

		mux := http.NewServeMux()
		mux.Handle("/mcp", auth.Middleware(httpServer))

		customHTTP := &http.Server{
			Addr:    addr,
			Handler: mux,
		}

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

	scheme := "http"
	if cfg.CertFile != "" {
		scheme = "https"
	}
	fmt.Fprintf(os.Stderr, "FolderMCP HTTP server listening on %s://localhost%s/mcp\n", scheme, addr)

	return httpServer.Start(addr)
}
