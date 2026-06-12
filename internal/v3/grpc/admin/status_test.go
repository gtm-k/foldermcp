//go:build cgo

package admin

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

func TestStatusReportsFileAndPassCounts(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "st.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}

	// Seed files and pipeline state
	if _, err := db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a.md',X'AA',1,1,'text/markdown','document','/',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/b.py',X'BB',2,2,'text/x-python','code','/',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO pipeline_state(file_id,pass_name,status,checkpoint_at)
		VALUES(1,'embeddings','done',1)`); err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db, StartTime: time.Now().Add(-10 * time.Second)}
	resp, err := h.Status(context.Background(), &pb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.FilesTotal != 2 {
		t.Errorf("files_total = %d, want 2", resp.FilesTotal)
	}
	if resp.FilesIndexed != 1 {
		t.Errorf("files_indexed = %d, want 1", resp.FilesIndexed)
	}
	if resp.PassCounts["embeddings"] != 1 {
		t.Errorf("pass_counts[embeddings] = %d, want 1", resp.PassCounts["embeddings"])
	}
	if resp.UptimeSeconds < 10 {
		t.Errorf("uptime = %d, want >= 10", resp.UptimeSeconds)
	}
}

// TestStatusSurfacesFailureCounts (D28b.5, pre-mortem Story 2): failed
// pipeline_state rows surface in pass_counts under synthetic
// '<pass>_failed' integer keys — no proto change (D4). Fixture: 10
// files, 8 fully indexed, 2 failed at the structural pass.
func TestStatusSurfacesFailureCounts(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "st.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 10; i++ {
		if _, err := db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
			VALUES(?,X'AB',1,1,'text/markdown','document','/',1)`, fmt.Sprintf("/f%d.md", i)); err != nil {
			t.Fatal(err)
		}
	}
	// Files 1–8 completed the terminal pass.
	for i := 1; i <= 8; i++ {
		if _, err := db.Exec(`INSERT INTO pipeline_state(file_id,pass_name,status,checkpoint_at)
			VALUES(?,'embeddings','done',1)`, i); err != nil {
			t.Fatal(err)
		}
	}
	// Files 9–10 failed at structural.
	for i := 9; i <= 10; i++ {
		if _, err := db.Exec(`INSERT INTO pipeline_state(file_id,pass_name,status,checkpoint_at,error_message)
			VALUES(?,'structural','failed',1,'extract: synthetic failure')`, i); err != nil {
			t.Fatal(err)
		}
	}

	h := &Handler{DB: db}
	resp, err := h.Status(context.Background(), &pb.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PassCounts["structural_failed"] != 2 {
		t.Errorf("pass_counts[structural_failed] = %d, want 2", resp.PassCounts["structural_failed"])
	}
	if resp.FilesIndexed != 8 {
		t.Errorf("files_indexed = %d, want 8", resp.FilesIndexed)
	}
	if resp.PassCounts["embeddings"] != 8 {
		t.Errorf("pass_counts[embeddings] = %d, want 8", resp.PassCounts["embeddings"])
	}
	// No spurious failure keys for passes without failures.
	for _, k := range []string{"chunker_failed", "embeddings_failed"} {
		if v, ok := resp.PassCounts[k]; ok {
			t.Errorf("unexpected pass_counts[%s] = %d", k, v)
		}
	}
}

func TestHealthOnFreshDB(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.Open(store.Options{Path: filepath.Join(tmp, "h.db"), Tier: store.TierMid})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}

	h := &Handler{DB: db}
	resp, err := h.Health(context.Background(), &pb.HealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.DbQuickCheckOk {
		t.Errorf("quick_check = false, err = %s", resp.ErrorMessage)
	}
	if !resp.IntegrityCheckOk {
		t.Errorf("integrity_check = false, err = %s", resp.ErrorMessage)
	}
}
