//go:build harness

// metrics.go — pure, cgo-free scoring + aggregation primitives shared by the
// gRPC search modes and the filesystem baselines (agentic-grep, raw-read).
//
// Everything here operates on plain []string paths (not *pb.SearchHit) so it can
// score baseline candidates and live search hits with the SAME functions, and so
// it can be unit-tested with in-memory fixtures and no live gRPC server.
package main

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// ── Benchmark strata ─────────────────────────────────────
//
// A query's stratum is derived from the file EXTENSION of its highest-grade
// relevant judgment (content_class alone is insufficient because 'document'
// splits across prose and pdf — see internal/v3/walker/mime.go).
//
// These extension sets MIRROR the canonical sources and are intentionally
// hardcoded (not imported) to keep the harness cgo-free: importing
// internal/v3/walker would pull in its //go:build cgo files whenever CGO is
// enabled. Sources of truth:
//   - pdf  : binaryDocumentExts in internal/v3/walker/binary.go
//   - csv  : structuredDataExts in internal/v3/walker/binary.go
//   - code/prose : classFromExtOrMime in internal/v3/walker/mime.go
const (
	stratumCode  = "code"
	stratumProse = "prose"
	stratumPDF   = "pdf"
	stratumCSV   = "csv"
	stratumOther = "other"
)

var (
	codeExts  = map[string]bool{".py": true, ".go": true, ".js": true, ".ts": true, ".rs": true, ".java": true, ".c": true, ".cc": true, ".cpp": true, ".h": true, ".hpp": true}
	proseExts = map[string]bool{".md": true, ".txt": true, ".rst": true, ".org": true, ".tex": true}
	pdfExts   = map[string]bool{".pdf": true, ".docx": true, ".odt": true}
	dataExts  = map[string]bool{".csv": true, ".tsv": true, ".json": true, ".yaml": true, ".yml": true, ".xml": true}
)

// stratumOf maps a path to its benchmark stratum by file extension.
func stratumOf(path string) string {
	if path == "" {
		return stratumOther
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case pdfExts[ext]:
		return stratumPDF
	case dataExts[ext]:
		return stratumCSV
	case codeExts[ext]:
		return stratumCode
	case proseExts[ext]:
		return stratumProse
	default:
		return stratumOther
	}
}

// queryStratum returns the stratum of the query's highest-grade relevant
// judgment (grade > 0). Ties resolve to the first such judgment in file order.
// Returns stratumOther when the query has no relevant judgment.
func queryStratum(q Query) string {
	best := 0
	stratum := stratumOther
	for _, j := range q.Judgments {
		if j.Grade > best {
			best = j.Grade
			stratum = stratumOf(j.Path)
		}
	}
	return stratum
}

// ── Path-based metric cores ──────────────────────────────
//
// These preserve the EXACT semantics of the original *pb.SearchHit-based
// recallAtK / ndcgAtK (substring path match, distinct-relevant denominator,
// 2^grade-1 gain) — those functions now delegate here.

// recallAtKPaths = (distinct relevant docs found in top-k) / (total distinct
// relevant docs). A path matches a judgment when it CONTAINS the judgment path
// as a substring; each hit credits at most one relevant doc.
func recallAtKPaths(paths []string, judgments []Judgment, k int) float64 {
	relevant := make(map[string]bool)
	for _, j := range judgments {
		if j.Grade > 0 {
			relevant[j.Path] = true
		}
	}
	if len(relevant) == 0 {
		return 0
	}
	found := 0
	for i, p := range paths {
		if i >= k {
			break
		}
		for rp := range relevant {
			if strings.Contains(p, rp) {
				found++
				delete(relevant, rp)
				break
			}
		}
	}
	return float64(found) / float64(found+len(relevant))
}

// ndcgAtKPaths = DCG@k / IDCG@k with gain 2^grade-1 and log2(rank+1) discount.
func ndcgAtKPaths(paths []string, judgments []Judgment, k int) float64 {
	gradeMap := make(map[string]int)
	for _, j := range judgments {
		gradeMap[j.Path] = j.Grade
	}

	dcg := 0.0
	for i, p := range paths {
		if i >= k {
			break
		}
		rel := hitRelevance(p, gradeMap)
		dcg += (math.Pow(2, float64(rel)) - 1) / math.Log2(float64(i+2))
	}

	grades := make([]int, 0, len(judgments))
	for _, j := range judgments {
		grades = append(grades, j.Grade)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(grades)))

	idcg := 0.0
	for i, g := range grades {
		if i >= k {
			break
		}
		idcg += (math.Pow(2, float64(g)) - 1) / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// ── Token accounting ─────────────────────────────────────

// approxTokenCount is a PILOT approximate token count using the widely-cited
// ~4-characters-per-token heuristic (rounded up). It is NOT a real BPE count:
//   - the cgo-gated chunker.TiktokenCounter is unreachable from this cgo-free
//     harness, and
//   - tiktoken-go fetches its cl100k_base vocab over the network on first use,
//     which would break the offline pilot.
//
// Because every baseline uses this same counter, the token-EFFICIENCY ratio
// (hybrid vs raw-read) stays meaningful even though absolute counts are
// approximate. Swap in a real BPE counter later (pre-staged TIKTOKEN_CACHE_DIR).
func approxTokenCount(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// tokenLedger accumulates approximate tokens over a baseline's tool I/O:
// query bytes issued (search patterns / commands) and bytes read back
// (rg output, window/whole-file reads, pdftotext output).
type tokenLedger struct {
	query int
	read  int
}

func (l *tokenLedger) addQuery(s string) { l.query += approxTokenCount(s) }
func (l *tokenLedger) addRead(s string)  { l.read += approxTokenCount(s) }
func (l *tokenLedger) total() int        { return l.query + l.read }

// ── Per-query evaluation + aggregation ───────────────────

// queryEval is one query's outcome for a single mode/baseline. ID identifies the
// query so the token-efficiency ratio can be computed over the INTERSECTION of
// queries that both paths (auto, raw-read) actually evaluated.
type queryEval struct {
	ID       string
	Stratum  string
	Recall5  float64
	Recall10 float64
	NDCG10   float64
	Tokens   int
}

// evalQuery scores a ranked candidate path list against a query's judgments and
// records the tool-I/O token cost, tagged with the query's id and stratum.
func evalQuery(q Query, paths []string, tokens int) queryEval {
	return queryEval{
		ID:       q.ID,
		Stratum:  queryStratum(q),
		Recall5:  recallAtKPaths(paths, q.Judgments, 5),
		Recall10: recallAtKPaths(paths, q.Judgments, 10),
		NDCG10:   ndcgAtKPaths(paths, q.Judgments, 10),
		Tokens:   tokens,
	}
}

// metricBlock is the JSON-serialized metric summary for an aggregate or a
// single stratum.
type metricBlock struct {
	RecallAt5        float64 `json:"recall@5"`
	RecallAt10       float64 `json:"recall@10"`
	NDCGAt10         float64 `json:"ndcg@10"`
	TokensTotal      int     `json:"tokens_total"`
	TokensPerQuery   float64 `json:"tokens_per_query"`
	QueriesEvaluated int     `json:"queries_evaluated"`
}

// resultEntry is the per-mode/per-baseline JSON value.
type resultEntry struct {
	Aggregate  metricBlock            `json:"aggregate"`
	PerStratum map[string]metricBlock `json:"per_stratum"`
}

// meanBlock averages recall/ndcg and sums tokens over a set of query evals.
func meanBlock(evals []queryEval) metricBlock {
	n := len(evals)
	if n == 0 {
		return metricBlock{}
	}
	var r5, r10, nd float64
	var toks int
	for _, e := range evals {
		r5 += e.Recall5
		r10 += e.Recall10
		nd += e.NDCG10
		toks += e.Tokens
	}
	fn := float64(n)
	return metricBlock{
		RecallAt5:        r5 / fn,
		RecallAt10:       r10 / fn,
		NDCGAt10:         nd / fn,
		TokensTotal:      toks,
		TokensPerQuery:   float64(toks) / fn,
		QueriesEvaluated: n,
	}
}

// aggregate produces the overall block plus a per-stratum breakdown.
func aggregate(evals []queryEval) resultEntry {
	groups := make(map[string][]queryEval)
	for _, e := range evals {
		groups[e.Stratum] = append(groups[e.Stratum], e)
	}
	per := make(map[string]metricBlock, len(groups))
	for s, g := range groups {
		per[s] = meanBlock(g)
	}
	return resultEntry{Aggregate: meanBlock(evals), PerStratum: per}
}

// ── JSON envelope ────────────────────────────────────────

// tokenEfficiency reports the hybrid-vs-raw-read token ratio (gate criterion
// hybrid <= 0.1x raw-read). Informational in the pilot — NOT a hard exit gate.
type tokenEfficiency struct {
	HybridMode     string  `json:"hybrid_mode"`
	HybridTokens   int     `json:"hybrid_tokens"`
	RawReadTokens  int     `json:"raw_read_tokens"`
	Ratio          float64 `json:"ratio"`
	Threshold      float64 `json:"threshold"`
	MeetsThreshold bool    `json:"meets_threshold"`
}

// benchOutput is the stable, documented JSON schema written by -out / -json.
//
// Schema (schema_version 1):
//
//	{
//	  "schema_version": 1,
//	  "note": "PILOT — grades NOT user-approved",
//	  "results": {
//	    "<mode_or_baseline>": {
//	      "aggregate":   { recall@5, recall@10, ndcg@10, tokens_total, tokens_per_query, queries_evaluated },
//	      "per_stratum": { "<stratum>": { ...same fields... }, ... }
//	    }, ...
//	  },
//	  "token_efficiency": { hybrid_mode, hybrid_tokens, raw_read_tokens, ratio, threshold, meets_threshold }
//	}
type benchOutput struct {
	SchemaVersion   int                    `json:"schema_version"`
	Note            string                 `json:"note"`
	Results         map[string]resultEntry `json:"results"`
	TokenEfficiency *tokenEfficiency       `json:"token_efficiency,omitempty"`
}
