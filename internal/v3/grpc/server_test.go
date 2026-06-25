//go:build cgo

package grpc

import (
	"context"
	"database/sql"
	"net"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestVectorSearchGatedOnIncompatibleIndex pins the central read-side gate:
// the PUBLIC VectorSearch RPC (and Batch) accept client-supplied int8 codes and
// never touch the embedder, so the CLI's embedder-nil gate does not protect
// them. NewServer must refuse vector search when the stored quantization mode
// is incompatible with this binary's encoder, rather than run KNN on
// incomparable codes and return silent garbage.
func TestVectorSearchGatedOnIncompatibleIndex(t *testing.T) {
	newDB := func(t *testing.T) *sql.DB {
		t.Helper()
		db, err := store.Open(store.Options{Path: filepath.Join(t.TempDir(), "s.db"), Tier: store.TierMid})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Migrate(db, ""); err != nil {
			t.Fatal(err)
		}
		return db
	}
	req := &pb.VectorSearchRequest{QueryEmbeddingInt8: make([]byte, 384), K: 3}

	t.Run("incompatible index degrades", func(t *testing.T) {
		db := newDB(t)
		defer func() { _ = db.Close() }()
		if err := store.WriteFingerprint(db, store.Fingerprint{
			ModelName: "all-MiniLM-L6-v2", ModelVersion: "2.2.0", Dimension: 384,
			ChunkingPolicy: "recursive_80_200_260_v1", QuantizationMode: "int8", // legacy per-vector
		}); err != nil {
			t.Fatal(err)
		}
		srv := NewServer(ServerOpts{DB: db})
		resp, err := srv.VectorSearch(context.Background(), req)
		if err != nil {
			t.Fatalf("VectorSearch: %v", err)
		}
		if resp.Status.Status != "DEGRADED" {
			t.Errorf("status = %q, want DEGRADED — incompatible index must not run KNN", resp.Status.Status)
		}
		if len(resp.Results) != 0 {
			t.Errorf("got %d results, want 0 on incompatible index", len(resp.Results))
		}
	})

	t.Run("compatible index is not gated", func(t *testing.T) {
		db := newDB(t) // fresh DB: no fingerprint → compatible
		defer func() { _ = db.Close() }()
		srv := NewServer(ServerOpts{DB: db})
		resp, err := srv.VectorSearch(context.Background(), req)
		if err != nil {
			t.Fatalf("VectorSearch: %v", err)
		}
		if resp.Status.Status == "DEGRADED" {
			t.Errorf("fresh compatible index was gated (DEGRADED): %s", resp.Status.ErrorMessage)
		}
	})
}

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
	// A4 added migration 0004 (chunk_kind widening); a migrated store reports 4.
	if resp.SchemaVersion != 4 {
		t.Errorf("schema_version = %d, want 4", resp.SchemaVersion)
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
