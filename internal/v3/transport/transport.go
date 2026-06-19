// Package transport provides the foldermcp daemon's local IPC transport in a
// cross-platform, cgo-free form so it can be shared by both the cgo daemon
// (server side) and the non-cgo MCP shim (client side):
//
//   - Unix/macOS: a Unix domain socket created with mode 0600 (fail-closed).
//   - Windows:    a randomly-named named pipe whose DACL grants access to the
//     current user (and LocalSystem) only; the random name is recorded in an
//     owner-only endpoint file at the rendezvous path so clients can find it.
//
// The random Windows pipe name (rather than a predictable derivation of the
// socket path) prevents a local attacker from pre-creating ("squatting") the
// pipe before the daemon and intercepting client queries.
//
// Both sides resolve the same rendezvous path via DefaultSocketPath, so the
// indexer and the clients that connect always agree without sharing state.
package transport

import (
	"os"
	"path/filepath"
)

// DefaultSocketPath returns the rendezvous path the daemon listens on and
// clients dial. On Unix it is the Unix-socket path; on Windows it is the path
// of a small owner-only endpoint file recording the named-pipe name.
// FOLDERMCP_SOCKET overrides it for BOTH server and client so they always
// agree on the same endpoint.
func DefaultSocketPath() string {
	if v := os.Getenv("FOLDERMCP_SOCKET"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".foldermcp", "run", "serve.sock")
}
