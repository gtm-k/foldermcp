//go:build cgo

package tools

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// Browse lists files whose parent_dir matches the requested path,
// with base64-encoded cursor pagination.
func (h *BrowseHandler) Browse(ctx context.Context, req *pb.BrowseFolderRequest) (*pb.BrowseFolderResponse, error) {
	resp := &pb.BrowseFolderResponse{
		Status: &pb.SourceStatus{SourceName: "browse"},
	}
	maxItems := int(req.MaxItems)
	if maxItems <= 0 {
		maxItems = 50
	}
	if maxItems > 500 {
		maxItems = 500
	}

	offset := 0
	if req.Cursor != "" {
		raw, err := base64.StdEncoding.DecodeString(req.Cursor)
		if err == nil {
			if n, err := strconv.Atoi(string(raw)); err == nil {
				offset = n
			}
		}
	}

	// Exact-path children: files whose parent_dir = req.Path
	rows, err := h.DB.QueryContext(ctx, `
SELECT file_id, path, size, mtime, content_class
FROM files
WHERE parent_dir = ? AND deleted_at IS NULL
ORDER BY path
LIMIT ? OFFSET ?`, req.Path, maxItems+1, offset)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = err.Error()
		return resp, nil
	}
	defer func() { _ = rows.Close() }()

	n := 0
	for rows.Next() {
		n++
		if n > maxItems {
			resp.NextCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset + maxItems)))
			break
		}
		var e pb.FolderEntry
		if err := rows.Scan(&e.FileId, &e.Path, &e.Size, &e.Mtime, &e.ContentClass); err != nil {
			resp.Status.Status = "CONTRACT_ERROR"
			resp.Status.ErrorMessage = err.Error()
			return resp, nil
		}
		parts := strings.Split(e.Path, "/")
		e.Name = parts[len(parts)-1]
		e.IsDir = false
		resp.Entries = append(resp.Entries, &e)
	}
	if err := rows.Err(); err != nil {
		resp.Status.Status = "CONTRACT_ERROR"
		resp.Status.ErrorMessage = err.Error()
		return resp, nil
	}

	resp.Status.Status = "OK"
	return resp, nil
}
