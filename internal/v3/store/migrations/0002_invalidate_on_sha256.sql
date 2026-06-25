-- 0002_invalidate_on_sha256.sql — D28b (D8 Layer 1). Forward-only.
--
-- Purpose: when a file's sha256 changes (content was modified since last
-- index), its downstream graph must invalidate so the Runner reprocesses
-- it on its next pass. Schema-level invariant prevents any future code
-- path from sneaking in a stale read.
--
-- Statement order is load-bearing (D28b plan, Amendment 2026-06-11):
-- embeddings is a vec0 VIRTUAL table (created at runtime in store/open.go)
-- and sits outside SQLite's foreign-key graph — nothing cascades to it.
-- The embeddings delete must therefore run FIRST, while the chunk rows
-- that identify the orphaned vectors still exist. nodes delete then
-- cascades to chunks via FK ON DELETE CASCADE, and chunks_fts auto-syncs
-- via the chunks_ad trigger from migration 0001.
--
-- vec0 DML inside a trigger body was verified empirically against this
-- build (mattn/go-sqlite3 v1.14.42 + sqlite-vec v0.1.6) on 2026-06-11:
-- both the parse-time CREATE TRIGGER and the runtime UPSERT-fired DELETE
-- succeed. CI gate: TestSha256ChangeFiresInvalidationTrigger.
--
-- IF NOT EXISTS is defence-in-depth: the migration runner (migrate.go)
-- applies each migration at most once per DB, but the clause tolerates
-- ad-hoc replay (tests, manual sqlite3 sessions) at zero cost.

CREATE TRIGGER IF NOT EXISTS files_sha256_invalidate
BEFORE UPDATE OF sha256 ON files
WHEN NEW.sha256 != OLD.sha256
BEGIN
    DELETE FROM embeddings WHERE chunk_id IN (
      SELECT chunk_id FROM chunks WHERE node_id IN (
        SELECT node_id FROM nodes WHERE file_id = OLD.file_id));
    DELETE FROM pipeline_state WHERE file_id = OLD.file_id;
    DELETE FROM nodes WHERE file_id = OLD.file_id;
END;
