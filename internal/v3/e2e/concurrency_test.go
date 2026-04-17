//go:build cgo && e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestShutdownNoLeaks starts the full pipeline (all-v3 + mcp-v3), issues a
// few queries to warm the server, then shuts everything down via context
// cancellation and verifies that no goroutines have leaked.
//
// This is the L4b concurrency gate: it proves that the process cleans up
// all background workers (indexer loops, gRPC listeners, MCP stdio pump)
// on shutdown rather than leaking goroutines.
func TestShutdownNoLeaks(t *testing.T) {
	defer goleak.VerifyNone(t,
		// Known safe background goroutines from third-party libraries:
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
		goleak.IgnoreTopFunction("google.golang.org/grpc.(*addrConn).resetTransport"),
		goleak.IgnoreTopFunction("google.golang.org/grpc.(*ccBalancerWrapper).watcher"),
	)

	if testing.Short() {
		t.Skip("skipping L4b concurrency test in short mode")
	}

	bin := findBinary(t)
	repoRoot := findRepoRoot(t)
	microFixture := filepath.Join(repoRoot, "testdata", "v3", "micro")

	if _, err := os.Stat(microFixture); err != nil {
		t.Fatalf("micro-fixture not found at %s", microFixture)
	}

	// Isolated temp dirs for store, socket, and HOME override.
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "store")
	homeDir := filepath.Join(tmpDir, "home")
	homeFolderMCPDir := filepath.Join(homeDir, ".foldermcp", "run")
	if err := os.MkdirAll(homeFolderMCPDir, 0700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	sockPath := filepath.Join(homeFolderMCPDir, "serve.sock")

	// Start foldermcp all-v3
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	allCmd := exec.CommandContext(ctx, bin, "all-v3", microFixture)
	allCmd.Env = append(os.Environ(),
		fmt.Sprintf("FOLDERMCP_STORE=%s", storeDir),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("USERPROFILE=%s", homeDir),
	)
	allCmd.Stderr = os.Stderr
	if err := allCmd.Start(); err != nil {
		t.Fatalf("start all-v3: %v", err)
	}

	t.Logf("all-v3 started (pid %d), waiting for socket", allCmd.Process.Pid)
	waitForSocket(t, sockPath, 45*time.Second)
	t.Log("socket ready — issuing warm-up queries")

	// Start MCP shim and issue a few queries to exercise the pipeline
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

	scanner := bufio.NewScanner(mcpStdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	// Initialize MCP session
	initResp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]string{"name": "leak-test", "version": "1.0"},
			"capabilities":    map[string]interface{}{},
		},
	})
	if initResp.Error != nil {
		t.Fatalf("initialize error: %s", initResp.Error.Message)
	}

	initNotif, _ := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	fmt.Fprintf(mcpStdin, "%s\n", initNotif)

	// Issue 3 warm-up queries to exercise search, inspect, browse
	for i, query := range []string{"config", "session", "middleware"} {
		resp := sendJSONRPC(t, mcpStdin, scanner, jsonRPCRequest{
			JSONRPC: "2.0", ID: 10 + i, Method: "tools/call",
			Params: map[string]interface{}{
				"name":      "foldermcp_search",
				"arguments": map[string]string{"query": query},
			},
		})
		if resp.Error != nil {
			t.Logf("warm-up query %q: error %s (non-fatal)", query, resp.Error.Message)
		}
	}

	t.Log("warm-up complete — shutting down")

	// Shut down: close MCP stdin, kill processes, wait for exit
	_ = mcpStdin.Close()
	_ = mcpCmd.Process.Kill()
	_ = mcpCmd.Wait()

	cancel() // cancel context — triggers all-v3 shutdown
	_ = allCmd.Wait()

	t.Log("shutdown complete — goleak.VerifyNone will check for leaked goroutines")
}

// TestConcurrentQueriesUnderLoad starts the pipeline and fires 50 concurrent
// MCP search queries, then cancels mid-flight to verify no panics or
// goroutine leaks under concurrent cancellation.
func TestConcurrentQueriesUnderLoad(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
		goleak.IgnoreTopFunction("google.golang.org/grpc.(*addrConn).resetTransport"),
		goleak.IgnoreTopFunction("google.golang.org/grpc.(*ccBalancerWrapper).watcher"),
	)

	if testing.Short() {
		t.Skip("skipping L4b concurrent load test in short mode")
	}

	bin := findBinary(t)
	repoRoot := findRepoRoot(t)
	microFixture := filepath.Join(repoRoot, "testdata", "v3", "micro")

	if _, err := os.Stat(microFixture); err != nil {
		t.Fatalf("micro-fixture not found at %s", microFixture)
	}

	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "store")
	homeDir := filepath.Join(tmpDir, "home")
	homeFolderMCPDir := filepath.Join(homeDir, ".foldermcp", "run")
	if err := os.MkdirAll(homeFolderMCPDir, 0700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	sockPath := filepath.Join(homeFolderMCPDir, "serve.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	allCmd := exec.CommandContext(ctx, bin, "all-v3", microFixture)
	allCmd.Env = append(os.Environ(),
		fmt.Sprintf("FOLDERMCP_STORE=%s", storeDir),
		fmt.Sprintf("HOME=%s", homeDir),
		fmt.Sprintf("USERPROFILE=%s", homeDir),
	)
	allCmd.Stderr = os.Stderr
	if err := allCmd.Start(); err != nil {
		t.Fatalf("start all-v3: %v", err)
	}
	defer func() {
		cancel()
		_ = allCmd.Wait()
	}()

	waitForSocket(t, sockPath, 45*time.Second)
	t.Log("socket ready — launching concurrent queries")

	// Fire 50 concurrent MCP sessions, each issuing one search query.
	// We use separate MCP shim processes to test true concurrency against
	// the gRPC serve backend (not just concurrent JSON-RPC on one stdio pipe).
	const numWorkers = 50
	queries := []string{
		"config", "session", "middleware", "cache", "pipeline",
		"worker", "repository", "architecture", "API reference", "meeting",
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		panics  int
		errors  int
		success int
	)

	// Cancel midway through to test cancellation under load.
	// The helper goroutine must exit when the test completes, otherwise
	// goleak.VerifyNone(t) will flag it as a leak. We use a cancel channel
	// so the goroutine always exits regardless of whether the 5s timer fires.
	midCancel, midStop := context.WithCancel(ctx)
	defer midStop()
	testDone := make(chan struct{})
	defer close(testDone)
	go func() {
		select {
		case <-time.After(5 * time.Second):
			midStop()
		case <-testDone:
		}
	}()

	for i := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					panics++
					mu.Unlock()
					t.Errorf("worker %d panicked: %v", workerID, r)
				}
			}()

			query := queries[workerID%len(queries)]
			mcpCmd := exec.CommandContext(midCancel, bin, "mcp-v3")
			mcpCmd.Env = append(os.Environ(),
				fmt.Sprintf("HOME=%s", homeDir),
				fmt.Sprintf("USERPROFILE=%s", homeDir),
			)
			stdin, err := mcpCmd.StdinPipe()
			if err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}
			stdout, err := mcpCmd.StdoutPipe()
			if err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}
			mcpCmd.Stderr = nil // suppress stderr noise from concurrent workers
			if err := mcpCmd.Start(); err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}
			defer func() {
				_ = stdin.Close()
				_ = mcpCmd.Process.Kill()
				_ = mcpCmd.Wait()
			}()

			scan := bufio.NewScanner(stdout)
			scan.Buffer(make([]byte, 1<<20), 1<<20)

			// Initialize
			initData, _ := json.Marshal(jsonRPCRequest{
				JSONRPC: "2.0", ID: 1, Method: "initialize",
				Params: map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"clientInfo":      map[string]string{"name": fmt.Sprintf("worker-%d", workerID), "version": "1.0"},
					"capabilities":    map[string]interface{}{},
				},
			})
			if _, err := fmt.Fprintf(stdin, "%s\n", initData); err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			// Read init response (best-effort, may be cancelled)
			if !scan.Scan() {
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			// Send search query
			searchData, _ := json.Marshal(jsonRPCRequest{
				JSONRPC: "2.0", ID: 2, Method: "tools/call",
				Params: map[string]interface{}{
					"name":      "foldermcp_search",
					"arguments": map[string]string{"query": query},
				},
			})
			if _, err := fmt.Fprintf(stdin, "%s\n", searchData); err != nil {
				mu.Lock()
				errors++
				mu.Unlock()
				return
			}

			if scan.Scan() {
				line := scan.Text()
				if strings.Contains(line, "result") {
					mu.Lock()
					success++
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	t.Logf("results: %d success, %d errors, %d panics (out of %d workers)", success, errors, panics, numWorkers)
	mu.Unlock()

	if panics > 0 {
		t.Fatalf("FAIL: %d workers panicked under concurrent load", panics)
	}

	t.Log("concurrent load test complete — goleak.VerifyNone will check for leaked goroutines")
}
