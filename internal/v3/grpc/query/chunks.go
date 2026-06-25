//go:build cgo

package query

import (
	"context"
	"database/sql"
	"fmt"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ChunksHandler implements GetChunks — fetch specific chunks by ID.
type ChunksHandler struct {
	DB *sql.DB
}

func (h *ChunksHandler) Get(ctx context.Context, req *pb.GetChunksRequest) (*pb.GetChunksResponse, error) {
	if len(req.ChunkIds) == 0 {
		return &pb.GetChunksResponse{}, nil
	}

	placeholders := make([]byte, 0, 2*len(req.ChunkIds))
	args := make([]any, len(req.ChunkIds))
	for i, id := range req.ChunkIds {
		args[i] = id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}

	q := fmt.Sprintf(`
SELECT chunk_id, text, token_count, chunk_kind
FROM chunks WHERE chunk_id IN (%s) AND deleted_at IS NULL`, placeholders)

	rows, err := h.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get_chunks query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	resp := &pb.GetChunksResponse{}
	for rows.Next() {
		c := &pb.HydratedChunk{}
		if err := rows.Scan(&c.ChunkId, &c.Text, &c.TokenCount, &c.ChunkKind); err != nil {
			return nil, status.Errorf(codes.Internal, "get_chunks scan: %v", err)
		}
		resp.Chunks = append(resp.Chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "get_chunks rows: %v", err)
	}
	return resp, nil
}

// BlobsHandler implements GetBlob — fetch a blob by ID from the blobs table.
type BlobsHandler struct {
	DB *sql.DB
}

func (h *BlobsHandler) Get(ctx context.Context, req *pb.GetBlobRequest) (*pb.GetBlobResponse, error) {
	var cachePath, blobType string
	err := h.DB.QueryRowContext(ctx, `
SELECT cache_path, blob_type FROM blobs
WHERE blob_id = ?`, req.BlobId).Scan(&cachePath, &blobType)
	if err == sql.ErrNoRows {
		return nil, status.Errorf(codes.NotFound, "blob %d not found", req.BlobId)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get_blob: %v", err)
	}
	// In M1, blobs table is mostly empty (OCR is M2). Return metadata only.
	return &pb.GetBlobResponse{Mime: blobType}, nil
}
