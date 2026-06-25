//go:build cgo

package query

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
)

// fakeAWSKey is a synthetic (non-functional) AWS access key id used to simulate
// a secret that reached the index BEFORE Phase-7 ingest redaction shipped — its
// chunk text was never ingest-redacted, so egress is the only layer that can
// catch it. NEVER a real key.
const fakeAWSKey = "AKIAIOSFODNN7EXAMPLE"

// TestGetChunks_EgressRedactsPreRedactionChunk (MED-3): a chunk whose RAW text
// still carries a secret (a pre-Phase-7 index, or an ingest miss) must be
// redacted at the GetChunks egress path — the low-level IndexQuery RPC that
// previously returned raw text to any same-user client.
func TestGetChunks_EgressRedactsPreRedactionChunk(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.md',X'AA',1,1,'text/markdown','document','/',1)`)
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,provenance,created_at,updated_at)
		VALUES(1,'doc','x','EXTRACTED',1,1)`)
	// Insert a RAW secret directly (bypasses ingest redaction — simulates a
	// pre-Phase-7 chunk).
	mustExec(t, db, `INSERT INTO chunks(node_id,text,byte_start,byte_end,token_count,chunk_kind)
		VALUES(1,?,0,40,5,'prose')`, "deploy with key "+fakeAWSKey+" now")

	h := &ChunksHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetChunksRequest{ChunkIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(resp.Chunks))
	}
	if strings.Contains(resp.Chunks[0].Text, fakeAWSKey) {
		t.Errorf("GetChunks returned raw secret: %q — egress redaction missing", resp.Chunks[0].Text)
	}
	if !strings.Contains(resp.Chunks[0].Text, "[REDACTED]") {
		t.Errorf("GetChunks text not redacted: %q", resp.Chunks[0].Text)
	}
}

// TestGetNodes_EgressRedactsPropertiesJson (MED-3): node PropertiesJson can carry
// a secret in a Go symbol signature (e.g. `const apiKey = "..."`). GetNodes must
// redact it at egress.
func TestGetNodes_EgressRedactsPropertiesJson(t *testing.T) {
	db := openTestDB(t)
	mustExec(t, db, `INSERT INTO files(path,sha256,size,mtime,mime,content_class,parent_dir,last_seen)
		VALUES('/x.go',X'FF',10,1,'text/x-go','code','/',1)`)
	props := `{"signature":"const apiKey = \"` + fakeAWSKey + `\""}`
	mustExec(t, db, `INSERT INTO nodes(file_id,node_type,name,properties,provenance,created_at,updated_at)
		VALUES(1,'const','apiKey',?,'EXTRACTED',1,1)`, props)

	h := &NodesHandler{DB: db}
	resp, err := h.Get(context.Background(), &pb.GetNodesRequest{NodeIds: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}
	if strings.Contains(resp.Nodes[0].PropertiesJson, fakeAWSKey) {
		t.Errorf("GetNodes returned raw secret in PropertiesJson: %q", resp.Nodes[0].PropertiesJson)
	}
	if !strings.Contains(resp.Nodes[0].PropertiesJson, "[REDACTED]") {
		t.Errorf("PropertiesJson not redacted: %q", resp.Nodes[0].PropertiesJson)
	}
	// F1: the egressed properties_json MUST remain valid JSON. The flat-text
	// Sanitizer's greedy patterns (api[_-]?key...\S{10,}, sk-...{20,}) consume the
	// closing quote and brace, producing invalid JSON that breaks any consumer
	// doing json.Unmarshal on properties_json. Structural redaction must keep it
	// parseable.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp.Nodes[0].PropertiesJson), &parsed); err != nil {
		t.Errorf("redacted PropertiesJson is not valid JSON: %v — got %q", err, resp.Nodes[0].PropertiesJson)
	}
}

// TestRedactPropertiesJSON_EmbeddedSecretStaysValidJSON (F1): a secret planted
// inside a properties STRING value must be redacted AND the result must still
// parse as JSON. This is the assertion the original flat-text egress path failed.
func TestRedactPropertiesJSON_EmbeddedSecretStaysValidJSON(t *testing.T) {
	// A signature carrying an api-key assignment — the greedy api[_-]?key pattern
	// would, over the whole flat string, eat the closing quote+brace.
	in := `{"signature":"const apiKey = \"` + fakeAWSKey + `\"","kind":"const","line":42}`
	out := redactPropertiesJSON(in, "node_id", 1)

	if strings.Contains(out, fakeAWSKey) {
		t.Errorf("secret not redacted: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker: %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("redacted output is not valid JSON: %v — got %q", err, out)
	}
	// Non-secret fields must survive structurally intact.
	if parsed["kind"] != "const" {
		t.Errorf("kind field lost or mangled: %#v", parsed["kind"])
	}
	if parsed["line"] != float64(42) {
		t.Errorf("line field lost or mangled: %#v", parsed["line"])
	}
}

// TestRedactPropertiesJSON_NestedAndArrays (F1): the recursive walk must reach
// secrets nested in objects and arrays, and leave the structure valid JSON.
func TestRedactPropertiesJSON_NestedAndArrays(t *testing.T) {
	in := `{"outer":{"token":"sk-` + strings.Repeat("a", 30) + `"},"list":["plain","ghp_` + strings.Repeat("b", 25) + `"]}`
	out := redactPropertiesJSON(in, "node_id", 1)
	if strings.Contains(out, "sk-"+strings.Repeat("a", 30)) {
		t.Errorf("nested object secret not redacted: %q", out)
	}
	if strings.Contains(out, "ghp_"+strings.Repeat("b", 25)) {
		t.Errorf("array element secret not redacted: %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("redacted output is not valid JSON: %v — got %q", err, out)
	}
}

// TestRedactPropertiesJSON_InvalidOrEmptyInput (F1): if the stored properties is
// already not valid JSON, redaction must NOT emit corrupt output — it falls back
// to a valid empty object "{}". Empty input passes through unchanged. No panic.
func TestRedactPropertiesJSON_InvalidOrEmptyInput(t *testing.T) {
	// Already-invalid JSON containing a secret: fall back to "{}" (never leak,
	// never emit corrupt output).
	bad := `not json at all AKIAIOSFODNN7EXAMPLE`
	out := redactPropertiesJSON(bad, "node_id", 1)
	if out != "{}" {
		t.Errorf("invalid input should fall back to {}, got %q", out)
	}
	if strings.Contains(out, fakeAWSKey) {
		t.Errorf("fallback leaked the secret: %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("fallback output is not valid JSON: %v", err)
	}

	// Empty input passes through unchanged.
	if got := redactPropertiesJSON("", "node_id", 1); got != "" {
		t.Errorf("empty input should pass through, got %q", got)
	}
	if got := redactPropertiesJSON("   ", "node_id", 1); got != "   " {
		t.Errorf("whitespace-only input should pass through, got %q", got)
	}
}
