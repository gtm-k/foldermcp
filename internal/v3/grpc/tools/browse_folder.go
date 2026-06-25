//go:build cgo

package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// Browse lists the immediate children — files AND subdirectories — of a folder.
//
// The store has no directory table: directories are implicit in file paths
// (parent_dir = filepath.Dir(path), no trailing slash, OS-native separators).
// So Browse (a) normalizes the requested path to match that convention, (b)
// lists files whose parent_dir equals the path, (c) derives immediate
// subdirectories from the parent_dir of deeper files via an index-friendly
// prefix range, and (d) distinguishes a path with no indexed entries
// (NOT_FOUND) from a successful listing — the previous handler hardcoded
// IsDir=false, never surfaced subdirectories, did no normalization, and
// returned OK for every path (so a wrong path was indistinguishable from an
// empty one).
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
		if raw, err := base64.StdEncoding.DecodeString(req.Cursor); err == nil {
			if n, err := strconv.Atoi(string(raw)); err == nil && n > 0 {
				offset = n
			}
		}
	}

	path := normalizeBrowsePath(req.Path)
	sep := string(os.PathSeparator)
	childPrefix := path + sep
	if isRoot(path, sep) {
		childPrefix = sep
	}

	// Immediate subdirectories: distinct first path segment among files whose
	// parent_dir falls in the [childPrefix, childPrefix++) prefix range. A
	// range scan (not LIKE) keeps it injection-safe and uses idx_files_parent_dir.
	dirSet := map[string]bool{}
	lo := childPrefix
	hi := prefixUpperBound(childPrefix)
	dirQuery := `SELECT DISTINCT parent_dir FROM files WHERE deleted_at IS NULL AND parent_dir >= ?`
	args := []any{lo}
	if hi != "" {
		dirQuery += ` AND parent_dir < ?`
		args = append(args, hi)
	}
	rows, err := h.DB.QueryContext(ctx, dirQuery, args...)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = "browse subdir query failed"
		return resp, nil
	}
	for rows.Next() {
		var pd string
		if err := rows.Scan(&pd); err != nil {
			_ = rows.Close()
			resp.Status.Status = "CONTRACT_ERROR"
			resp.Status.ErrorMessage = "row scan failed"
			return resp, nil
		}
		rel := strings.TrimPrefix(pd, childPrefix)
		if rel == "" {
			continue
		}
		seg := rel
		if i := strings.IndexAny(rel, `/\`); i >= 0 {
			seg = rel[:i]
		}
		if seg != "" {
			dirSet[seg] = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		resp.Status.Status = "CONTRACT_ERROR"
		resp.Status.ErrorMessage = "result iteration failed"
		return resp, nil
	}
	_ = rows.Close()

	entries := make([]*pb.FolderEntry, 0, len(dirSet))
	for seg := range dirSet {
		entries = append(entries, &pb.FolderEntry{
			Name:  seg,
			Path:  childPrefix + seg,
			IsDir: true,
		})
	}

	// Direct file children: parent_dir equals the normalized path exactly.
	frows, err := h.DB.QueryContext(ctx, `
SELECT file_id, path, size, mtime, content_class
FROM files
WHERE parent_dir = ? AND deleted_at IS NULL`, path)
	if err != nil {
		resp.Status.Status = "DEGRADED"
		resp.Status.ErrorMessage = "browse query failed"
		return resp, nil
	}
	for frows.Next() {
		var e pb.FolderEntry
		if err := frows.Scan(&e.FileId, &e.Path, &e.Size, &e.Mtime, &e.ContentClass); err != nil {
			_ = frows.Close()
			resp.Status.Status = "CONTRACT_ERROR"
			resp.Status.ErrorMessage = "row scan failed"
			return resp, nil
		}
		e.Name = baseName(e.Path)
		e.IsDir = false
		entries = append(entries, &e)
	}
	if err := frows.Err(); err != nil {
		_ = frows.Close()
		resp.Status.Status = "CONTRACT_ERROR"
		resp.Status.ErrorMessage = "result iteration failed"
		return resp, nil
	}
	_ = frows.Close()

	// Actor-observability: a path with no files and no subdirectories does not
	// exist in the index (an empty directory cannot be indexed). Root is always
	// a valid container even when empty.
	if len(entries) == 0 && !isRoot(path, sep) {
		resp.Status.Status = "NOT_FOUND"
		resp.Status.ErrorMessage = "no indexed entries under this path"
		return resp, nil
	}

	// Deterministic order by full path; paginate the merged dir+file list.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	if offset > len(entries) {
		offset = len(entries)
	}
	end := offset + maxItems
	if end >= len(entries) {
		end = len(entries)
	} else {
		resp.NextCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	resp.Entries = entries[offset:end]
	resp.Status.Status = "OK"
	return resp, nil
}

// normalizeBrowsePath canonicalizes the request path to the daemon's native
// form so it matches the walker-stored parent_dir (filepath.Dir). filepath is
// OS-specific at compile time, and the daemon indexed the paths on this same
// OS, so FromSlash + Clean align a client's slash style and trailing separators
// with the stored separator — fixing the case where a forward-slash request
// would never match backslash-indexed parent_dir values on Windows.
func normalizeBrowsePath(p string) string {
	if p == "" {
		return string(os.PathSeparator)
	}
	return filepath.Clean(filepath.FromSlash(p))
}

func isRoot(p, sep string) bool {
	return p == sep || p == "/"
}

// prefixUpperBound returns the smallest string strictly greater than every
// string with prefix s, for a half-open range scan. Empty means "no upper
// bound" (s is all 0xff bytes — not reachable for real paths).
func prefixUpperBound(s string) string {
	b := []byte(s)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return string(b[:i+1])
		}
	}
	return ""
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
