//go:build cgo

package pipeline

import (
	"context"
	"database/sql"
	"time"
)

// PassName identifies a pipeline pass for checkpoint tracking.
type PassName string

const (
	PassWalker     PassName = "walker"
	PassStructural PassName = "structural"
	PassChunker    PassName = "chunker"
	PassEmbeddings PassName = "embeddings"
)

// Status represents the state of a (file, pass) pair.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// MarkRunning upserts a (file_id, pass_name) row with status=running.
func MarkRunning(ctx context.Context, db *sql.DB, fileID int64, pass PassName) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO pipeline_state(file_id, pass_name, status, checkpoint_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(file_id, pass_name) DO UPDATE SET
    status=excluded.status, checkpoint_at=excluded.checkpoint_at, error_message=NULL`,
		fileID, string(pass), string(StatusRunning), time.Now().Unix())
	return err
}

// MarkDone sets status=done, clears error.
func MarkDone(ctx context.Context, db *sql.DB, fileID int64, pass PassName) error {
	_, err := db.ExecContext(ctx, `
UPDATE pipeline_state SET status='done', checkpoint_at=?, error_message=NULL
WHERE file_id=? AND pass_name=?`, time.Now().Unix(), fileID, string(pass))
	return err
}

// MarkFailed records an error message and sets status=failed.
func MarkFailed(ctx context.Context, db *sql.DB, fileID int64, pass PassName, passErr error) error {
	_, err := db.ExecContext(ctx, `
UPDATE pipeline_state SET status='failed', checkpoint_at=?, error_message=?
WHERE file_id=? AND pass_name=?`, time.Now().Unix(), passErr.Error(), fileID, string(pass))
	return err
}

// PendingFiles returns file IDs that have no 'done' row for the given pass.
// Used for resume-from-checkpoint: after a crash, only unfinished files
// are returned.
func PendingFiles(ctx context.Context, db *sql.DB, pass PassName, limit int) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `
SELECT f.file_id FROM files f
WHERE f.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM pipeline_state ps
    WHERE ps.file_id=f.file_id AND ps.pass_name=? AND ps.status='done'
  )
LIMIT ?`, string(pass), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
