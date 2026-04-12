//go:build cgo

// Package tools contains high-level IndexTools RPC handlers.
// Full implementations arrive in Phase E; these stubs let the
// server assembly compile and pass UNIMPLEMENTED to callers.
package tools

import (
	"context"
	"database/sql"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/embed"
	"github.com/gtm-k/foldermcp/internal/v3/grpc/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SearchBroadlyHandler will implement staged retrieval + RRF fusion in Phase E.
type SearchBroadlyHandler struct {
	DB       *sql.DB
	Vector   *query.VectorHandler
	FTS      *query.FTSHandler
	Filename *query.FilenameHandler
	Embedder *embed.Embedder
}

func (h *SearchBroadlyHandler) SearchBroadly(ctx context.Context, req *pb.SearchBroadlyRequest) (*pb.SearchBroadlyResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "SearchBroadly: Phase E stub")
}

// InspectHandler will implement node inspection in Phase E.
type InspectHandler struct {
	DB    *sql.DB
	Nodes *query.NodesHandler
}

func (h *InspectHandler) Inspect(ctx context.Context, req *pb.InspectNodeRequest) (*pb.InspectNodeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "InspectNode: Phase E stub")
}

// BrowseHandler will implement folder browsing in Phase E.
type BrowseHandler struct {
	DB *sql.DB
}

func (h *BrowseHandler) Browse(ctx context.Context, req *pb.BrowseFolderRequest) (*pb.BrowseFolderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "BrowseFolder: Phase E stub")
}
