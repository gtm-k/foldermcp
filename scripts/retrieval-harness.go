//go:build harness

// Retrieval harness — measures recall@10 and NDCG@10 across three search modes
// (auto, lexical, filename) against a labeled query set, then checks M1 exit
// gate G5: auto mode must beat filename-only baseline.
//
// Usage:
//
//	bin/foldermcp all-v3 <workspace> &
//	sleep 60
//	go run -tags harness scripts/retrieval-harness.go \
//	    -labeled testdata/v3/labeled_queries.yaml \
//	    -socket /path/to/serve.sock
//
// The harness connects to a running foldermcp serve instance over gRPC and
// issues SearchBroadly RPCs with different mode settings. It does NOT require
// cgo — it's a pure gRPC client.
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
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
	ID        string    `yaml:"id"`
	Text      string    `yaml:"text"`
	Persona   string    `yaml:"persona"`
	Judgments []Judgment `yaml:"judgments"`
}

// Judgment is a single relevance judgment for a query-document pair.
type Judgment struct {
	Path   string `yaml:"path"`
	Symbol string `yaml:"symbol"`
	Grade  int    `yaml:"grade"` // 0=not relevant, 1=somewhat, 2=highly
}

// ── Main ─────────────────────────────────────────────────

func main() {
	var (
		labeled = flag.String("labeled", "testdata/v3/labeled_queries.yaml", "path to labeled query set YAML")
		socket  = flag.String("socket", "", "serve socket path (default: $FOLDERMCP_SOCKET or ~/.foldermcp/run/serve.sock)")
		timeout = flag.Duration("timeout", 30*time.Second, "per-query RPC timeout")
	)
	flag.Parse()

	// Resolve socket path
	sock := *socket
	if sock == "" {
		sock = os.Getenv("FOLDERMCP_SOCKET")
	}
	if sock == "" {
		home, _ := os.UserHomeDir()
		sock = home + "/.foldermcp/run/serve.sock"
	}

	// Load labeled queries
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

	fmt.Printf("loaded %d labeled queries from %s\n", len(qs.Queries), *labeled)

	// Connect to gRPC server
	conn, err := grpc.NewClient("unix:"+sock,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot connect to serve at %s: %v\n", sock, err)
		os.Exit(2)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewIndexToolsClient(conn)

	// Run each mode and compute aggregate metrics
	modes := []string{"auto", "lexical", "filename"}
	recalls := make(map[string]float64)
	ndcgs := make(map[string]float64)

	for _, mode := range modes {
		var totalRecall, totalNDCG float64
		var evaluated int

		for _, q := range qs.Queries {
			// Skip queries with no relevant judgments (all grade=0)
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

			hits := resp.GetResults() // []*pb.SearchHit
			recall := recallAtK(hits, q.Judgments, 10)
			ndcg := ndcgAtK(hits, q.Judgments, 10)
			totalRecall += recall
			totalNDCG += ndcg
			evaluated++
		}

		if evaluated == 0 {
			fmt.Fprintf(os.Stderr, "warning: no queries evaluated for mode %s\n", mode)
			continue
		}

		n := float64(evaluated)
		recalls[mode] = totalRecall / n
		ndcgs[mode] = totalNDCG / n

		fmt.Printf("  [%s] evaluated %d queries\n", mode, evaluated)
	}

	// Print results table
	fmt.Println()
	fmt.Println("mode            recall@10   ndcg@10")
	fmt.Println("──────────────  ─────────   ───────")
	fmt.Printf("auto            %.3f       %.3f\n", recalls["auto"], ndcgs["auto"])
	fmt.Printf("lexical (FTS)   %.3f       %.3f\n", recalls["lexical"], ndcgs["lexical"])
	fmt.Printf("filename-only   %.3f       %.3f\n", recalls["filename"], ndcgs["filename"])
	fmt.Println()

	// Compute multipliers (guard against division by zero)
	fnRecall := math.Max(recalls["filename"], 0.001)
	fnNDCG := math.Max(ndcgs["filename"], 0.001)
	ftsRecall := math.Max(recalls["lexical"], 0.001)
	ftsNDCG := math.Max(ndcgs["lexical"], 0.001)

	fmt.Printf("auto vs filename = %.2fx recall, %.2fx ndcg\n",
		recalls["auto"]/fnRecall, ndcgs["auto"]/fnNDCG)
	fmt.Printf("auto vs FTS      = %.2fx recall, %.2fx ndcg\n",
		recalls["auto"]/ftsRecall, ndcgs["auto"]/ftsNDCG)

	// M1 Exit Gate G5: auto recall > filename-only recall
	if recalls["auto"] <= recalls["filename"] {
		fmt.Fprintln(os.Stderr, "\n❌ FAIL G5: auto mode does not beat filename-only baseline")
		os.Exit(1)
	}
	fmt.Println("\n✅ PASS G5: auto mode beats filename-only baseline")
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

// recallAtK computes the fraction of relevant documents found in the top-k hits.
// A hit matches a judgment if the hit's path contains the judgment path as a substring.
func recallAtK(hits []*pb.SearchHit, judgments []Judgment, k int) float64 {
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
	for i, h := range hits {
		if i >= k {
			break
		}
		for path := range relevant {
			if strings.Contains(h.GetPath(), path) {
				found++
				delete(relevant, path) // count each relevant doc at most once
				break
			}
		}
	}
	return float64(found) / float64(found+len(relevant)) // found / total_relevant
}

// ndcgAtK computes Normalized Discounted Cumulative Gain at k.
//
// DCG@k  = Σᵢ₌₁ᵏ (2^relᵢ - 1) / log₂(i + 1)
// IDCG@k = DCG@k over the ideal ranking (judgments sorted by grade descending)
// NDCG@k = DCG@k / IDCG@k
//
// Relevance for a hit is determined by substring-matching the hit path against
// judgment paths. If no judgment matches, relevance is 0.
func ndcgAtK(hits []*pb.SearchHit, judgments []Judgment, k int) float64 {
	// Build path → grade map for lookup
	gradeMap := make(map[string]int)
	for _, j := range judgments {
		gradeMap[j.Path] = j.Grade
	}

	// Compute DCG@k from actual hits
	dcg := 0.0
	for i, h := range hits {
		if i >= k {
			break
		}
		rel := hitRelevance(h.GetPath(), gradeMap)
		dcg += (math.Pow(2, float64(rel)) - 1) / math.Log2(float64(i+2))
	}

	// Compute IDCG@k from ideal ranking (sorted grades descending)
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
