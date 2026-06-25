//go:build cgo

package query

import (
	"context"
	"strings"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFilenameSearchFindsMatch(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/docs/notes/meeting.md',X'CC',100,1000,'text/markdown','document','/docs/notes/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','meeting.md','EXTRACTED',1,1)`)

	h := &FilenameHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FilenameSearchRequest{Query: "meeting", K: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %d, want 1", len(resp.Results))
	}
}

func TestFilenameSearchEmptyQuery(t *testing.T) {
	db := openTestDB(t)
	h := &FilenameHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.FilenameSearchRequest{Query: "", K: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", resp.Status.Status)
	}
}

func TestMetadataSearchByContentClass(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a.py',X'DD',50,2000,'text/x-python','code','/','1')`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'module','a.py','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/b.md',X'EE',30,1000,'text/markdown','document','/','1')`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(2,'doc','b.md','EXTRACTED',1,1)`)

	h := &MetadataHandler{DB: db}
	resp, err := h.Search(context.Background(), &pb.MetadataSearchRequest{
		ContentClass: "code",
		K:            10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "OK" {
		t.Errorf("status = %s, want OK", resp.Status.Status)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %d, want 1 (only code)", len(resp.Results))
	}
}

func TestGetNodesReturnsSeededNode(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.go',X'FF',10,1,'text/x-go','code','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'function','main','EXTRACTED',1,1)`)

	h := &NodesHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetNodesRequest{NodeIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}
	if resp.Nodes[0].Name != "main" {
		t.Errorf("name = %s, want main", resp.Nodes[0].Name)
	}
}

func TestGetChunksReturnsSeededChunk(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','x','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,'test chunk text',0,15,3,'prose')`)

	h := &ChunksHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetChunksRequest{ChunkIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(resp.Chunks))
	}
	if resp.Chunks[0].Text != "test chunk text" {
		t.Errorf("text = %s", resp.Chunks[0].Text)
	}
}

func TestGraphExpandReturnsStub(t *testing.T) {
	db := openTestDB(t)
	h := &GraphHandler{DB: db}
	resp, err := h.Expand(context.Background(), &pb.GraphExpandRequest{SeedNodeIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status.Status != "EMPTY_BUT_EXECUTED" {
		t.Errorf("status = %s, want EMPTY_BUT_EXECUTED", resp.Status.Status)
	}
}

// TestSoftDeletedFileExcludedFromHydrate (integration FIX 2): A6 watch reconcile
// SOFT-deletes a dropped file (sets files.deleted_at) and HARD-purges only at the
// next reconcile. Between the two, the file's NODES still have deleted_at NULL, so
// hydrateNodes (which filtered only n.deleted_at) and GetNodes still returned its
// content — leaking a deleted file for up to one reconcile interval. The fix adds
// `AND f.deleted_at IS NULL` at the hydrate/GetNodes choke points so exclusion is
// IMMEDIATE the instant reconcile flags the file. This drives a vector + FTS +
// hydrate round-trip plus a direct GetNodes against a soft-deleted file whose node
// has deleted_at NULL and asserts its content is absent from every path.
func TestSoftDeletedFileExcludedFromHydrate(t *testing.T) {
	db := openTestDB(t)

	// file 1: LIVE. file 2: SOFT-DELETED (files.deleted_at set) but its NODE row
	// has deleted_at NULL — exactly the reconcile interim window.
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/live.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen,deleted_at)
		VALUES('/gone.md',X'BB',1,1,'text/markdown','document','/',1,12345)`)
	mustExec(t, db, `INSERT INTO nodes(node_id,file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,1,'doc','live','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO nodes(node_id,file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(2,2,'doc','gone','EXTRACTED',1,1)`) // deleted_at NULL on the node
	mustExec(t, db, `INSERT INTO chunks(chunk_id,node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,1,'shared probe phrase tungsten',0,28,4,'prose')`)
	mustExec(t, db, `INSERT INTO chunks(chunk_id,node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(2,2,'shared probe phrase tungsten',0,28,4,'prose')`)
	vec := make([]byte, 384)
	for i := range vec {
		vec[i] = byte(i % 127)
	}
	mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(1, vec_int8(?))`, vec)
	mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(2, vec_int8(?))`, vec)

	hint := &pb.HydrationHint{Chunks: true, ChunksPerNode: 5}

	// Vector path: the soft-deleted node 2 must NOT enter the candidate set at all
	// (FIX 1b: file/node join filters it BEFORE the LIMIT k, so it never consumes a
	// top-K slot), and the live node 1 must hydrate.
	vh := &VectorHandler{DB: db}
	vresp, err := vh.Search(context.Background(), &pb.VectorSearchRequest{QueryEmbeddingInt8: vec, K: 10, Hydrate: hint})
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	assertSoftDeletedExcludedFromCandidates(t, "vector", vresp.Results)

	// FTS path: same invariant.
	fh := &FTSHandler{DB: db}
	fresp, err := fh.Search(context.Background(), &pb.FTSSearchRequest{Query: "tungsten", K: 10, Hydrate: hint})
	if err != nil {
		t.Fatalf("fts search: %v", err)
	}
	assertSoftDeletedExcludedFromCandidates(t, "fts", fresp.Results)

	// Direct GetNodes by id must also exclude the soft-deleted file's node.
	nh := &NodesHandler{DB: db}
	nresp, err := nh.Get(context.Background(), &pb.GetNodesRequest{NodeIds: []int64{1, 2}, Hydrate: hint})
	if err != nil {
		t.Fatalf("get_nodes: %v", err)
	}
	for _, n := range nresp.Nodes {
		if n.NodeId == 2 {
			t.Errorf("GetNodes returned node 2 whose file is soft-deleted (deleted_at set) — must be excluded immediately")
		}
	}
	if len(nresp.Nodes) != 1 || nresp.Nodes[0].NodeId != 1 {
		t.Errorf("GetNodes nodes = %v, want exactly [node 1]", nodeIDs(nresp.Nodes))
	}

	// FIX 1a: GetChunks by id against the soft-deleted file's chunk (chunk 2, whose
	// own deleted_at is NULL because soft-delete sets ONLY files.deleted_at) must
	// return NOTHING — a client holding chunk IDs must not be able to re-fetch the
	// soft-deleted file's TEXT. The live chunk 1 must still come back.
	ch := &ChunksHandler{DB: db}
	cresp, err := ch.Get(context.Background(), &pb.GetChunksRequest{ChunkIds: []int64{1, 2}})
	if err != nil {
		t.Fatalf("get_chunks: %v", err)
	}
	for _, c := range cresp.Chunks {
		if c.ChunkId == 2 {
			t.Errorf("GetChunks returned chunk 2 whose file is soft-deleted — text leaked: %q", c.Text)
		}
	}
	if len(cresp.Chunks) != 1 || cresp.Chunks[0].ChunkId != 1 {
		t.Errorf("GetChunks chunks = %v, want exactly [chunk 1]", chunkIDs(cresp.Chunks))
	}
}

// assertSoftDeletedExcludedFromCandidates asserts the soft-deleted file's node 2 is
// ABSENT from the result set entirely (FIX 1b — filtered before the LIMIT k, so it
// never consumes a top-K slot), not merely present-but-unhydrated, and that the live
// node 1 IS present and hydrated.
func assertSoftDeletedExcludedFromCandidates(t *testing.T, path string, results []*pb.ScoredNode) {
	t.Helper()
	sawLive := false
	for _, s := range results {
		switch s.NodeId {
		case 1:
			sawLive = true
			if s.Hydrated == nil {
				t.Errorf("%s: live node 1 was not hydrated", path)
			}
		case 2:
			t.Errorf("%s: soft-deleted file's node 2 entered the candidate set — must be filtered before the top-K (hydrated=%v)",
				path, s.Hydrated != nil)
		}
	}
	if !sawLive {
		t.Errorf("%s: live node 1 absent from results — fixture/search problem", path)
	}
}

func chunkIDs(chunks []*pb.HydratedChunk) []int64 {
	out := make([]int64, len(chunks))
	for i, c := range chunks {
		out[i] = c.ChunkId
	}
	return out
}

func nodeIDs(nodes []*pb.HydratedNode) []int64 {
	out := make([]int64, len(nodes))
	for i, n := range nodes {
		out[i] = n.NodeId
	}
	return out
}

// TestMatchedChunkSurfacedInSnippet (FINDING 1, retrieval quality): A4/A5 broke the
// M1 assumption that one node ~= one chunk. A whole PDF/DOCX/CSV is ONE node holding
// MANY chunks. The vector and FTS sources match at the CHUNK level, but the M1 hydrate
// path discarded the matched chunk_id, fetched only `chunksPerNode` chunks ORDER BY
// chunk_id (the title page / schema header), and firstSnippet showed Chunks[0]. A query
// matching chunk #4 of 5 ranked the node correctly but showed chunk #1 and never
// hydrated chunk #4 — the match was invisible. The fix threads the best matched chunk_id
// per node INTERNALLY (no proto change) so (a) the matched chunk IS hydrated and (b) it
// is Chunks[0] so firstSnippet derives the snippet from it.
//
// This drives both the vector and FTS paths against a multi-chunk node where the match
// is on a NON-first chunk, and asserts the matched chunk is present and is Chunks[0].
func TestMatchedChunkSurfacedInSnippet(t *testing.T) {
	db := openTestDB(t)

	// One file/node holding FIVE distinct chunks — a multi-chunk document node (PDF/CSV).
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/big.pdf',X'AA',1,1,'application/pdf','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(node_id,file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,1,'doc','big','EXTRACTED',1,1)`)
	chunkTexts := []string{
		"title page front matter cover",          // chunk 1 (Chunks[0] under ORDER BY chunk_id)
		"table of contents listing sections",     // chunk 2
		"introduction overview background",       // chunk 3
		"deep technical tungsten alloy analysis", // chunk 4 — THE MATCH
		"appendix references bibliography",       // chunk 5
	}
	for i, txt := range chunkTexts {
		mustExec(t, db, `INSERT INTO chunks(chunk_id,node_id,text,byte_start,byte_end,token_count,chunk_kind)
			VALUES(?,1,?,0,?,?,'pdf_text')`, i+1, txt, len(txt), len(txt)/4+1)
	}
	// Embeddings: make chunk 4 the closest match to the query vector and the others
	// progressively farther, so the vector source's best hit for node 1 is chunk 4.
	query := make([]byte, 384)
	for i := range query {
		query[i] = 100
	}
	// chunk_id -> offset from the query vector; chunk 4 (idx 3) is identical (closest).
	offsets := []int{40, 30, 20, 0, 25}
	for i, off := range offsets {
		vec := make([]byte, 384)
		for j := range vec {
			vec[j] = byte(100 + off)
		}
		mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_int8(?))`, i+1, vec)
	}

	hint := &pb.HydrationHint{Chunks: true, ChunksPerNode: 2}

	// --- Vector path: best matched chunk is chunk 4. ---
	vh := &VectorHandler{DB: db}
	vresp, err := vh.Search(context.Background(), &pb.VectorSearchRequest{QueryEmbeddingInt8: query, K: 10, Hydrate: hint})
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	assertMatchedChunkSurfaced(t, "vector", vresp.Results, 1, 4, "deep technical tungsten alloy analysis")

	// --- FTS path: the query term lives ONLY in chunk 4. ---
	fh := &FTSHandler{DB: db}
	fresp, err := fh.Search(context.Background(), &pb.FTSSearchRequest{Query: "tungsten", K: 10, Hydrate: hint})
	if err != nil {
		t.Fatalf("fts search: %v", err)
	}
	assertMatchedChunkSurfaced(t, "fts", fresp.Results, 1, 4, "deep technical tungsten alloy analysis")

	// --- firstSnippet must derive from the matched chunk's text, NOT chunk #1's. ---
	for _, n := range []*pb.HydratedNode{nodeHydrated(vresp.Results, 1), nodeHydrated(fresp.Results, 1)} {
		if n == nil {
			t.Fatal("node 1 not hydrated")
		}
		snip := tools_firstSnippet(n)
		if strings.Contains(snip, "title page") {
			t.Errorf("snippet derived from chunk #1 (title page), want chunk #4: %q", snip)
		}
		if !strings.Contains(snip, "tungsten") {
			t.Errorf("snippet does not contain matched chunk #4 text: %q", snip)
		}
	}
}

// TestSingleChunkNodeUnchanged (FINDING 1 regression): a single-chunk code node (the M1
// common case) has no non-first matched chunk to surface, so it must keep returning its
// one chunk's snippet — the fix must not regress code nodes.
func TestSingleChunkNodeUnchanged(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/a.go',X'BB',1,1,'text/x-go','code','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(node_id,file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,1,'function','main','EXTRACTED',1,1)`)
	mustExec(t, db, `INSERT INTO chunks(chunk_id,node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,1,'func main tungsten body',0,23,4,'code_ast')`)
	vec := make([]byte, 384)
	for i := range vec {
		vec[i] = byte(i % 127)
	}
	mustExec(t, db, `INSERT INTO embeddings(chunk_id, embedding) VALUES(1, vec_int8(?))`, vec)

	hint := &pb.HydrationHint{Chunks: true, ChunksPerNode: 2}
	fh := &FTSHandler{DB: db}
	fresp, err := fh.Search(context.Background(), &pb.FTSSearchRequest{Query: "tungsten", K: 10, Hydrate: hint})
	if err != nil {
		t.Fatalf("fts search: %v", err)
	}
	n := nodeHydrated(fresp.Results, 1)
	if n == nil {
		t.Fatal("single-chunk node 1 not hydrated")
	}
	if len(n.Chunks) != 1 {
		t.Fatalf("single-chunk node hydrated %d chunks, want 1", len(n.Chunks))
	}
	if n.Chunks[0].ChunkId != 1 {
		t.Errorf("single-chunk node Chunks[0] = %d, want 1", n.Chunks[0].ChunkId)
	}
}

// assertMatchedChunkSurfaced asserts the hydrated node INCLUDES the matched chunk and
// that it is Chunks[0] (so any Chunks[0]-based snippet derivation uses the match).
func assertMatchedChunkSurfaced(t *testing.T, path string, results []*pb.ScoredNode, nodeID, matchedChunkID int64, matchedText string) {
	t.Helper()
	n := nodeHydrated(results, nodeID)
	if n == nil {
		t.Fatalf("%s: node %d not present/hydrated in results", path, nodeID)
	}
	var found bool
	for _, c := range n.Chunks {
		if c.ChunkId == matchedChunkID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%s: matched chunk %d NOT in hydrated chunks %v — match is invisible", path, matchedChunkID, hydratedChunkIDs(n.Chunks))
	}
	if len(n.Chunks) == 0 || n.Chunks[0].ChunkId != matchedChunkID {
		t.Errorf("%s: Chunks[0] = %v, want matched chunk %d first (so snippet derives from it)", path, firstChunkID(n.Chunks), matchedChunkID)
	}
	if len(n.Chunks) > 0 && !strings.Contains(n.Chunks[0].Text, "tungsten") && matchedText != "" {
		t.Errorf("%s: Chunks[0].Text = %q, want matched chunk text %q", path, n.Chunks[0].Text, matchedText)
	}
}

func nodeHydrated(results []*pb.ScoredNode, nodeID int64) *pb.HydratedNode {
	for _, s := range results {
		if s.NodeId == nodeID {
			return s.Hydrated
		}
	}
	return nil
}

func hydratedChunkIDs(chunks []*pb.HydratedChunk) []int64 {
	out := make([]int64, len(chunks))
	for i, c := range chunks {
		out[i] = c.ChunkId
	}
	return out
}

func firstChunkID(chunks []*pb.HydratedChunk) int64 {
	if len(chunks) == 0 {
		return -1
	}
	return chunks[0].ChunkId
}

// tools_firstSnippet mirrors tools.firstSnippet's standard-detail behaviour (use
// Chunks[0]) for this in-package assertion, since the real firstSnippet lives in the
// tools package. The fix guarantees Chunks[0] IS the matched chunk.
func tools_firstSnippet(n *pb.HydratedNode) string {
	if len(n.Chunks) == 0 {
		return ""
	}
	return n.Chunks[0].Text
}

func TestGetBlobNotFoundInM1(t *testing.T) {
	db := openTestDB(t)
	h := &BlobsHandler{DB: db}
	_, err := h.Get(context.Background(), &pb.GetBlobRequest{BlobId: 999})
	if err == nil {
		t.Fatal("expected NOT_FOUND error for missing blob")
	}
	// Verify it's a gRPC NOT_FOUND status
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("code = %v, want NotFound", st.Code())
	}
}
