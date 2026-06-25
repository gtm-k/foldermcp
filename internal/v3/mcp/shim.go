// Package mcp is the v3 MCP stdio shim that translates MCP tool calls
// into gRPC IndexTools RPCs via the thin router.
package mcp

import (
	"context"
	"fmt"

	"github.com/gtm-k/foldermcp/internal/v3/router"
	"github.com/gtm-k/foldermcp/internal/v3/transport"
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
	sockPath := transport.DefaultSocketPath()

	// transport.DialOptions yields a Unix-socket target on Unix/macOS and a
	// named-pipe context dialer on Windows, so the shim connects the same way
	// the daemon listens on each platform.
	target, dialOpts := transport.DialOptions(sockPath)
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, dialOpts...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc client for %s: %w", sockPath, err)
	}

	r := router.New(conn)
	s := server.NewMCPServer("foldermcp-v3", "v3.0.0-m1",
		server.WithRecovery(),
	)

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
