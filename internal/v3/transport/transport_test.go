package transport

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestListenDialRoundTrip proves the cross-platform transport carries a gRPC
// call end-to-end. A server with no registered services must answer an unknown
// method with codes.Unimplemented — which only happens if the connection and
// HTTP/2 handshake actually completed over the socket (Unix) or pipe (Windows).
func TestListenDialRoundTrip(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "rt.sock")

	lis, err := Listen(sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = lis.Close() }()

	srv := grpc.NewServer()
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	target, dialOpts := DialOptions(sockPath)
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, dialOpts...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = conn.Invoke(ctx, "/foldermcp.Probe/Ping", &emptypb.Empty{}, &emptypb.Empty{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("round trip: got %v (code %s), want Unimplemented", err, status.Code(err))
	}
}

// TestDefaultSocketPath verifies FOLDERMCP_SOCKET overrides the rendezvous path
// for both server and client (so they always agree).
func TestDefaultSocketPath(t *testing.T) {
	t.Setenv("FOLDERMCP_SOCKET", "/custom/serve.sock")
	if got := DefaultSocketPath(); got != "/custom/serve.sock" {
		t.Errorf("DefaultSocketPath() = %q, want /custom/serve.sock", got)
	}
}
