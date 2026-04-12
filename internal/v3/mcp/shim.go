// Package mcp is the v3 MCP stdio shim that translates MCP tool calls
// into gRPC IndexTools RPCs via the thin router.
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gtm-k/foldermcp/internal/v3/router"
	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Shim bridges the mcp-go stdio server to the gRPC-backed Router.
type Shim struct {
	router *router.Router
	server *server.MCPServer
	conn   *grpc.ClientConn
}

// NewShim dials the local serve Unix socket and wires up the MCP stdio server.
// The gRPC connection is lazy — errors surface at first tool call, not here.
func NewShim(ctx context.Context) (*Shim, error) {
	sockPath := defaultSocketPath()

	conn, err := grpc.NewClient(
		"unix:"+sockPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc client for %s: %w", sockPath, err)
	}

	r := router.New(conn)
	s := server.NewMCPServer("foldermcp-v3", "v3.0.0-m1")

	shim := &Shim{router: r, server: s, conn: conn}
	shim.registerTools()
	return shim, nil
}

// Serve blocks on the MCP stdio transport until the client disconnects.
func (s *Shim) Serve() error {
	return server.ServeStdio(s.server)
}

// Close tears down the gRPC connection.
func (s *Shim) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func defaultSocketPath() string {
	if v := os.Getenv("FOLDERMCP_SOCKET"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".foldermcp", "run", "serve.sock")
}
