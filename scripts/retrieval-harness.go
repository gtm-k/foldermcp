//go:build harness

// Retrieval harness — measures recall@5/recall@10/NDCG@10 across four gRPC
// search modes (auto, lexical, filename, semantic) AND two deterministic
// filesystem baselines (agentic-grep, raw-read) against a labeled query set,
// bucketed per stratum (code/prose/pdf/csv), then checks M1 exit gate G5 (auto
// must beat filename-only).
//
// Usage:
//
//	bin/foldermcp all-v3 <workspace> &
//	sleep 60
//	go run -tags harness ./scripts \
//	    -labeled testdata/v3/labeled_queries.yaml \
//	    -socket /path/to/serve.sock \
//	    -corpus <workspace> \         # enables the filesystem baselines
//	    -out results.json             # writes the JSON schema (see metrics.go)
//
// The harness is now a multi-file package (retrieval-harness.go + metrics.go +
// baselines.go), so it must be run as `./scripts`, not a single .go file.
// For an offline baselines-only run (no server), add -skip-search.
//
// The gRPC modes require a running serve instance; the baselines require the
// corpus on disk plus `rg` (and `pdftotext` for the pdf stratum). The harness
// itself is pure Go — NO cgo.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ── Labeled query set schema ─────────────────────────────

// LabeledQuerySet is the top-level YAML structure for the labeled queries.
type LabeledQuerySet struct {
	Version int     `yaml:"version"`
	Queries []Query `yaml:"queries"`
}

// Query is a single labeled query with relevance judgments.
type Query struct {
	ID        string     `yaml:"id"`
	Text      string     `yaml:"text"`
	Persona   string     `yaml:"persona"`
	Judgments []Judgment `yaml:"judgments"`
}

// Judgment is a single relevance judgment for a query-document pair.
type Judgment struct {
	Path   string `yaml:"path"`
	Symbol string `yaml:"symbol"`
	Grade  int    `yaml:"grade"` // 0=not relevant, 1=somewhat, 2=highly
}

// gRPC search modes, in display order.
var grpcModes = []string{"auto", "lexical", "filename", "semantic"}

// Filesystem baseline names, in display order.
const (
	baselineAgenticGrep = "agentic-grep"
	baselineRawRead     = "raw-read"
)

// outputOrder is the deterministic key order for tables and JSON.
var outputOrder = []string{"auto", "lexical", "filename", "semantic", baselineAgenticGrep, baselineRawRead}

// ── Main ─────────────────────────────────────────────────

func main() {
	var (
		labeled    = flag.String("labeled", "testdata/v3/labeled_queries.yaml", "path to labeled query set YAML")
		socket     = flag.String("socket", "", "serve socket path (default: $FOLDERMCP_SOCKET or ~/.foldermcp/run/serve.sock)")
		timeout    = flag.Duration("timeout", 30*time.Second, "per-query RPC/tool timeout")
		corpus     = flag.String("corpus", "", "corpus root dir; when set, runs the agentic-grep + raw-read filesystem baselines")
		skipSearch = flag.Bool("skip-search", false, "skip the gRPC search modes and G5 (offline baselines-only run)")
		outPath    = flag.String("out", "", "write the JSON results schema to this file")
		jsonStdout = flag.Bool("json", false, "emit JSON results to stdout (human output is routed to stderr)")
		rgBin      = flag.String("rg", "rg", "ripgrep binary for the agentic-grep/raw-read baselines")
		pdfBin     = flag.String("pdftotext", "pdftotext", "pdftotext binary for the pdf stratum")
		callBudget = flag.Int("call-budget", defaultCallBudget, "per-query rg-search + confirm-read budget for agentic-grep (floor 8; PILOT placeholder)")
		pdfBudget  = flag.Int("pdf-budget", defaultCallBudget, "per-query pdftotext-extraction budget, SEPARATE from -call-budget (floor 8; PILOT placeholder)")
		readWindow = flag.Int("read-window", defaultReadWindowLines, "± lines for agentic-grep bounded window reads")
		readTopN   = flag.Int("read-topn", defaultReadTopN, "top candidates confirmed (agentic) / read whole (raw-read)")
	)
	flag.Parse()

	// Human-readable output goes to stderr when JSON is on stdout, so machine
	// output stays parseable.
	var hout io.Writer = os.Stdout
	if *jsonStdout {
		hout = os.Stderr
	}

	// Resolve socket path.
	sock := *socket
	if sock == "" {
		sock = os.Getenv("FOLDERMCP_SOCKET")
	}
	if sock == "" {
		home, _ := os.UserHomeDir()
		sock = home + "/.foldermcp/run/serve.sock"
	}

	// Load labeled queries.
	raw, err := os.ReadFile(*labeled)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot read labeled queries: %v\n", err)
		os.Exit(2)
	}
	var qs LabeledQuerySet
	if err := yaml.Unmarshal(raw, &qs); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot parse labeled queries: %v\n", err)
		os.Exit(2)
	}
	if len(qs.Queries) == 0 {
		fmt.Fprintln(os.Stderr, "error: labeled query set is empty")
		os.Exit(2)
	}
	fmt.Fprintf(hout, "loaded %d labeled queries from %s\n", len(qs.Queries), *labeled)

	results := make(map[string]resultEntry)
	// Per-query evals for auto and raw-read are retained so the token-efficiency
	// ratio is computed over the INTERSECTION of queries both evaluated (Finding 8).
	var autoEvals, rrEvals []queryEval

	// ── gRPC search modes ────────────────────────────────
	// searchRan tracks whether the gRPC modes were attempted (gates G5 below).
	searchRan := !*skipSearch
	var client pb.IndexToolsClient
	if *skipSearch {
		if *corpus == "" {
			fmt.Fprintln(os.Stderr, "error: -skip-search requires -corpus (nothing else to run)")
			os.Exit(2)
		}
		fmt.Fprintln(hout, "skip-search: gRPC modes disabled, running baselines only")
	} else {
		conn, connErr := grpc.NewClient("unix:"+sock,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if connErr != nil {
			if *corpus == "" {
				// Backward-compatible behavior: no baselines requested → hard fail.
				fmt.Fprintf(os.Stderr, "error: cannot connect to serve at %s: %v\n", sock, connErr)
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "warning: cannot connect to serve at %s: %v — running baselines only\n", sock, connErr)
			searchRan = false
		} else {
			defer func() { _ = conn.Close() }()
			client = pb.NewIndexToolsClient(conn)
		}
	}
	if client != nil {
		for _, mode := range grpcModes {
			var evals []queryEval
			for _, q := range qs.Queries {
				if countRelevant(q.Judgments) == 0 {
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), *timeout)
				resp, err := client.SearchBroadly(ctx, &pb.SearchBroadlyRequest{
					Query:      q.Text,
					Mode:       mode,
					MaxResults: 10,
				})
				cancel()
				if err != nil {
					fmt.Fprintf(os.Stderr, "  [%s] query %s (%q): %v\n", mode, q.ID, q.Text, err)
					continue
				}
				hits := resp.GetResults()
				evals = append(evals, evalQuery(q, hitPaths(hits), grpcQueryTokens(q.Text, hits)))
			}
			results[mode] = aggregate(evals)
			if mode == "auto" {
				autoEvals = evals
			}
			if len(evals) == 0 {
				fmt.Fprintf(os.Stderr, "warning: no queries evaluated for mode %s\n", mode)
				continue
			}
			fmt.Fprintf(hout, "  [%s] evaluated %d queries\n", mode, len(evals))
		}
	}

	// ── Filesystem baselines ─────────────────────────────
	if *corpus != "" {
		cfg := baselineConfig{
			corpus:          *corpus,
			rgBin:           *rgBin,
			callBudget:      *callBudget,
			pdfBudget:       *pdfBudget,
			readWindowLines: *readWindow,
			readTopN:        *readTopN,
		}
		// rg is mandatory for both baselines — fail VISIBLY if missing.
		if _, err := exec.LookPath(*rgBin); err != nil {
			fmt.Fprintf(os.Stderr, "error: ripgrep (%q) not found on PATH: %v — baselines require it\n", *rgBin, err)
			os.Exit(3)
		}
		// pdftotext is optional (pdf stratum only) — warn VISIBLY if missing.
		if _, err := exec.LookPath(*pdfBin); err != nil {
			fmt.Fprintf(os.Stderr, "warning: pdftotext (%q) not found on PATH: %v — pdf-stratum candidates will be skipped\n", *pdfBin, err)
		} else {
			cfg.pdftotextBin = *pdfBin
		}

		results[baselineAgenticGrep] = aggregate(runBaseline(baselineAgenticGrep, runAgenticGrep, cfg, qs.Queries, *timeout, hout))
		rrEvals = runBaseline(baselineRawRead, runRawRead, cfg, qs.Queries, *timeout, hout)
		results[baselineRawRead] = aggregate(rrEvals)
	}

	// ── Token-efficiency ratio (hybrid vs raw-read) ──────
	// Computed over the INTERSECTION of queries BOTH auto and raw-read evaluated
	// (Finding 8). auto drops queries whose RPC errors; raw-read drops queries
	// whose discovery errors — independent failure modes. Summing TokensTotal over
	// non-identical query sets would silently flatter the ratio (e.g. a timeout on
	// the largest-payload query would drop it from the numerator only). Keying by
	// query id and summing only common ids keeps the denominators matched.
	te := computeTokenEfficiency(autoEvals, rrEvals)

	// ── Human-readable tables (unless JSON-only on stdout) ─
	if !*jsonStdout {
		printTables(hout, results, te)
	}

	// ── JSON output ──────────────────────────────────────
	out := benchOutput{
		SchemaVersion:   1,
		Note:            "PILOT — grades NOT user-approved",
		Results:         results,
		TokenEfficiency: te,
	}
	if *outPath != "" {
		if err := writeJSON(*outPath, out); err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot write JSON to %s: %v\n", *outPath, err)
			os.Exit(2)
		}
		fmt.Fprintf(hout, "\nwrote JSON results to %s\n", *outPath)
	}
	if *jsonStdout {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot encode JSON: %v\n", err)
			os.Exit(2)
		}
	}

	// ── M1 Exit Gate G5: auto recall > filename-only recall ─
	// Only meaningful when the gRPC modes actually ran.
	if searchRan {
		if results["auto"].Aggregate.RecallAt10 <= results["filename"].Aggregate.RecallAt10 {
			fmt.Fprintln(os.Stderr, "\n❌ FAIL G5: auto mode does not beat filename-only baseline")
			os.Exit(1)
		}
		fmt.Fprintln(hout, "\n✅ PASS G5: auto mode beats filename-only baseline")
	} else {
		fmt.Fprintln(hout, "\n(skipped G5: gRPC search modes did not run)")
	}
}

// ── Orchestration helpers ────────────────────────────────

// baselineFn runs one filesystem baseline for a single query.
type baselineFn func(context.Context, baselineConfig, Query) ([]string, int, error)

// runBaseline evaluates a filesystem baseline over the query set, skipping
// all-grade-0 queries and surfacing per-query errors on stderr.
func runBaseline(name string, fn baselineFn, cfg baselineConfig, queries []Query, timeout time.Duration, hout io.Writer) []queryEval {
	var evals []queryEval
	for _, q := range queries {
		if countRelevant(q.Judgments) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		paths, tokens, err := fn(ctx, cfg, q)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [%s] query %s (%q): %v\n", name, q.ID, q.Text, err)
			continue
		}
		evals = append(evals, evalQuery(q, paths, tokens))
	}
	fmt.Fprintf(hout, "  [%s] evaluated %d queries\n", name, len(evals))
	return evals
}

// computeTokenEfficiency builds the hybrid-vs-raw-read token ratio over the
// INTERSECTION of queries that BOTH auto and raw-read evaluated, keyed by query
// id (Finding 8). Returns nil when there is no common, non-empty overlap. When
// the two evaluated sets differ, it emits a visible stderr warning so a
// mismatched denominator can never silently flatter the ratio.
func computeTokenEfficiency(autoEvals, rrEvals []queryEval) *tokenEfficiency {
	if len(autoEvals) == 0 || len(rrEvals) == 0 {
		return nil
	}
	autoByID := make(map[string]int, len(autoEvals))
	for _, e := range autoEvals {
		autoByID[e.ID] = e.Tokens
	}
	var hybridTok, rawTok, common int
	for _, e := range rrEvals {
		if ht, ok := autoByID[e.ID]; ok {
			hybridTok += ht
			rawTok += e.Tokens
			common++
		}
	}
	if common != len(autoEvals) || common != len(rrEvals) {
		fmt.Fprintf(os.Stderr,
			"warning: token-efficiency over %d queries common to auto(%d) and raw-read(%d); mismatched queries excluded so the ratio stays apples-to-apples\n",
			common, len(autoEvals), len(rrEvals))
	}
	if common == 0 {
		return nil
	}
	ratio := 0.0
	if rawTok > 0 {
		ratio = float64(hybridTok) / float64(rawTok)
	}
	return &tokenEfficiency{
		HybridMode:     "auto",
		HybridTokens:   hybridTok,
		RawReadTokens:  rawTok,
		Ratio:          ratio,
		Threshold:      0.1,
		MeetsThreshold: rawTok > 0 && ratio <= 0.1,
	}
}

// hitPaths extracts the path of each search hit (the only field the metrics use).
func hitPaths(hits []*pb.SearchHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.GetPath()
	}
	return out
}

// grpcQueryTokens approximates the tool-I/O tokens for one SearchBroadly call:
// the query text issued plus the path+title+snippet returned per hit (the human-
// readable payload an agent would ingest).
func grpcQueryTokens(query string, hits []*pb.SearchHit) int {
	l := &tokenLedger{}
	l.addQuery(query)
	for _, h := range hits {
		l.addRead(h.GetPath())
		l.addRead(h.GetTitle())
		l.addRead(h.GetSnippet())
	}
	return l.total()
}

// writeJSON marshals out to path with stable indentation.
func writeJSON(path string, out benchOutput) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// printTables renders the backward-compatible 4-mode table plus the extended
// per-mode/baseline table, per-stratum breakdown, and token-efficiency line.
func printTables(w io.Writer, results map[string]resultEntry, te *tokenEfficiency) {
	r10 := func(k string) float64 { return results[k].Aggregate.RecallAt10 }
	nd10 := func(k string) float64 { return results[k].Aggregate.NDCGAt10 }

	// Backward-compatible original table (unchanged format).
	fmt.Fprintln(w)
	fmt.Fprintln(w, "mode            recall@10   ndcg@10")
	fmt.Fprintln(w, "──────────────  ─────────   ───────")
	fmt.Fprintf(w, "auto            %.3f       %.3f\n", r10("auto"), nd10("auto"))
	fmt.Fprintf(w, "lexical (FTS)   %.3f       %.3f\n", r10("lexical"), nd10("lexical"))
	fmt.Fprintf(w, "filename-only   %.3f       %.3f\n", r10("filename"), nd10("filename"))
	fmt.Fprintf(w, "semantic (vec)  %.3f       %.3f\n", r10("semantic"), nd10("semantic"))
	fmt.Fprintln(w)

	// Multipliers (guard against division by zero).
	fnRecall := math.Max(r10("filename"), 0.001)
	fnNDCG := math.Max(nd10("filename"), 0.001)
	ftsRecall := math.Max(r10("lexical"), 0.001)
	ftsNDCG := math.Max(nd10("lexical"), 0.001)
	fmt.Fprintf(w, "auto vs filename = %.2fx recall, %.2fx ndcg\n", r10("auto")/fnRecall, nd10("auto")/fnNDCG)
	fmt.Fprintf(w, "auto vs FTS      = %.2fx recall, %.2fx ndcg\n", r10("auto")/ftsRecall, nd10("auto")/ftsNDCG)

	// Extended table: recall@5, recall@10, ndcg@10, tokens/query for every entry.
	fmt.Fprintln(w)
	fmt.Fprintln(w, "entry            recall@5  recall@10  ndcg@10  tok/query  n")
	fmt.Fprintln(w, "───────────────  ────────  ─────────  ───────  ─────────  ──")
	for _, k := range outputOrder {
		e, ok := results[k]
		if !ok {
			continue
		}
		a := e.Aggregate
		fmt.Fprintf(w, "%-15s  %7.3f   %7.3f   %6.3f   %8.1f  %2d\n",
			k, a.RecallAt5, a.RecallAt10, a.NDCGAt10, a.TokensPerQuery, a.QueriesEvaluated)
	}

	// Per-stratum breakdown.
	fmt.Fprintln(w)
	fmt.Fprintln(w, "per-stratum recall@10:")
	strata := []string{stratumCode, stratumProse, stratumPDF, stratumCSV, stratumOther}
	fmt.Fprintf(w, "%-15s", "entry")
	for _, s := range strata {
		fmt.Fprintf(w, "  %-7s", s)
	}
	fmt.Fprintln(w)
	for _, k := range outputOrder {
		e, ok := results[k]
		if !ok {
			continue
		}
		fmt.Fprintf(w, "%-15s", k)
		for _, s := range strata {
			if mb, ok := e.PerStratum[s]; ok {
				fmt.Fprintf(w, "  %7.3f", mb.RecallAt10)
			} else {
				fmt.Fprintf(w, "  %7s", "-")
			}
		}
		fmt.Fprintln(w)
	}

	// Token-efficiency ratio.
	if te != nil {
		fmt.Fprintln(w)
		status := "above"
		if te.MeetsThreshold {
			status = "within"
		}
		fmt.Fprintf(w, "token efficiency: hybrid(%s)=%d tok, raw-read=%d tok, ratio=%.3f (%s %.2f threshold)\n",
			te.HybridMode, te.HybridTokens, te.RawReadTokens, te.Ratio, status, te.Threshold)
	}
}

// ── Metric functions ─────────────────────────────────────

// countRelevant returns the number of judgments with grade > 0.
func countRelevant(judgments []Judgment) int {
	n := 0
	for _, j := range judgments {
		if j.Grade > 0 {
			n++
		}
	}
	return n
}

// recallAtK computes recall@k over *pb.SearchHit results. Thin wrapper over the
// path-based core (metrics.go) so live hits and baseline candidates score the
// same way.
func recallAtK(hits []*pb.SearchHit, judgments []Judgment, k int) float64 {
	return recallAtKPaths(hitPaths(hits), judgments, k)
}

// ndcgAtK computes NDCG@k over *pb.SearchHit results (delegates to the core).
func ndcgAtK(hits []*pb.SearchHit, judgments []Judgment, k int) float64 {
	return ndcgAtKPaths(hitPaths(hits), judgments, k)
}

// hitRelevance finds the best-matching grade for a hit path against the grade map.
func hitRelevance(hitPath string, gradeMap map[string]int) int {
	bestGrade := 0
	for judgPath, grade := range gradeMap {
		if strings.Contains(hitPath, judgPath) && grade > bestGrade {
			bestGrade = grade
		}
	}
	return bestGrade
}
