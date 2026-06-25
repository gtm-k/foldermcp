//go:build cgo

package grpc

import (
	"context"
	"path/filepath"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

func TestGetCapabilitiesReflectsSchemaVersion(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "c.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}

	h := &CapabilitiesHandler{DB: db}
	resp, err := h.GetCapabilities(context.Background(), &pb.GetCapabilitiesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SchemaVersion != 3 {
		t.Errorf("schema_version = %d, want 3", resp.SchemaVersion)
	}
	if resp.WireVersion != 1 {
		t.Errorf("wire_version = %d", resp.WireVersion)
	}
	if len(resp.RpcsSupported) < 11 {
		t.Errorf("expected 11+ rpcs, got %d", len(resp.RpcsSupported))
	}
	if resp.ServerVersion != "v3.0.0-m1" {
		t.Errorf("server_version = %s", resp.ServerVersion)
	}
}
