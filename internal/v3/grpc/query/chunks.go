//go:build cgo

package query

import (
	"context"
	"database/sql"
	"fmt"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
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
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	resp := &pb.GetChunksResponse{}
	for rows.Next() {
		c := &pb.HydratedChunk{}
		if err := rows.Scan(&c.ChunkId, &c.Text, &c.TokenCount, &c.ChunkKind); err != nil {
			return nil, err
		}
		resp.Chunks = append(resp.Chunks, c)
	}
	return resp, rows.Err()
}

// BlobsHandler implements GetBlob — fetch a blob by ID from the blobs table.
type BlobsHandler struct {
	DB *sql.DB
}

func (h *BlobsHandler) Get(ctx context.Context, req *pb.GetBlobRequest) (*pb.GetBlobResponse, error) {
	resp := &pb.GetBlobResponse{}
	var cachePath, mime string
	err := h.DB.QueryRowContext(ctx, `
SELECT cache_path, b.blob_type FROM blobs b
WHERE b.blob_id = ?`, req.BlobId).Scan(&cachePath, &mime)
	if err == sql.ErrNoRows {
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	// In M1, blobs table is mostly empty (OCR is M2). Return metadata only.
	resp.Mime = mime
	return resp, nil
}
