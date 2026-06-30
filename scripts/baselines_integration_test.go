//go:build harness

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memGrep emulates `rg --json -F -i` over a corpus directory using a pure-Go
// case-insensitive line scan, emitting ripgrep-compatible --json so the WHOLE
// agentic-grep / raw-read orchestration (gatherMatches → parseRgJSON →
// rankCandidates → bounded/whole reads → token ledger) is exercised offline,
// without a real ripgrep binary or a live gRPC server.
func memGrep(_ context.Context, _ string, pattern, root string) ([]byte, error) {
	needle := strings.ToLower(pattern)
	var b strings.Builder
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// emulate rg's default skip of non-text/binary: only scan known text exts
		switch strings.ToLower(filepath.Ext(p)) {
		case ".go", ".py", ".md", ".txt", ".csv", ".json", ".yaml", ".yml":
		default:
			return nil
		}
		f, ferr := os.Open(p)
		if ferr != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		ln := 0
		for sc.Scan() {
			ln++
			line := sc.Text()
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			rec := map[string]any{
				"type": "match",
				"data": map[string]any{
					"path":        map[string]any{"text": p},
					"lines":       map[string]any{"text": line + "\n"},
					"line_number": ln,
				},
			}
			j, _ := json.Marshal(rec)
			b.Write(j)
			b.WriteByte('\n')
		}
		return nil
	})
	return []byte(b.String()), nil
}

// withMemGrep swaps in the in-memory grep for the duration of fn.
func withMemGrep(t *testing.T, fn func()) {
	t.Helper()
	orig := rgRun
	rgRun = memGrep
	defer func() { rgRun = orig }()
	fn()
}

func microCfg() baselineConfig {
	return baselineConfig{
		corpus:          filepath.FromSlash("../testdata/v3/micro"),
		rgBin:           "rg", // ignored — rgRun is stubbed
		callBudget:      defaultCallBudget,
		readWindowLines: defaultReadWindowLines,
		readTopN:        defaultReadTopN,
	}
}

func TestAgenticGrepEndToEndMicro(t *testing.T) {
	cfg := microCfg()
	if _, err := os.Stat(cfg.corpus); err != nil {
		t.Skipf("micro corpus not present: %v", err)
	}

	// Query whose relevant target is session_manager.py (RetryConfig).
	q := Query{
		ID:   "m001",
		Text: "exponential backoff retry logic for HTTP",
		Judgments: []Judgment{
			{Path: "code-python/session_manager.py", Grade: 2},
		},
	}

	var paths []string
	var tokens int
	withMemGrep(t, func() {
		var err error
		paths, tokens, err = runAgenticGrep(context.Background(), cfg, q)
		if err != nil {
			t.Fatalf("runAgenticGrep: %v", err)
		}
	})

	if len(paths) == 0 {
		t.Fatal("agentic-grep returned no candidates")
	}
	if tokens <= 0 {
		t.Errorf("agentic-grep tokens = %d, want > 0", tokens)
	}
	// The target file must surface as a candidate (recall@10 > 0).
	if r := recallAtKPaths(paths, q.Judgments, 10); r == 0 {
		t.Errorf("agentic-grep recall@10 = 0; candidates=%v", paths)
	}
}

func TestRawReadCostsMoreThanAgentic(t *testing.T) {
	cfg := microCfg()
	if _, err := os.Stat(cfg.corpus); err != nil {
		t.Skipf("micro corpus not present: %v", err)
	}

	q := Query{
		ID:   "m003",
		Text: "thread-safe LRU eviction policy with TTL expiration",
		Judgments: []Judgment{
			{Path: "code-python/cache.py", Grade: 2},
		},
	}

	var agenticTok, rawTok int
	withMemGrep(t, func() {
		_, at, err := runAgenticGrep(context.Background(), cfg, q)
		if err != nil {
			t.Fatalf("runAgenticGrep: %v", err)
		}
		_, rt, err := runRawRead(context.Background(), cfg, q)
		if err != nil {
			t.Fatalf("runRawRead: %v", err)
		}
		agenticTok, rawTok = at, rt
	})

	// Raw-read ingests whole files; agentic-grep only bounded windows. Raw-read
	// must cost strictly more tokens — that gap is the token-efficiency signal.
	if rawTok <= agenticTok {
		t.Errorf("raw-read tokens (%d) should exceed agentic-grep tokens (%d)", rawTok, agenticTok)
	}
}

// TestBenchOutputJSONRoundTrip validates the documented JSON schema is stable
// and round-trips (keys recall@5 / recall@10 / ndcg@10 / per_stratum present).
func TestBenchOutputJSONRoundTrip(t *testing.T) {
	evals := []queryEval{
		{Stratum: stratumCode, Recall5: 1, Recall10: 1, NDCG10: 1, Tokens: 50},
		{Stratum: stratumPDF, Recall5: 0, Recall10: 0.5, NDCG10: 0.3, Tokens: 80},
	}
	out := benchOutput{
		SchemaVersion: 1,
		Note:          "PILOT — grades NOT user-approved",
		Results:       map[string]resultEntry{"agentic-grep": aggregate(evals)},
		TokenEfficiency: &tokenEfficiency{
			HybridMode: "auto", HybridTokens: 10, RawReadTokens: 200, Ratio: 0.05, Threshold: 0.1, MeetsThreshold: true,
		},
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, key := range []string{`"recall@5"`, `"recall@10"`, `"ndcg@10"`, `"tokens_total"`, `"per_stratum"`, `"token_efficiency"`, `"schema_version"`} {
		if !strings.Contains(s, key) {
			t.Errorf("JSON missing key %s\n%s", key, s)
		}
	}
	var back benchOutput
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.SchemaVersion != 1 || back.Results["agentic-grep"].Aggregate.QueriesEvaluated != 2 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
}
