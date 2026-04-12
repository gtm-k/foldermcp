//go:build cgo

package store

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestVec0TableCreated(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "vec.db")

	db, err := Open(Options{Path: dbPath, Tier: TierMid})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Apply the base schema so chunks exists for the FK chain.
	// (The dedicated migration runner lands in Task B9 — for now we
	// exec the init SQL directly.)
	mfs, err := MigrationFiles()
	if err != nil {
		t.Fatalf("MigrationFiles: %v", err)
	}
	raw, err := fs.ReadFile(mfs, "0001_init.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(raw)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	// Walk the FK chain: files → nodes → chunks → embeddings.
	if _, err := db.Exec(`INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen) VALUES('/a',?,1,1,'text/plain','document','/',1)`, []byte("xx")); err != nil {
		t.Fatalf("file insert: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at) VALUES(1,'doc','a','EXTRACTED',1,1)`); err != nil {
		t.Fatalf("node insert: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind) VALUES(1,'hello',0,5,1,'prose')`); err != nil {
		t.Fatalf("chunk insert: %v", err)
	}

	vec := make([]byte, 384)
	for i := range vec {
		vec[i] = byte(i % 128)
	}
	if _, err := db.Exec(`INSERT INTO embeddings(chunk_id, embedding) VALUES(1, ?)`, vec); err != nil {
		t.Fatalf("embedding insert: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}
	db2, err := Open(Options{Path: dbPath, Tier: TierMid})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()

	var got []byte
	if err := db2.QueryRow(`SELECT embedding FROM embeddings WHERE chunk_id=1`).Scan(&got); err != nil {
		t.Fatalf("read embedding: %v", err)
	}
	if !bytes.Equal(got, vec) {
		t.Errorf("roundtrip mismatch: got %d bytes, want %d", len(got), len(vec))
	}
}
