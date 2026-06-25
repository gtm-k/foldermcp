//go:build !windows

package transport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
)

// Listen creates a Unix domain socket listener at socketPath with mode 0600,
// failing closed if the resulting mode differs.
func Listen(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	// Remove a stale socket left by a prior crash.
	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix: %w", err)
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("chmod: %w", err)
	}
	// Fail-closed: verify the mode is actually 0600.
	info, err := os.Stat(socketPath)
	if err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.Mode().Perm() != 0600 {
		_ = l.Close()
		return nil, fmt.Errorf("socket mode is %o, expected 0600 (fail-closed)", info.Mode().Perm())
	}
	return l, nil
}

// DialOptions returns the gRPC target and dial options for the Unix socket.
func DialOptions(socketPath string) (string, []grpc.DialOption) {
	return "unix:" + socketPath, nil
}
