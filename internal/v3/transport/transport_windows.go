//go:build windows

package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/Microsoft/go-winio"
	"google.golang.org/grpc"
)

// Listen creates a randomly-named named-pipe listener restricted to the current
// user (plus LocalSystem) and records the pipe name in an owner-only endpoint
// file at socketPath so clients can find it.
//
// The random name (vs. a predictable hash of socketPath) prevents a local
// attacker from pre-creating — "squatting" — the pipe before the daemon and
// intercepting client queries: the attacker cannot guess a 128-bit random name
// to create it first. The owner-only SDDL additionally prevents other users
// from adding instances to the daemon's pipe. Together these give the Windows
// pipe the same single-trust-boundary posture as the Unix 0600 socket.
func Listen(socketPath string) (net.Listener, error) {
	sddl, err := ownerOnlySDDL()
	if err != nil {
		return nil, err
	}
	name, err := randomPipeName()
	if err != nil {
		return nil, err
	}
	l, err := winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: sddl})
	if err != nil {
		return nil, fmt.Errorf("listen pipe: %w", err)
	}
	if err := writeEndpoint(socketPath, name); err != nil {
		_ = l.Close()
		return nil, err
	}
	return &endpointListener{Listener: l, endpointPath: socketPath}, nil
}

// DialOptions returns a gRPC target and a context dialer that reads the endpoint
// file at dial time (so a client started before the daemon retries cleanly once
// the daemon writes it) and connects the recorded named pipe.
func DialOptions(socketPath string) (string, []grpc.DialOption) {
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		name, err := readEndpoint(socketPath)
		if err != nil {
			return nil, fmt.Errorf("read endpoint %s: %w", socketPath, err)
		}
		return winio.DialPipeContext(ctx, name)
	}
	// passthrough scheme: skip name resolution; the context dialer connects.
	return "passthrough:///foldermcp", []grpc.DialOption{grpc.WithContextDialer(dialer)}
}

// endpointListener removes the endpoint file when the listener is closed so a
// stale file never points clients at a dead pipe after a clean shutdown.
type endpointListener struct {
	net.Listener
	endpointPath string
}

func (e *endpointListener) Close() error {
	_ = os.Remove(e.endpointPath)
	return e.Listener.Close()
}

// randomPipeName returns an unguessable named-pipe path.
func randomPipeName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("random pipe name: %w", err)
	}
	return `\\.\pipe\foldermcp-` + hex.EncodeToString(b[:]), nil
}

// writeEndpoint records the pipe name at socketPath (under the user-profile run
// dir) with owner-only permission.
//
// SECURITY: the endpoint file is a rendezvous convenience, NOT the security
// anchor. Confidentiality of the file is not relied upon — even if another user
// reads the random pipe name, the pipe was already created with an owner-only
// SDDL, so they cannot add an instance or intercept. The guarantee against
// cross-user squatting is (random name ⇒ cannot pre-create) + (owner-only SDDL
// ⇒ cannot add instances). A SAME-user process can overwrite this file or bind
// its own pipe — but same-user is the trust boundary by design, identical to a
// same-user process replacing the Unix 0600 socket. A stale file from a crashed
// daemon makes the next dial fail closed (dead pipe) and is overwritten on the
// next Listen.
func writeEndpoint(socketPath, pipeName string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return fmt.Errorf("mkdir run dir: %w", err)
	}
	if err := os.WriteFile(socketPath, []byte(pipeName), 0600); err != nil {
		return fmt.Errorf("write endpoint: %w", err)
	}
	return nil
}

// readEndpoint reads the pipe name recorded by Listen and validates its shape.
func readEndpoint(socketPath string) (string, error) {
	data, err := os.ReadFile(socketPath)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(data))
	if !strings.HasPrefix(name, `\\.\pipe\`) {
		return "", fmt.Errorf("invalid endpoint contents in %s", socketPath)
	}
	return name, nil
}

// ownerOnlySDDL builds an SDDL granting GENERIC_ALL to the current user (whose
// Uid is the account SID on Windows) and LocalSystem (SY) only, with a
// protected DACL (P) so no inherited ACEs widen access. This is "owner plus
// LocalSystem", not literally a 0600 single-principal socket: OS services
// running as LocalSystem can also reach the pipe. SY is retained because the
// daemon may legitimately be managed by a LocalSystem service; drop it if
// strict single-principal access is required.
func ownerOnlySDDL() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("current user: %w", err)
	}
	if !strings.HasPrefix(u.Uid, "S-") {
		return "", fmt.Errorf("current user Uid %q is not a Windows SID", u.Uid)
	}
	return fmt.Sprintf("D:P(A;;GA;;;%s)(A;;GA;;;SY)", u.Uid), nil
}
