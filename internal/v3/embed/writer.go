//go:build cgo

package embed

import (
	"database/sql"
	"fmt"

	"github.com/gtm-k/foldermcp/internal/v3/chunker"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

// EmbedPair holds a chunk ID and its quantized embedding vector.
type EmbedPair struct {
	ChunkID int64
	Vector  []int8
}

// Writer persists int8 embeddings to the vec0 table, guarded by
// fingerprint check. The fingerprint ensures that embeddings are only
// written if the model/chunking/quantization config matches what was
// previously stored.
type Writer struct {
	db *sql.DB
	fp store.Fingerprint
}

func NewWriter(db *sql.DB) *Writer {
	return &Writer{
		db: db,
		fp: store.Fingerprint{
			ModelName:    ModelName,
			ModelVersion: ModelVersion,
			Dimension:    Dimension,
			// Bound to the live chunker config (D28b D3, pre-mortem
			// Story 3) — a chunk-size change cannot silently leave a
			// stale policy label on persisted embeddings.
			ChunkingPolicy: chunker.DefaultConfig().PolicyString(),
			// Fixed-scale int8 (QuantizeInt8 uses a single global Int8Scale,
			// not per-vector 127/maxAbs). The mode label embeds the scale
			// (e.g. "int8_fixed_s256") so both an existing per-vector index
			// ("int8") AND any future scale change fail the startup fingerprint
			// check and trigger a rebuild. The widened CHECK that admits these
			// values ships in migration 0003.
			QuantizationMode: QuantizationModeString(),
		},
	}
}

// EnsureFingerprint writes the fingerprint if missing, or verifies it matches.
// Must be called once before any WriteBatch calls.
func (w *Writer) EnsureFingerprint() error {
	if err := store.CheckFingerprint(w.db, w.fp); err != nil {
		return fmt.Errorf("embedding writer: %w", err)
	}
	// Write only if not present
	_, err := store.ReadFingerprint(w.db)
	if err == sql.ErrNoRows {
		return store.WriteFingerprint(w.db, w.fp)
	}
	return err
}

// WriteBatch inserts one row per (chunk_id, int8 vector) pair into the
// vec0 embeddings table. Uses vec_int8() wrapper required by sqlite-vec
// for int8 vectors (Phase B lesson: plain blobs default to float32).
func (w *Writer) WriteBatch(tx *sql.Tx, pairs []EmbedPair) error {
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO embeddings(chunk_id, embedding) VALUES(?, vec_int8(?))`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, p := range pairs {
		if len(p.Vector) != Dimension {
			return fmt.Errorf("vector len %d, want %d", len(p.Vector), Dimension)
		}
		blob := make([]byte, Dimension)
		for i, v := range p.Vector {
			blob[i] = byte(v)
		}
		if _, err := stmt.Exec(p.ChunkID, blob); err != nil {
			return err
		}
	}
	return nil
}
