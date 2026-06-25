//go:build cgo && e2e

// Watch-mode end-to-end tests (Phase 6 / D9, D13, D14, D15).
//
// These are INTEGRATION-DEFERRED: they require a live daemon + the gitignored
// ONNX embedding model and so are NOT part of the in-worktree gating suite. The
// drain/dedup/hash-short-circuit/delete-chain/reconciliation/startup-sweep
// logic is covered deterministically by the unit tests in
// internal/v3/pipeline/watch_test.go (no daemon, no model). These e2e tests
// validate the wall-clock freshness SLO and the watcher→pipeline integration.
//
// Run (Linux/macOS, with the model present):
//
//	CGO_ENABLED=1 FOLDERMCP_MODEL_DIR=/path/to/internal/v3/embed/model \
//	  go test -tags 'cgo sqlite_fts5 e2e' -timeout 8m -run TestWatch ./internal/v3/e2e/...
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// startWatchDaemon launches `all-v3 --watch` over a fresh copy of the micro
// fixture in a temp dir (so the test can edit/add/delete files) and an MCP shim
// for querying. Returns the workspace dir, an MCP search function, and a
// cleanup. Shared scaffolding for the watch e2e tests below.
//
// NOTE (integration-deferred): this helper is written against the existing
// smoke-test harness (findBinary/findRepoRoot/waitForSocket/sendJSONRPC) and
// the all-v3 --watch flag added in Phase 6. It needs the real model dir, hence
// the e2e build tag.
func startWatchDaemon(t *testing.T) (workspace string, search func(string) (searchPayload, error), cleanup func()) {
	t.Helper()
	bin := findBinary(t)
	repoRoot := findRepoRoot(t)
	modelDir := filepath.Join(repoRoot, "internal", "v3", "embed", "model")
	for _, f := range []string{"model.onnx", "tokenizer.json"} {
		if _, err := os.Stat(filepath.Join(modelDir, f)); err != nil {
			t.Fatalf("embedding model file %s missing in %s — run `make v3-fetch-model`", f, modelDir)
		}
	}

	tmpDir := t.TempDir()
	workspace = filepath.Join(tmpDir, "ws")
	storeDir := filepath.Join(tmpDir, "store")
	homeDir := filepath.Join(tmpDir, "home")
	homeRun := filepath.Join(homeDir, ".foldermcp", "run")
	for _, d := range []string{workspace, storeDir, homeRun} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	sockPath := filepath.Join(homeRun, "serve.sock")

	// Seed one file so the initial index has content.
	if err := os.WriteFile(filepath.Join(workspace, "seed.md"),
		[]byte("# Seed\n\nInitial document about connection pooling.\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)

	allCmd := exec.CommandContext(ctx, bin, "all-v3", "--watch", "--watch-interval", "1s", workspace)
	allCmd.Env = append(os.Environ(),
		fmt.Sprintf("FOLDERMCP_STORE=%s", storeDir),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("USERPROFILE=%s", homeDir),
		fmt.Sprintf("FOLDERMCP_MODEL_DIR=%s", modelDir),
	)
	allCmd.Stderr = os.Stderr
	if err := allCmd.Start(); err != nil {
		cancel()
		t.Fatalf("start all-v3 --watch: %v", err)
	}
	waitForSocket(t, sockPath, 45*time.Second)

	mcpCmd := exec.CommandContext(ctx, bin, "mcp-v3")
	mcpCmd.Env = append(os.Environ(),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("USERPROFILE=%s", homeDir),
	)
	mcpStdin, _ := mcpCmd.StdinPipe()
	mcpStdout, _ := mcpCmd.StdoutPipe()
	mcpCmd.Stderr = os.Stderr
	if err := mcpCmd.Start(); err != nil {
		cancel()
		t.Fatalf("start mcp-v3: %v", err)
	}
	scanner := bufio.NewScanner(mcpStdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	initResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]string{"name": "watch-e2e", "version": "1.0"},
			"capabilities":    map[string]interface{}{},
		},
	})
	if initResp.Error != nil {
		cancel()
		t.Fatalf("initialize: %s", initResp.Error.Message)
	}
	initNotif, _ := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	fmt.Fprintf(mcpStdin, "%s\n", initNotif)

	reqID := 1
	search = func(query string) (searchPayload, error) {
		reqID++
		resp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
			JSONRPC: "2.0", ID: reqID, Method: "tools/call",
			Params: map[string]interface{}{
				"name":      "foldermcp_search",
				"arguments": map[string]string{"query": query},
			},
		})
		if resp.Error != nil {
			return searchPayload{}, fmt.Errorf("search rpc: %s", resp.Error.Message)
		}
		var env toolEnvelope
		if err := json.Unmarshal(resp.Result, &env); err != nil {
			return searchPayload{}, fmt.Errorf("parse envelope: %v", err)
		}
		if env.IsError || len(env.Content) == 0 {
			return searchPayload{}, fmt.Errorf("tool error: %s", truncate(string(resp.Result), 300))
		}
		var p searchPayload
		if err := json.Unmarshal([]byte(env.Content[0].Text), &p); err != nil {
			return searchPayload{}, fmt.Errorf("parse payload: %v", err)
		}
		return p, nil
	}

	cleanup = func() {
		_ = mcpStdin.Close()
		_ = mcpCmd.Process.Kill()
		_ = mcpCmd.Wait()
		_ = allCmd.Process.Kill()
		_ = allCmd.Wait()
		cancel()
	}
	return workspace, search, cleanup
}

// TestWatch_FreshnessUnder10s (DEFERRED): edit a file, assert it is searchable
// within 10s (Phase 6 freshness SLO, local target). NAS <30s is Phase 10 on the
// DS923+.
func TestWatch_FreshnessUnder10s(t *testing.T) {
	t.Skip("integration-deferred: needs live daemon + ONNX model; gating coverage in pipeline/watch_test.go")

	workspace, search, cleanup := startWatchDaemon(t)
	defer cleanup()

	// A distinctive term that is NOT in the seed file.
	const needle = "xylophone-quasar-marker"
	p := filepath.Join(workspace, "fresh.md")
	if err := os.WriteFile(p, []byte("# Fresh\n\nA paragraph containing "+needle+" for freshness.\n"), 0o644); err != nil {
		t.Fatalf("write fresh: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := search(needle)
		if err == nil && len(res.Results) > 0 {
			return // visible within 10s
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("edited file not searchable within 10s (freshness SLO violated)")
}

// TestWatch_StormMtimeOnly (DEFERRED): touch many files (mtime-only) + edit a
// few; assert only the truly-changed files re-embed (hash short-circuit, D9).
// Asserted in gating via TestWatch_MtimeOnlyChangeDoesNotMarkPending; this is
// the live-daemon storm variant from the plan's acceptance list.
func TestWatch_StormMtimeOnly(t *testing.T) {
	t.Skip("integration-deferred: needs live daemon + ONNX model; mtime short-circuit covered in pipeline/watch_test.go")
}

// TestWatch_DeletePurgeLive (DEFERRED): delete a file; assert it disappears
// from results within one tick and leaves zero orphaned embeddings. The
// explicit delete chain + zero-orphan invariant are asserted in gating via
// TestWatch_DeleteChainRemovesEmbeddings; this is the live-daemon variant.
func TestWatch_DeletePurgeLive(t *testing.T) {
	t.Skip("integration-deferred: needs live daemon + ONNX model; delete chain covered in pipeline/watch_test.go")
}

// TestWatch_RaceDeleteReindexHydrate (DEFERRED, D15): delete + re-index while
// queries hydrate concurrently under -race (≥100 iterations); zero panics, any
// affected response carries completeness=partial, no result references a
// nonexistent chunk. Depends on the shared-hydration index_updating annotation
// (grpc/tools — owned by a different phase per the A6 scope fence), so it is
// deferred until that lands.
func TestWatch_RaceDeleteReindexHydrate(t *testing.T) {
	t.Skip("integration-deferred: needs live daemon, ONNX model, AND the D15 shared-hydration index_updating annotation (grpc/tools, out of A6 scope)")
}

// TestWatch_L4bUnderWatchLoad (DEFERRED): re-run the L4b concurrency + G40
// kill-trial harness with the watch loop writing while readers query.
func TestWatch_L4bUnderWatchLoad(t *testing.T) {
	t.Skip("integration-deferred: needs live daemon + ONNX model; re-runs L4b/kill-trial under watch-mode write load")
}

// TestWatch_NASDisconnectGraceful (DEFERRED): simulate a polling failure
// (unreadable root) mid-watch; assert no crash, a WARN, and resume on
// reconnect. The watch loop already surfaces ConsecutiveFailures()>=3 at WARN.
func TestWatch_NASDisconnectGraceful(t *testing.T) {
	t.Skip("integration-deferred: needs live daemon; disconnect resilience lives in internal/watcher + watch-loop WARN path")
}
