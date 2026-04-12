//go:build cgo && windows

package grpc

import (
	"fmt"
	"net"
)

// ListenUnixSocket on Windows returns an error — M1 uses Unix domain
// sockets only. Named-pipe transport is M2 work per the scope fence.
func ListenUnixSocket(socketPath string) (net.Listener, error) {
	return nil, fmt.Errorf("named pipe listener: not implemented in M1 (Windows indexer is best-effort)")
}
