//go:build cgo

// Package tools contains high-level IndexTools RPC handlers.
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

// SearchBroadlyHandler implements staged retrieval + RRF fusion.
// Implementation in search_broadly.go.
type SearchBroadlyHandler struct {
	DB       *sql.DB
	Vector   *query.VectorHandler
	FTS      *query.FTSHandler
	Filename *query.FilenameHandler
	Embedder *embed.Embedder
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
