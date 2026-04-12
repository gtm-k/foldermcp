//go:build cgo && !windows

package grpc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// ListenUnixSocket creates a listener at socketPath, sets mode 0600,
// and fails closed if the resulting mode differs.
func ListenUnixSocket(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	// Remove stale socket from a prior crash
	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix: %w", err)
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("chmod: %w", err)
	}
	// Fail-closed: verify the mode is actually 0600
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
