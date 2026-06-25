//go:build cgo

package store

import (
	"database/sql"
	"errors"
	"time"
)

type Fingerprint struct {
	ModelName        string
	ModelVersion     string
	Dimension        int
	ChunkingPolicy   string
	QuantizationMode string // "int8", "int8_fixed_s<scale>", or "float32"
}

// ErrFingerprintMismatch reports that the stored index was built with a
// different embedding model, chunking policy, or quantization scheme than this
// binary produces, so its persisted vectors are not comparable to freshly
// embedded queries. The recovery is to rebuild: there is no in-place upgrade,
// so the message names the action that actually exists (delete + re-index)
// rather than a command that does not.
var ErrFingerprintMismatch = errors.New("embedding_fingerprint mismatch: this index was built with a different embedding model, chunking policy, or quantization scheme than this binary produces — delete the store's index.db and re-run `index-v3` to rebuild it")

// WriteFingerprint inserts or replaces the singleton row.
func WriteFingerprint(db *sql.DB, fp Fingerprint) error {
	_, err := db.Exec(`
INSERT INTO embedding_fingerprint(id, model_name, model_version, dimension, chunking_policy, quantization_mode, created_at)
VALUES (1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    model_name=excluded.model_name,
    model_version=excluded.model_version,
    dimension=excluded.dimension,
    chunking_policy=excluded.chunking_policy,
    quantization_mode=excluded.quantization_mode,
    created_at=excluded.created_at
`, fp.ModelName, fp.ModelVersion, fp.Dimension, fp.ChunkingPolicy, fp.QuantizationMode, time.Now().Unix())
	return err
}

// ReadFingerprint returns sql.ErrNoRows if no row is present.
func ReadFingerprint(db *sql.DB) (Fingerprint, error) {
	var fp Fingerprint
	err := db.QueryRow(`
SELECT model_name, model_version, dimension, chunking_policy, quantization_mode
FROM embedding_fingerprint WHERE id=1`).Scan(
		&fp.ModelName, &fp.ModelVersion, &fp.Dimension, &fp.ChunkingPolicy, &fp.QuantizationMode,
	)
	return fp, err
}

// CheckFingerprint compares the stored fingerprint with `want`.
// Returns ErrFingerprintMismatch if they differ; nil if equal or no row exists.
func CheckFingerprint(db *sql.DB, want Fingerprint) error {
	got, err := ReadFingerprint(db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if got != want {
		return ErrFingerprintMismatch
	}
	return nil
}
