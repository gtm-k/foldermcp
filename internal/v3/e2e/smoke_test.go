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
//	CGO_ENABLED=1 go test -tags 'cgo sqlite_fts5 e2e' -timeout 3m ./internal/v3/e2e/...
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	allCmd := exec.CommandContext(ctx, bin, "all-v3", microFixture)
	allCmd.Env = append(os.Environ(),
		fmt.Sprintf("FOLDERMCP_STORE=%s", storeDir),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("USERPROFILE=%s", homeDir), // Windows compat
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

	// ── Step 2b: Call foldermcp_search ───────────────────
	searchResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "foldermcp_search",
			"arguments": map[string]string{"query": "connection pooling"},
		},
	})
	if searchResp.Error != nil {
		t.Errorf("foldermcp_search error: %s", searchResp.Error.Message)
	} else {
		respStr := string(searchResp.Result)
		t.Logf("search response length: %d bytes", len(respStr))
		if !strings.Contains(respStr, "content") {
			t.Errorf("search response missing 'content' field: %s", truncate(respStr, 500))
		}
	}

	// ── Step 2c: Call foldermcp_inspect ──────────────────
	// The inspect tool requires a node_id (integer) from a prior search result.
	// Parse the search response to extract one; fall back to node_id=1 if parsing fails.
	nodeID := extractNodeID(t, searchResp)
	inspectResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "foldermcp_inspect",
			"arguments": map[string]interface{}{"node_id": nodeID},
		},
	})
	if inspectResp.Error != nil {
		t.Errorf("foldermcp_inspect error: %s", inspectResp.Error.Message)
	} else {
		respStr := string(inspectResp.Result)
		t.Logf("inspect response length: %d bytes", len(respStr))
		if !strings.Contains(respStr, "content") {
			t.Errorf("inspect response missing 'content' field: %s", truncate(respStr, 500))
		}
	}

	// ── Step 2d: Call foldermcp_browse ──────────���────────
	browseResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      "foldermcp_browse",
			"arguments": map[string]string{"path": "code-go"},
		},
	})
	if browseResp.Error != nil {
		t.Errorf("foldermcp_browse error: %s", browseResp.Error.Message)
	} else {
		respStr := string(browseResp.Result)
		t.Logf("browse response length: %d bytes", len(respStr))
		if !strings.Contains(respStr, "content") {
			t.Errorf("browse response missing 'content' field: %s", truncate(respStr, 500))
		}
	}

	t.Log("smoke test complete: all 3 MCP tools responded")
}

// truncate shortens a string for log output.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// extractNodeID attempts to parse a node_id from an MCP search tool response.
// The response Result contains a JSON array of content blocks; we look for
// "node_id" in the text content. Falls back to 1 if parsing fails.
func extractNodeID(t *testing.T, resp jsonRPCResponse) int {
	t.Helper()
	if resp.Error != nil || resp.Result == nil {
		t.Log("extractNodeID: no result to parse, falling back to node_id=1")
		return 1
	}

	// MCP tool results are: {"content": [{"type":"text","text":"..."}]}
	var toolResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &toolResult); err != nil {
		t.Logf("extractNodeID: unmarshal failed: %v, falling back to node_id=1", err)
		return 1
	}

	// Search the text content for a node_id field in JSON
	for _, c := range toolResult.Content {
		// Try to find "node_id": <number> in the text
		var results []map[string]interface{}
		if err := json.Unmarshal([]byte(c.Text), &results); err == nil {
			for _, r := range results {
				if id, ok := r["node_id"]; ok {
					if idFloat, ok := id.(float64); ok && idFloat > 0 {
						t.Logf("extractNodeID: found node_id=%d", int(idFloat))
						return int(idFloat)
					}
				}
			}
		}
		// Also try as a single object
		var single map[string]interface{}
		if err := json.Unmarshal([]byte(c.Text), &single); err == nil {
			if id, ok := single["node_id"]; ok {
				if idFloat, ok := id.(float64); ok && idFloat > 0 {
					t.Logf("extractNodeID: found node_id=%d", int(idFloat))
					return int(idFloat)
				}
			}
		}
	}

	t.Log("extractNodeID: no node_id found in search results, falling back to 1")
	return 1
}
