//go:build cgo

// Package tools contains high-level IndexTools RPC handlers.
// Struct definitions live here; implementations in per-handler files.
package tools

import (
	"database/sql"

	"github.com/gtm-k/foldermcp/internal/v3/embed"
	"github.com/gtm-k/foldermcp/internal/v3/grpc/query"
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

// InspectHandler implements node inspection.
// Implementation in inspect_node.go.
type InspectHandler struct {
	DB    *sql.DB
	Nodes *query.NodesHandler
}

// BrowseHandler implements folder browsing with cursor pagination.
// Implementation in browse_folder.go.
type BrowseHandler struct {
	DB *sql.DB
}
