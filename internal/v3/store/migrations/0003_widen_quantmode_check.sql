-- 0003_widen_quantmode_check.sql — Q1 fixed-scale int8 quantization. Forward-only.
--
-- The embedding int8 scheme changed from per-vector scaling (scale = 127/maxAbs,
-- which divided each vector by its own max-abs and broke cross-vector L2
-- comparability) to a single fixed global scale (embed.Int8Scale). That is a
-- distinct on-disk representation, so embedding_fingerprint.quantization_mode
-- advances from 'int8' to a scale-tagged 'int8_fixed_s<scale>' (e.g.
-- 'int8_fixed_s256', produced by embed.QuantizationModeString). Encoding the
-- scale means a future scale change ALSO changes the label, so an existing
-- index (per-vector 'int8' OR fixed-scale at a different scale) fails the
-- startup fingerprint check (store.CheckFingerprint) and is rebuilt.
--
-- SQLite cannot ALTER a CHECK constraint in place, so we rebuild the table.
-- embedding_fingerprint is a singleton (CHECK id=1) with no inbound foreign
-- keys, triggers, or indexes, so create-copy-drop-rename is safe and cheap.
-- The widened CHECK keeps the legacy values valid (forward-compatible widening,
-- not a swap) so float32 stores and any not-yet-rebuilt int8 stores still load.
--
-- IF [NOT] EXISTS mirrors 0002's idempotency contract: the migrate.go runner
-- applies each file once in a single transaction, but the clauses tolerate
-- ad-hoc manual replay (tests, sqlite3 sessions) at zero cost.

DROP TABLE IF EXISTS embedding_fingerprint_new;

CREATE TABLE IF NOT EXISTS embedding_fingerprint_new (
    id INTEGER PRIMARY KEY CHECK (id = 1),  -- singleton row
    model_name TEXT NOT NULL,
    model_version TEXT NOT NULL,
    dimension INTEGER NOT NULL,
    chunking_policy TEXT NOT NULL,
    quantization_mode TEXT NOT NULL
        CHECK (quantization_mode IN ('int8','float32')
               OR quantization_mode GLOB 'int8_fixed_s[0-9]*'),
    created_at INTEGER NOT NULL
);

INSERT INTO embedding_fingerprint_new
    SELECT id, model_name, model_version, dimension, chunking_policy, quantization_mode, created_at
    FROM embedding_fingerprint;

DROP TABLE embedding_fingerprint;

ALTER TABLE embedding_fingerprint_new RENAME TO embedding_fingerprint;
