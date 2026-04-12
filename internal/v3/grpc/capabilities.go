//go:build cgo

package grpc

import (
	"context"
	"database/sql"
	"strconv"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

const (
	WireVersion               = 1
	RetrievalSemanticsVersion = 1
)

// ServerVersion is set at build time or defaults to the M1 dev version.
var ServerVersion = "v3.0.0-m1"

// CapabilitiesHandler implements IndexCapabilities.GetCapabilities.
type CapabilitiesHandler struct {
	DB *sql.DB
}

func (h *CapabilitiesHandler) GetCapabilities(ctx context.Context, _ *pb.GetCapabilitiesRequest) (*pb.GetCapabilitiesResponse, error) {
	var sv string
	_ = h.DB.QueryRowContext(ctx, `SELECT value FROM config WHERE key='schema_version'`).Scan(&sv)
	schemaVer, _ := strconv.Atoi(sv)
	return &pb.GetCapabilitiesResponse{
		ServerVersion:             ServerVersion,
		WireVersion:               int32(WireVersion),
		SchemaVersion:             int32(schemaVer),
		RetrievalSemanticsVersion: int32(RetrievalSemanticsVersion),
		Features: []string{
			"fts5", "sqlite-vec-int8", "tree-sitter-python", "tree-sitter-go",
			"batch-rpc", "hydration-hint",
		},
		RpcsSupported: []string{
			"IndexQuery.VectorSearch", "IndexQuery.FTSSearch", "IndexQuery.FilenameSearch",
			"IndexQuery.MetadataSearch", "IndexQuery.GraphExpand", "IndexQuery.GetNodes",
			"IndexQuery.GetChunks", "IndexQuery.GetBlob", "IndexQuery.Batch",
			// IndexTools RPCs omitted until Phase E implements them
			"IndexAdmin.Status", "IndexAdmin.Health",
			"IndexCapabilities.GetCapabilities",
		},
	}, nil
}
