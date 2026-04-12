//go:build cgo

package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

func TestPendingFilesSkipsDone(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "p.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Seed 3 files
	for i := 0; i < 3; i++ {
		_, err := db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen) VALUES(?,?,1,1,'t/p','document','/',1)`,
			string(rune('a'+i)), []byte("x"))
		if err != nil {
			t.Fatalf("seed file %d: %v", i, err)
		}
	}

	ctx := context.Background()
	// Mark file_id=1 as done for walker
	if err := MarkRunning(ctx, db, 1, PassWalker); err != nil {
		t.Fatal(err)
	}
	if err := MarkDone(ctx, db, 1, PassWalker); err != nil {
		t.Fatal(err)
	}

	pending, err := PendingFiles(ctx, db, PassWalker, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("pending = %d, want 2 (file 1 done)", len(pending))
	}
}

func TestMarkFailedRecordsError(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "p.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	_, err = db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen) VALUES('f',?,1,1,'t/p','code','/',1)`, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := MarkRunning(ctx, db, 1, PassStructural); err != nil {
		t.Fatal(err)
	}
	if err := MarkFailed(ctx, db, 1, PassStructural, &testError{"parse failed"}); err != nil {
		t.Fatal(err)
	}

	var status, errMsg string
	err = db.QueryRow(`SELECT status, error_message FROM pipeline_state WHERE file_id=1 AND pass_name='structural'`).Scan(&status, &errMsg)
	if err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	if errMsg != "parse failed" {
		t.Errorf("error_message = %q, want 'parse failed'", errMsg)
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
