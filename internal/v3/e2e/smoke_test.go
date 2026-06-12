//go:build cgo && e2e

// Package e2e contains end-to-end smoke tests for the foldermcp v3 pipeline.
//
// These tests require:
//   - cgo + sqlite_fts5 build tags (indexer depends on mattn/go-sqlite3)
//   - Unix domain sockets (Linux/macOS only; Windows is unsupported)
//   - A pre-built foldermcp binary at $FOLDERMCP_BIN or in PATH
//   - The micro-fixture at testdata/v3/micro/ relative to the repo root
//
// Run:
//
//	CGO_ENABLED=1 go test -tags 'cgo sqlite_fts5 e2e' -timeout 8m ./internal/v3/e2e/...
//
// G37 (tightened, D28b D6): the test asserts non-empty results plus a
// judgment-path substring for three graded known-hit queries from
// testdata/v3/micro_labeled_queries.yaml — it therefore needs the real
// embedding model at internal/v3/embed/model/ (run `make v3-fetch-model`)
// and an ONNX Runtime shared library (default dlopen("onnxruntime.so"),
// or set FOLDERMCP_ORT_LIB).
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gtm-k/foldermcp/internal/v3/store"
)

// findBinary locates the foldermcp binary. Checks:
// 1. $FOLDERMCP_BIN environment variable
// 2. bin/foldermcp relative to the repo root
// 3. foldermcp in PATH
func findBinary(t *testing.T) string {
	t.Helper()

	if bin := os.Getenv("FOLDERMCP_BIN"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}

	// Walk up from the test directory to find the repo root
	repoRoot := findRepoRoot(t)
	localBin := filepath.Join(repoRoot, "bin", "foldermcp")
	if _, err := os.Stat(localBin); err == nil {
		return localBin
	}

	// Fallback to PATH
	path, err := exec.LookPath("foldermcp")
	if err != nil {
		t.Fatalf("foldermcp binary not found: set FOLDERMCP_BIN or build to bin/foldermcp")
	}
	return path
}

// findRepoRoot walks up from the current working directory to find go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found (no go.mod)")
		}
		dir = parent
	}
}

// waitForSocket polls until a Unix domain socket accepts connections.
func waitForSocket(t *testing.T, sockPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("socket %s not ready after %v", sockPath, timeout)
}

// jsonRPCRequest is a minimal JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is a minimal JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// sendJSONRPC writes a request to stdin and reads a response from stdout.
func sendJSONRPC(t *testing.T, stdin io.Writer, scanner *bufio.Scanner, req jsonRPCRequest) jsonRPCResponse {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(stdin, "%s\n", data); err != nil {
		t.Fatalf("write request: %v", err)
	}

	// Read lines until we get a valid JSON-RPC response with matching ID.
	// The MCP server may emit notifications or other messages before responding.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				t.Fatalf("read response: %v", err)
			}
			t.Fatalf("unexpected EOF while waiting for response to id=%d", req.ID)
		}
		line := scanner.Text()
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			// Not a JSON line — skip (could be stderr bleed or notification)
			continue
		}
		if resp.ID == req.ID {
			return resp
		}
		// Response for a different ID — skip (could be a notification with id=0)
	}
	t.Fatalf("timeout waiting for JSON-RPC response to id=%d", req.ID)
	return jsonRPCResponse{} // unreachable
}

// g37Queries are the three graded known-hit queries (grade=2) from
// testdata/v3/micro_labeled_queries.yaml (D28b D6): they span
// code-python / code-go / docs-markdown. Each must return ≥1 result
// with the judgment path among the hits.
var g37Queries = []struct {
	id       string
	query    string
	wantPath string
}{
	{"m002", "connection pooling with configurable timeouts", "code-python/session_manager.py"},
	{"m008", "hierarchical YAML configuration with environment overrides", "code-go/config.go"},
	{"m013", "system architecture components and data flow", "docs-markdown/architecture.md"},
}

// toolEnvelope is the MCP tools/call result wrapper.
type toolEnvelope struct {
	IsError bool `json:"isError"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// searchPayload mirrors router.SearchResult (the JSON inside content[0].text).
type searchPayload struct {
	Status  string `json:"status"`
	Results []struct {
		NodeID int64  `json:"node_id"`
		Path   string `json:"path"`
	} `json:"results"`
}

func TestE2EAllSubcommandSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e smoke test in short mode")
	}

	bin := findBinary(t)
	repoRoot := findRepoRoot(t)
	microFixture := filepath.Join(repoRoot, "testdata", "v3", "micro")

	if _, err := os.Stat(microFixture); err != nil {
		t.Fatalf("micro-fixture not found at %s", microFixture)
	}

	// The tightened G37 assertions need a real index, which needs the
	// embedding model. Fail (not skip) when absent: skipping would
	// re-mask the zero-chunk failure this test exists to catch.
	modelDir := filepath.Join(repoRoot, "internal", "v3", "embed", "model")
	for _, f := range []string{"model.onnx", "tokenizer.json"} {
		if _, err := os.Stat(filepath.Join(modelDir, f)); err != nil {
			t.Fatalf("embedding model file %s missing in %s — run `make v3-fetch-model`", f, modelDir)
		}
	}

	// Use a temp directory for the store and socket to avoid conflicts
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "store")
	runDir := filepath.Join(tmpDir, "run")
	if err := os.MkdirAll(runDir, 0700); err != nil {
		t.Fatalf("mkdir run: %v", err)
	}
	sockPath := filepath.Join(runDir, "serve.sock")

	// The all-v3 command reads FOLDERMCP_STORE for the store path, but the
	// socket path is hardcoded to ~/.foldermcp/run/serve.sock. For isolation
	// we override HOME so both paths land in tmpDir.
	homeDir := filepath.Join(tmpDir, "home")
	homeFolderMCPDir := filepath.Join(homeDir, ".foldermcp", "run")
	if err := os.MkdirAll(homeFolderMCPDir, 0700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	sockPath = filepath.Join(homeFolderMCPDir, "serve.sock")

	// ── Phase 1: Start foldermcp all-v3 ─────────────────
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	allCmd := exec.CommandContext(ctx, bin, "all-v3", microFixture)
	allCmd.Env = append(os.Environ(),
		fmt.Sprintf("FOLDERMCP_STORE=%s", storeDir),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("USERPROFILE=%s", homeDir), // Windows compat
		// HOME is overridden above for isolation, so the binary cannot
		// find the model via its ~/.foldermcp default — point it at the
		// repo-local model dir. FOLDERMCP_ORT_LIB (if set) passes
		// through via os.Environ().
		fmt.Sprintf("FOLDERMCP_MODEL_DIR=%s", modelDir),
	)
	allCmd.Stderr = os.Stderr // pipe server logs to test output
	if err := allCmd.Start(); err != nil {
		t.Fatalf("start all-v3: %v", err)
	}
	defer func() {
		_ = allCmd.Process.Kill()
		_ = allCmd.Wait()
	}()

	t.Logf("all-v3 started (pid %d), waiting for socket at %s", allCmd.Process.Pid, sockPath)
	waitForSocket(t, sockPath, 45*time.Second)
	t.Log("socket ready")

	// ── Phase 2: Start MCP shim and send tool calls ─────
	mcpCmd := exec.CommandContext(ctx, bin, "mcp-v3")
	mcpCmd.Env = append(os.Environ(),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("USERPROFILE=%s", homeDir),
	)
	mcpStdin, err := mcpCmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	mcpStdout, err := mcpCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	mcpCmd.Stderr = os.Stderr
	if err := mcpCmd.Start(); err != nil {
		t.Fatalf("start mcp-v3: %v", err)
	}
	defer func() {
		_ = mcpStdin.Close()
		_ = mcpCmd.Process.Kill()
		_ = mcpCmd.Wait()
	}()

	scanner := bufio.NewScanner(mcpStdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MB buffer for large responses

	// ── Step 2a: Initialize MCP session ─────────────────
	initResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]string{"name": "smoke-test", "version": "1.0"},
			"capabilities":    map[string]interface{}{},
		},
	})
	if initResp.Error != nil {
		t.Fatalf("initialize error: %s", initResp.Error.Message)
	}
	t.Log("MCP initialized")

	// Send initialized notification (required by MCP protocol)
	initNotif, _ := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})
	fmt.Fprintf(mcpStdin, "%s\n", initNotif)

	reqID := 1 // initialize already used id=1
	nextID := func() int { reqID++; return reqID }

	search := func(query string) (searchPayload, error) {
		resp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      nextID(),
			Method:  "tools/call",
			Params: map[string]interface{}{
				"name":      "foldermcp_search",
				"arguments": map[string]string{"query": query},
			},
		})
		if resp.Error != nil {
			return searchPayload{}, fmt.Errorf("rpc error: %s", resp.Error.Message)
		}
		var env toolEnvelope
		if err := json.Unmarshal(resp.Result, &env); err != nil {
			return searchPayload{}, fmt.Errorf("parse envelope: %v in %s", err, truncate(string(resp.Result), 300))
		}
		if env.IsError || len(env.Content) == 0 {
			return searchPayload{}, fmt.Errorf("tool error: %s", truncate(string(resp.Result), 300))
		}
		var p searchPayload
		if err := json.Unmarshal([]byte(env.Content[0].Text), &p); err != nil {
			return searchPayload{}, fmt.Errorf("parse payload: %v in %s", err, truncate(env.Content[0].Text, 300))
		}
		return p, nil
	}
	hasPathHit := func(p searchPayload, want string) bool {
		for _, r := range p.Results {
			if strings.Contains(r.Path, want) {
				return true
			}
		}
		return false
	}

	// ── Step 2b: G37 — three known-hit queries (D28b D6) ─
	// The indexer runs in the background; poll until all three graded
	// queries hit (first run pays ONNX init + per-chunk embedding), then
	// assert. A walker-only binary indexes nothing and can never satisfy
	// this — the pre-D28b `"content"`-substring mask is gone.
	lastResults := make(map[string]searchPayload, len(g37Queries))
	lastErrs := make(map[string]error, len(g37Queries))
	deadline := time.Now().Add(150 * time.Second)
	for {
		allGood := true
		for _, q := range g37Queries {
			p, err := search(q.query)
			lastResults[q.id], lastErrs[q.id] = p, err
			if err != nil || !hasPathHit(p, q.wantPath) {
				allGood = false
			}
		}
		if allGood || time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	for _, q := range g37Queries {
		p := lastResults[q.id]
		if err := lastErrs[q.id]; err != nil {
			t.Errorf("G37 %s (%q): search failed: %v", q.id, q.query, err)
			continue
		}
		if len(p.Results) == 0 {
			t.Errorf("G37 %s (%q): 0 results, want > 0", q.id, q.query)
			continue
		}
		if !hasPathHit(p, q.wantPath) {
			paths := make([]string, 0, len(p.Results))
			for _, r := range p.Results {
				paths = append(paths, r.Path)
			}
			t.Errorf("G37 %s (%q): no result path contains %q; got %v", q.id, q.query, q.wantPath, paths)
		} else {
			t.Logf("G37 %s: %d results, judgment path %q hit", q.id, len(p.Results), q.wantPath)
		}
	}

	// ── Step 2b': Phase 2 AC — non-zero chunks AND embeddings ─
	// Command-level assertion (Codex E2 finding #1): open the store DB
	// the all-v3 process wrote and count rows directly, so a pipeline
	// that returns search hits without persisting chunks/embeddings
	// cannot pass. store.Open registers sqlite-vec as an auto-extension
	// at package init, which the embeddings vec0 virtual table needs.
	storeDB, err := store.Open(store.Options{
		Path:     filepath.Join(storeDir, "index.db"),
		Tier:     store.TierMid,
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("open store db for count assertions: %v", err)
	}
	defer func() { _ = storeDB.Close() }()
	var chunkCount, embeddingCount int
	if err := storeDB.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&chunkCount); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM chunks: %v", err)
	}
	if err := storeDB.QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&embeddingCount); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM embeddings: %v", err)
	}
	if chunkCount == 0 {
		t.Error("chunks count = 0, want > 0 (Phase 2 AC)")
	}
	if embeddingCount == 0 {
		t.Error("embeddings count = 0, want > 0 (Phase 2 AC)")
	}
	t.Logf("store counts: chunks=%d embeddings=%d", chunkCount, embeddingCount)

	// ── Step 2c: Call foldermcp_inspect ──────────────────
	// node_id comes from m002's results (D28b D6). On a correctly wired
	// pipeline the fallback must not fire; warn (don't fail) if it does,
	// leaving room for node-id ordering drift.
	nodeID := 1
	if m002 := lastResults["m002"]; len(m002.Results) > 0 && m002.Results[0].NodeID > 0 {
		nodeID = int(m002.Results[0].NodeID)
	} else {
		t.Log("WARNING: no node_id extracted from m002 results, falling back to node_id=1")
	}
	inspectResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextID(),
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "foldermcp_inspect",
			"arguments": map[string]interface{}{"node_id": nodeID},
		},
	})
	if inspectResp.Error != nil {
		t.Errorf("foldermcp_inspect error: %s", inspectResp.Error.Message)
	} else {
		var env toolEnvelope
		if err := json.Unmarshal(inspectResp.Result, &env); err != nil || env.IsError || len(env.Content) == 0 {
			t.Errorf("inspect: bad envelope (err=%v): %s", err, truncate(string(inspectResp.Result), 500))
		} else {
			var p struct {
				Status string `json:"status"`
				Node   *struct {
					NodeID int64  `json:"node_id"`
					Path   string `json:"path"`
				} `json:"node"`
			}
			if err := json.Unmarshal([]byte(env.Content[0].Text), &p); err != nil {
				t.Errorf("inspect: parse payload: %v in %s", err, truncate(env.Content[0].Text, 500))
			} else if p.Node == nil {
				t.Errorf("inspect node_id=%d: nil node in payload %s", nodeID, truncate(env.Content[0].Text, 500))
			} else {
				t.Logf("inspect: node_id=%d path=%s", p.Node.NodeID, p.Node.Path)
			}
		}
	}

	// ── Step 2d: Call foldermcp_browse ───────────────────
	// Browse matches files.parent_dir exactly, so pass the absolute
	// fixture subfolder. D28b D6: ≥1 child under code-go.
	browseResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextID(),
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "foldermcp_browse",
			"arguments": map[string]string{"path": filepath.Join(microFixture, "code-go")},
		},
	})
	if browseResp.Error != nil {
		t.Errorf("foldermcp_browse error: %s", browseResp.Error.Message)
	} else {
		var env toolEnvelope
		if err := json.Unmarshal(browseResp.Result, &env); err != nil || env.IsError || len(env.Content) == 0 {
			t.Errorf("browse: bad envelope (err=%v): %s", err, truncate(string(browseResp.Result), 500))
		} else {
			var p struct {
				Status  string `json:"status"`
				Entries []struct {
					Path string `json:"path"`
					Name string `json:"name"`
				} `json:"entries"`
			}
			if err := json.Unmarshal([]byte(env.Content[0].Text), &p); err != nil {
				t.Errorf("browse: parse payload: %v in %s", err, truncate(env.Content[0].Text, 500))
			} else if len(p.Entries) == 0 {
				t.Errorf("browse code-go: 0 entries, want ≥ 1 (payload %s)", truncate(env.Content[0].Text, 500))
			} else {
				t.Logf("browse code-go: %d entries", len(p.Entries))
			}
		}
	}

	t.Log("smoke test complete: all 3 MCP tools responded with tightened G37 assertions and non-zero store counts")
}

// truncate shortens a string for log output.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

