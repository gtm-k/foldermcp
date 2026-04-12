-- 0001_init.sql — v3.0 initial schema. Forward-only; do not modify after release.

-- Config kv store (schema_version lives here)
CREATE TABLE config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Pass lookup (FK target for pipeline_state.pass_name)
CREATE TABLE passes (
    pass_name TEXT PRIMARY KEY
);

INSERT INTO passes(pass_name) VALUES
    ('walker'), ('structural'), ('chunker'), ('embeddings'),
    ('cross_linking'), ('community'), ('ocr'), ('whisper');

-- Walker manifest
CREATE TABLE files (
    file_id INTEGER PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    sha256 BLOB NOT NULL,
    size INTEGER NOT NULL,
    mtime INTEGER NOT NULL,
    mime TEXT NOT NULL,
    content_class TEXT NOT NULL
        CHECK (content_class IN ('code','document','image','media','data','unknown')),
    parent_dir TEXT NOT NULL,
    last_seen INTEGER NOT NULL,
    deleted_at INTEGER
);

CREATE INDEX idx_files_parent_dir ON files(parent_dir);
CREATE INDEX idx_files_content_class ON files(content_class) WHERE deleted_at IS NULL;
CREATE INDEX idx_files_sha256 ON files(sha256);

-- Graph nodes with provenance
CREATE TABLE nodes (
    node_id INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    node_type TEXT NOT NULL,
    name TEXT NOT NULL,
    properties TEXT NOT NULL DEFAULT '{}',  -- JSON
    provenance TEXT NOT NULL
        CHECK (provenance IN ('EXTRACTED','INFERRED','AMBIGUOUS','LLM_INFERRED')),
    confidence REAL,
    community_id INTEGER,              -- NULL in M1 (no Louvain)
    connectivity_rank REAL,            -- NULL in M1
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER,
    -- Generated virtual columns for hot JSON predicates
    language TEXT GENERATED ALWAYS AS (json_extract(properties, '$.language')) VIRTUAL,
    symbol_name TEXT GENERATED ALWAYS AS (json_extract(properties, '$.symbol_name')) VIRTUAL,
    confidence_num REAL GENERATED ALWAYS AS (confidence) VIRTUAL
);

CREATE INDEX idx_nodes_file_id ON nodes(file_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_nodes_node_type ON nodes(node_type);
CREATE INDEX idx_nodes_language ON nodes(language);
CREATE INDEX idx_nodes_symbol_name ON nodes(symbol_name);
CREATE INDEX idx_nodes_provenance ON nodes(provenance);

-- Graph edges
CREATE TABLE edges (
    edge_id INTEGER PRIMARY KEY,
    src_node INTEGER NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    dst_node INTEGER NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    edge_type TEXT NOT NULL,
    properties TEXT NOT NULL DEFAULT '{}',
    provenance TEXT NOT NULL
        CHECK (provenance IN ('EXTRACTED','INFERRED','AMBIGUOUS','LLM_INFERRED')),
    confidence REAL,
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_edges_src ON edges(src_node, edge_type);
CREATE INDEX idx_edges_dst ON edges(dst_node, edge_type);

-- Text chunks
CREATE TABLE chunks (
    chunk_id INTEGER PRIMARY KEY,
    node_id INTEGER NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    byte_start INTEGER NOT NULL,
    byte_end INTEGER NOT NULL,
    token_count INTEGER NOT NULL,
    chunk_kind TEXT NOT NULL
        CHECK (chunk_kind IN ('prose','code_ast','pdf_text','code_fallback')),
    header TEXT,
    deleted_at INTEGER
);

CREATE INDEX idx_chunks_node ON chunks(node_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_chunks_kind ON chunks(chunk_kind);

-- FTS5 external-content virtual table (same txn as chunks)
CREATE VIRTUAL TABLE chunks_fts USING fts5(
    text,
    header,
    content='chunks',
    content_rowid='chunk_id',
    tokenize='porter unicode61'
);

-- FTS5 sync triggers
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

-- Communities (populated in M2)
CREATE TABLE communities (
    community_id INTEGER PRIMARY KEY,
    size INTEGER NOT NULL,
    density REAL NOT NULL,
    label TEXT,
    parent_community_id INTEGER REFERENCES communities(community_id)
);

-- Blob metadata (OCR text, thumbnails, transcripts — mostly empty in M1)
CREATE TABLE blobs (
    blob_id INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    blob_type TEXT NOT NULL
        CHECK (blob_type IN ('ocr_text','thumbnail','transcript','pdf_text')),
    cache_path TEXT NOT NULL,
    sha256 BLOB NOT NULL,
    size INTEGER NOT NULL
);

CREATE INDEX idx_blobs_file ON blobs(file_id, blob_type);

-- Refcounted GC for blobs
CREATE TABLE blob_refs (
    blob_id INTEGER PRIMARY KEY REFERENCES blobs(blob_id) ON DELETE CASCADE,
    refcount INTEGER NOT NULL CHECK (refcount >= 0),
    updated_at INTEGER NOT NULL
);

-- Per-(file, pass) checkpoints
CREATE TABLE pipeline_state (
    file_id INTEGER NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    pass_name TEXT NOT NULL REFERENCES passes(pass_name),
    status TEXT NOT NULL
        CHECK (status IN ('pending','running','done','failed','skipped')),
    checkpoint_at INTEGER NOT NULL,
    error_message TEXT,
    PRIMARY KEY (file_id, pass_name)
);

CREATE INDEX idx_pipeline_state_status ON pipeline_state(pass_name, status);

-- Embedding fingerprint (rebuild-vectors check)
CREATE TABLE embedding_fingerprint (
    id INTEGER PRIMARY KEY CHECK (id = 1),  -- singleton row
    model_name TEXT NOT NULL,
    model_version TEXT NOT NULL,
    dimension INTEGER NOT NULL,
    chunking_policy TEXT NOT NULL,
    quantization_mode TEXT NOT NULL
        CHECK (quantization_mode IN ('int8','float32')),
    created_at INTEGER NOT NULL
);

-- LLM enrichments (append-only, v3.1+; empty in M1)
CREATE TABLE llm_enrichments (
    enrichment_id INTEGER PRIMARY KEY,
    node_id INTEGER NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
    model_name TEXT NOT NULL,
    run_id TEXT NOT NULL,
    properties TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_llm_run ON llm_enrichments(run_id);

-- sqlite-vec virtual table is created at runtime via store/open.go
-- after sqlite-vec is loaded as an extension (see Task B7).

-- Record initial schema version
INSERT INTO config(key, value) VALUES ('schema_version', '1');
INSERT INTO config(key, value) VALUES ('instance_uuid', '');
INSERT INTO config(key, value) VALUES ('blob_availability', 'unknown');
