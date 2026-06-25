-- 0004_widen_chunk_kind.sql — Program A consolidated chunk_kind widening. Forward-only.
--
-- M2 Program A introduces extractor- and structure-aware chunkers (A4 PDF/Office,
-- A5 csv/json/yaml/xml). Each emits a distinct chunk_kind so provenance is never
-- ambiguous:
--   office_text       — docx/odt text runs (A4; DISTINCT from pdf_text by design)
--   csv_schema        — per-CSV column/dtype summary chunk (A5)
--   csv_rows          — stratified CSV sample-row chunks (A5)
--   data_structured   — json/yaml/xml structure-aware chunks (A5)
-- pdf_text and the M1 kinds (prose, code_ast, code_fallback) were already allowed
-- by 0001_init.sql, so PDF itself needs no migration; this file widens the CHECK
-- once for ALL of Program A's new kinds (net-new #4 of the gap-closure plan), so
-- A4 and A5 do not race a migration-number scramble — they share this file and
-- therefore serialize under the conservative-merge rule.
--
-- R1 (gap-closure plan, HIGH): Q1 already shipped 0003_widen_quantmode_check.sql,
-- so the runner's schema_version is 3 on real upgraded stores. migrate.go selects
-- migrations by numeric prefix and applies only those with version > current
-- (migrate.go:128), so this MUST be 0004 — a duplicate 0003 would pass fresh-DB
-- tests yet be SILENTLY SKIPPED on a populated schema_version=3 store. The
-- migration test exercises the populated-upgrade path, not only from-empty.
--
-- SQLite cannot ALTER a CHECK constraint in place, so widening chunk_kind requires
-- a full chunks-table rebuild (D7). chunks is the FK CHILD of nodes (no table
-- references chunks), so the rename re-points no parent FK; with foreign_keys=ON
-- the DROP is legal. Rowid (chunk_id) preservation is CRITICAL: embeddings is a
-- vec0 virtual table keyed by chunk_id rowid (D13, no FK cascade) and chunks_fts
-- is external-content keyed by content_rowid='chunk_id'; the INSERT...SELECT
-- copies chunk_id EXPLICITLY so every embedding and FTS rowid reference stays
-- valid. The migrate.go runner wraps this whole file in one transaction and takes
-- a VACUUM INTO snapshot first (verified migrate.go:43-58), so a mid-rebuild crash
-- rolls back atomically and the snapshot is the manual fallback.

-- legacy_alter_table=ON is REQUIRED for the rename in step 4. Migration 0002
-- added the files_sha256_invalidate trigger whose body references the chunks
-- table. Since SQLite 3.25 (legacy_alter_table OFF, the default), ALTER TABLE
-- ... RENAME re-validates every other trigger/view body, and at the rename
-- instant the original chunks table has just been dropped — so SQLite raises
-- "error in trigger files_sha256_invalidate: no such table: main.chunks" and
-- aborts. legacy_alter_table=ON restores the pre-3.25 behaviour (rename does NOT
-- rewrite or validate references in unrelated triggers), which is exactly what
-- this create-copy-drop-rename rebuild needs. The trigger's chunks reference is
-- correct again the moment chunks_new is renamed to chunks. Verified against
-- sqlite 3.45.1 (the WSL gate build). This PRAGMA is connection-scoped; reset to
-- OFF after the rename so the rest of the migration keeps modern semantics.
-- The migrate.go runner pins the pool to one connection, so the toggle is local
-- to this migration and does not leak.
PRAGMA legacy_alter_table=ON;

-- 1. Drop the three FTS5 sync triggers so chunk DML during the rebuild does not
--    write the shadow tables (we rebuild FTS from scratch in step 6).
DROP TRIGGER IF EXISTS chunks_ai;
DROP TRIGGER IF EXISTS chunks_ad;
DROP TRIGGER IF EXISTS chunks_au;

DROP TABLE IF EXISTS chunks_new;

-- 2. Identical DDL to 0001_init.sql:81-92 with the CHECK widened to all
--    Program-A kinds.
CREATE TABLE chunks_new (
    chunk_id INTEGER PRIMARY KEY,
    node_id INTEGER NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    byte_start INTEGER NOT NULL,
    byte_end INTEGER NOT NULL,
    token_count INTEGER NOT NULL,
    chunk_kind TEXT NOT NULL
        CHECK (chunk_kind IN ('prose','code_ast','pdf_text','code_fallback','office_text','csv_schema','csv_rows','data_structured')),
    header TEXT,
    deleted_at INTEGER
);

-- 3. Copy with the EXPLICIT chunk_id column to preserve rowids (load-bearing for
--    embeddings.chunk_id and chunks_fts content_rowid).
INSERT INTO chunks_new (chunk_id, node_id, text, byte_start, byte_end, token_count, chunk_kind, header, deleted_at)
    SELECT chunk_id, node_id, text, byte_start, byte_end, token_count, chunk_kind, header, deleted_at
    FROM chunks;

-- 4. Swap. (legacy_alter_table=ON above makes the RENAME tolerate the
--    files_sha256_invalidate trigger's reference to the just-dropped chunks.)
DROP TABLE chunks;
ALTER TABLE chunks_new RENAME TO chunks;
PRAGMA legacy_alter_table=OFF;

-- 5. Recreate indexes + FTS5 sync triggers VERBATIM from 0001_init.sql:94-118.
CREATE INDEX idx_chunks_node ON chunks(node_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_chunks_kind ON chunks(chunk_kind);

CREATE TRIGGER chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, text, header) VALUES (new.chunk_id, new.text, COALESCE(new.header, ''));
END;
CREATE TRIGGER chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text, header)
    VALUES ('delete', old.chunk_id, old.text, COALESCE(old.header, ''));
END;
CREATE TRIGGER chunks_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text, header)
    VALUES ('delete', old.chunk_id, old.text, COALESCE(old.header, ''));
    INSERT INTO chunks_fts(rowid, text, header) VALUES (new.chunk_id, new.text, COALESCE(new.header, ''));
END;

-- 6. Rebuild the external-content FTS index from the renamed table (cheaper and
--    safer than trusting shadow tables across the drop/rename).
INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild');

-- 7. Orphan sweep (D13): embeddings is a vec0 virtual table with NO FK cascade,
--    so any embedding whose chunk_id no longer exists in the rebuilt chunks table
--    would return KNN hits pointing at dead chunks. Delete them explicitly.
DELETE FROM embeddings WHERE chunk_id NOT IN (SELECT chunk_id FROM chunks);

-- 8. FTS integrity-check — raises (aborting the migration transaction) if the
--    rebuilt index is inconsistent with the content table.
INSERT INTO chunks_fts(chunks_fts, rank) VALUES('integrity-check', 1);
