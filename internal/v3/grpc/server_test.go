//go:build cgo

package grpc

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestServerBootsAndAnswersCapabilities(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "s.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(ServerOpts{DB: db})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, lis)
	}()
	time.Sleep(200 * time.Millisecond)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewIndexCapabilitiesClient(conn)
	resp, err := client.GetCapabilities(ctx, &pb.GetCapabilitiesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SchemaVersion != 3 {
		t.Errorf("schema_version = %d, want 3", resp.SchemaVersion)
	}
	if resp.WireVersion != 1 {
		t.Errorf("wire_version = %d", resp.WireVersion)
	}
}

func TestServerHealthOnFreshDB(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "h.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(ServerOpts{DB: db})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.Serve(ctx, lis)
	}()
	time.Sleep(200 * time.Millisecond)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewIndexAdminClient(conn)
	healthResp, err := client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !healthResp.DbQuickCheckOk {
		t.Errorf("quick_check = false, err = %s", healthResp.ErrorMessage)
	}
}
