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
	QuantizationMode string // "int8" or "float32"
}

var ErrFingerprintMismatch = errors.New("embedding_fingerprint mismatch — run `foldermcp index rebuild-vectors` or pass --force-new-schema")

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
