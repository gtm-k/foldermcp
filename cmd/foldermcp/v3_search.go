//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gtm-k/foldermcp/internal/v3/embed"
	v3grpc "github.com/gtm-k/foldermcp/internal/v3/grpc"
	pb "github.com/gtm-k/foldermcp/internal/v3/proto/gen"
	"github.com/gtm-k/foldermcp/internal/v3/store"
)

var (
	searchMode       string
	searchDetail     string
	searchK          int
	searchKind       string
	searchPathPrefix string
)

var v3SearchCmd = &cobra.Command{
	Use:   "search-v3 <query>",
	Short: "Search the local index (hybrid lexical + semantic) and print ranked file matches",
	Long: `Search runs the same hybrid retrieval the MCP foldermcp_search tool uses,
in-process against the local index (no running daemon required). It prints
ranked file paths so a human or CI can find the relevant files instead of
reading everything.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		resp, err := runV3Search(ctx, cmd.OutOrStdout(), strings.Join(args, " "))
		// Exit-code contract for CI: 0 = at least one hit, 1 = zero hits, 2 = error.
		// runV3Search has already closed the DB + embedder explicitly (no leaked
		// native ONNX session), so an os.Exit here skips no cleanup. We bypass
		// cobra's own exit-1-on-error by returning nil after printing the error
		// and exiting with our own code.
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
		}
		os.Exit(searchExitCode(resp, err))
		return nil // unreachable
	},
}

// runV3Search opens the store, runs SearchBroadly, applies client-side filters,
// writes the formatted output, and returns the (filtered) response so the caller
// can derive a process exit code. It closes the DB and embedder EXPLICITLY before
// returning so the caller may os.Exit without leaking the native ONNX session
// (a deferred Close would be skipped by os.Exit).
func runV3Search(ctx context.Context, out io.Writer, query string) (*pb.SearchBroadlyResponse, error) {
	if !validSearchMode(searchMode) {
		return nil, fmt.Errorf("invalid --mode %q: want auto|lexical|semantic|filename", searchMode)
	}
	if !validSearchKind(searchKind) {
		return nil, fmt.Errorf("invalid --kind %q: want code|prose|pdf|csv", searchKind)
	}

	storeDir := os.Getenv("FOLDERMCP_STORE")
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".foldermcp", "store", "default")
	}
	dbPath := filepath.Join(storeDir, "index.db")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no index at %s — run `foldermcp index-v3 <folder>` first", dbPath)
		}
		return nil, fmt.Errorf("cannot access index at %s: %w", dbPath, err)
	}

	db, err := store.Open(store.Options{Path: dbPath, Tier: store.DetectTier(detectRAMMB()), ReadOnly: true})
	if err != nil {
		return nil, err
	}
	// Explicit cleanup (not defer): the caller may os.Exit on our return, which
	// would skip deferred closers and leak the native ONNX session.
	closeDB := func() { _ = db.Close() }

	// Optional semantic search: nil embedder degrades to lexical (FTS+filename).
	configureOrtLib()
	var emb *embed.Embedder
	if modelPath, tokenizerPath, modelErr := resolveModelPaths(); modelErr != nil {
		fmt.Fprintf(os.Stderr, "foldermcp search: WARNING semantic search disabled: %v\n", modelErr)
	} else {
		emb = embed.NewEmbedder(modelPath, tokenizerPath)
	}
	cleanup := func() {
		if emb != nil {
			_ = emb.Close() // free native ONNX session/tensors
		}
		closeDB()
	}

	// Read-side fingerprint gate: don't run fixed-scale query codes against an
	// index quantized with a different scheme (e.g. an old per-vector "int8"
	// store) — that silently returns garbage rankings. Degrade to lexical and
	// say why. `emb` (and its Close in cleanup) stays the resource owner; only
	// the server's view is cleared, so cleanup still runs correctly.
	searchEmbedder := emb
	if searchEmbedder != nil {
		if ok, stored := embed.SemanticIndexCompatible(db); !ok {
			fmt.Fprintf(os.Stderr, "foldermcp search: WARNING semantic search disabled — "+
				"index quantization %q is incompatible with this binary (%q); "+
				"delete the store's index.db and re-run index-v3 to rebuild\n",
				stored, embed.QuantizationModeString())
			searchEmbedder = nil
		}
	}

	srv := v3grpc.NewServer(v3grpc.ServerOpts{DB: db, Embedder: searchEmbedder})
	resp, err := srv.SearchBroadly(ctx, &pb.SearchBroadlyRequest{
		Query:      query,
		Mode:       searchMode,
		Detail:     searchDetail,
		MaxResults: int32(searchK),
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("search: %w", err)
	}

	// Client-side post-filters (no proto change): drop hits outside --path-prefix
	// and/or --kind, then renumber ranks 1..N in the formatters.
	var prefiltered int
	if resp != nil {
		prefiltered = len(resp.Results)
	}
	resp = filterSearchResults(resp, searchKind, searchPathPrefix)
	// Observable signal: if a filter is active and it removed ALL real hits, the
	// user would otherwise see a generic "no matches" and could not tell their
	// filter ate the hits — especially confusing since --kind is a documented
	// file-extension heuristic. Emit a NOTE to stderr (NOT stdout/JSON, which stay
	// clean). The exit code stays 1 (zero results is still zero results).
	if (searchKind != "" || searchPathPrefix != "") && prefiltered > 0 && (resp == nil || len(resp.Results) == 0) {
		fmt.Fprintf(os.Stderr, "foldermcp search: NOTE all %d hit(s) were removed by "+
			"--kind/--path-prefix (--kind is a file-extension heuristic); "+
			"relax the filter to see them\n", prefiltered)
	}

	var rendered string
	if jsonOutput {
		rendered = formatSearchResultsJSON(resp, query)
	} else {
		rendered = formatSearchResults(resp, query)
	}
	_, werr := io.WriteString(out, rendered)
	cleanup()
	if werr != nil {
		return resp, werr
	}
	return resp, nil
}

// searchExitCode maps a (response, error) pair to a CI-usable process exit code:
// 2 on any error, 1 when there were zero hits, 0 when at least one hit. Pure.
//
// The SearchBroadly handler signals several failures IN-BAND: it sets
// OverallStatus="error" and returns a nil Go error (e.g. --mode semantic with no
// embedder / incompatible index, or a required lexical/filename/vector source
// that did not execute — see internal/v3/grpc/tools/search_broadly.go). An
// in-band error MUST dominate the zero-hits check, otherwise a broken search
// backend is indistinguishable from a legitimate empty result set (a false CI
// contract). "degraded" is partial success WITH hits and is intentionally NOT
// treated as an error — a degraded response with hits stays exit 0.
func searchExitCode(resp *pb.SearchBroadlyResponse, err error) int {
	if err != nil {
		return 2
	}
	if resp != nil && resp.OverallStatus == "error" {
		return 2 // in-band failure: backend down / required source failed
	}
	if resp == nil || len(resp.Results) == 0 {
		return 1
	}
	return 0
}

// validSearchKind reports whether k is a recognized --kind filter ("" = no filter).
func validSearchKind(k string) bool {
	switch k {
	case "", "code", "prose", "pdf", "csv":
		return true
	}
	return false
}

// filterSearchResults returns a NEW response whose Results are limited to hits
// matching the optional --kind and --path-prefix filters (empty = no filter).
// The input is not mutated. Ranks are positional, so dropping hits and keeping
// the surviving order is sufficient — the formatters renumber from 1.
//
// LIMITATION: the SearchBroadly wire schema (SearchHit) carries no chunk_kind,
// so --kind cannot select by the indexer's true chunk kind (code_ast,
// code_fallback, prose, pdf_text, csv_schema, csv_rows). It is a best-effort
// filter on the result PATH's file extension. Surfacing real chunk_kind would
// need a proto change (out of scope for this command).
func filterSearchResults(resp *pb.SearchBroadlyResponse, kind, pathPrefix string) *pb.SearchBroadlyResponse {
	if resp == nil || (kind == "" && pathPrefix == "") {
		return resp
	}
	out := &pb.SearchBroadlyResponse{
		OverallStatus: resp.OverallStatus,
		Completeness:  resp.Completeness,
		Sources:       resp.Sources,
		FailedSources: resp.FailedSources,
	}
	for _, h := range resp.Results {
		if h == nil {
			continue
		}
		if pathPrefix != "" && !strings.HasPrefix(h.Path, pathPrefix) {
			continue
		}
		if kind != "" && !pathMatchesKind(h.Path, kind) {
			continue
		}
		out.Results = append(out.Results, h)
	}
	return out
}

// pathMatchesKind is the best-effort extension heuristic backing --kind (see the
// LIMITATION note on filterSearchResults).
func pathMatchesKind(path, kind string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch kind {
	case "code":
		switch ext {
		case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".h", ".cc",
			".cpp", ".hpp", ".rs", ".rb", ".php", ".sh", ".cs", ".kt", ".swift", ".scala", ".m":
			return true
		}
	case "prose":
		switch ext {
		case ".md", ".markdown", ".txt", ".rst", ".adoc":
			return true
		}
	case "pdf":
		return ext == ".pdf"
	case "csv":
		return ext == ".csv"
	}
	return false
}

// formatSearchResultsJSON renders a SearchBroadly response as STABLE, documented
// JSON. Kept pure (no I/O) so it is unit-testable.
//
// Schema:
//
//	{
//	  "query":        string,
//	  "status":       string,   // overall_status
//	  "completeness": string,
//	  "degraded_sources": [string],   // omitted when empty
//	  "sources": [ {"name": string, "status": string, "latency_ms": int} ],
//	  "results": [ {"rank": int, "path": string, "title": string,
//	                "snippet": string, "matched_sources": [string]} ]
//	}
//
// Deliberately NO "score" field: SearchHit.Score is the Reciprocal Rank Fusion
// value (a positional artifact), not a relevance magnitude. Surfacing it invites
// callers to compare hits by a meaningless number. Rank order + matched_sources
// are the honest signals — same principle as the human formatter. All
// path/title/snippet values pass through the terminal sanitizers so crafted
// indexed content cannot inject control characters into the JSON stream.
func formatSearchResultsJSON(resp *pb.SearchBroadlyResponse, query string) string {
	type jsonSource struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		LatencyMs int32  `json:"latency_ms"`
	}
	type jsonResult struct {
		Rank           int      `json:"rank"`
		Path           string   `json:"path"`
		Title          string   `json:"title"`
		Snippet        string   `json:"snippet"`
		MatchedSources []string `json:"matched_sources"`
	}
	type jsonDoc struct {
		Query           string       `json:"query"`
		Status          string       `json:"status"`
		Completeness    string       `json:"completeness"`
		DegradedSources []string     `json:"degraded_sources,omitempty"`
		Sources         []jsonSource `json:"sources"`
		Results         []jsonResult `json:"results"`
	}

	doc := jsonDoc{
		Query:           query,
		Status:          resp.OverallStatus,
		Completeness:    resp.Completeness,
		DegradedSources: resp.FailedSources,
		Sources:         []jsonSource{},
		Results:         []jsonResult{},
	}
	for _, s := range resp.Sources {
		if s == nil {
			continue
		}
		doc.Sources = append(doc.Sources, jsonSource{
			Name:      s.SourceName,
			Status:    s.Status,
			LatencyMs: s.LatencyMs,
		})
	}
	for i, h := range resp.Results {
		if h == nil {
			continue
		}
		doc.Results = append(doc.Results, jsonResult{
			Rank:           i + 1, // renumber 1..N (filters may have dropped hits)
			Path:           sanitizeLine(h.Path),
			Title:          sanitizeLine(h.Title),
			Snippet:        sanitizeTerminal(h.Snippet),
			MatchedSources: h.MatchedSources,
		})
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		// Fields are plain strings/ints; marshal cannot realistically fail. Keep
		// the contract (valid JSON) even in the impossible case. Do NOT use %q here:
		// Go's %q is not JSON-string-safe (a control byte renders as \x1b, invalid
		// JSON), so marshal a minimal struct to guarantee valid escaping.
		fb, ferr := json.Marshal(struct {
			Query  string `json:"query"`
			Status string `json:"status"`
			Error  string `json:"error"`
		}{Query: query, Status: "error", Error: err.Error()})
		if ferr != nil {
			// Doubly-impossible: fall back to a constant valid-JSON literal.
			return "{\"status\":\"error\",\"results\":[]}\n"
		}
		return string(fb) + "\n"
	}
	return string(b) + "\n"
}

// formatSearchResults renders a SearchBroadly response as human-readable text.
// Kept pure (no I/O) so it is unit-testable.
func formatSearchResults(resp *pb.SearchBroadlyResponse, query string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "query %q — %d result(s) [status=%s, completeness=%s]\n",
		query, len(resp.Results), resp.OverallStatus, resp.Completeness)

	if len(resp.Sources) > 0 {
		parts := make([]string, 0, len(resp.Sources))
		for _, s := range resp.Sources {
			if s == nil {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%s(%dms)", s.SourceName, s.Status, s.LatencyMs))
		}
		fmt.Fprintf(&b, "sources: %s\n", strings.Join(parts, " "))
	}
	if len(resp.FailedSources) > 0 {
		fmt.Fprintf(&b, "degraded sources: %s\n", strings.Join(resp.FailedSources, ", "))
	}

	if len(resp.Results) == 0 {
		b.WriteString("\nno matches — try broader terms or --mode lexical\n")
		return b.String()
	}

	for i, h := range resp.Results {
		matched := ""
		if len(h.MatchedSources) > 0 {
			matched = " (" + strings.Join(h.MatchedSources, "+") + ")"
		}
		// Results are printed in fused-rank order (the ordinal IS the rank).
		// We deliberately do NOT print h.Score: it is the Reciprocal Rank Fusion
		// score (1/(k+rank+1)), a positional artifact, not a relevance magnitude —
		// showing it invites readers to compare hits by a meaningless number.
		// The honest signals are rank order and source agreement (how many
		// independent sources matched, shown in parentheses). Surfacing a true
		// per-source relevance score needs a wire-schema change (tracked separately).
		fmt.Fprintf(&b, "\n%2d.%s %s\n", i+1, matched, sanitizeLine(h.Path))
		if h.Title != "" && h.Title != h.Path {
			fmt.Fprintf(&b, "    %s\n", sanitizeLine(h.Title))
		}
		if snip := strings.TrimSpace(sanitizeTerminal(h.Snippet)); snip != "" {
			for _, line := range strings.Split(snip, "\n") {
				fmt.Fprintf(&b, "    | %s\n", line)
			}
		}
	}
	return b.String()
}

// validSearchMode reports whether m is a recognized SearchBroadly mode.
func validSearchMode(m string) bool {
	switch m {
	case "auto", "lexical", "semantic", "filename":
		return true
	}
	return false
}

// unsafeTerminalRune reports whether r is a control or format character that must
// never reach a terminal or CI log when it originates from indexed file content:
//   - C0 controls 0x00-0x1F (including ESC 0x1b, the ANSI/OSC introducer),
//   - DEL 0x7f,
//   - C1 controls 0x80-0x9F (single-byte CSI 0x9b / OSC 0x9d / DCS 0x90),
//   - Unicode line/paragraph separators U+2028/U+2029, and
//   - BiDi override/isolate controls U+202A-U+202E and U+2066-U+2069
//     (the "Trojan Source" class: visual reordering / line spoofing).
//
// Newline and tab are intentionally excluded so multi-line callers (snippets)
// can keep them; single-line callers use sanitizeLine, which drops them too.
func unsafeTerminalRune(r rune) bool {
	return r < 0x20 || r == 0x7f ||
		(r >= 0x80 && r <= 0x9f) ||
		r == 0x2028 || r == 0x2029 ||
		(r >= 0x202a && r <= 0x202e) ||
		(r >= 0x2066 && r <= 0x2069)
}

// sanitizeTerminal strips unsafe control/format characters from multi-line text
// (e.g. snippets), preserving newline and tab for layout, so crafted indexed
// content cannot inject terminal/CI-log escape sequences via search output.
func sanitizeTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unsafeTerminalRune(r) {
			return -1
		}
		return r
	}, s)
}

// sanitizeLine is sanitizeTerminal for single-line fields (paths, titles): it
// additionally drops newline and tab so a crafted value cannot inject extra
// output lines (terminal/CI-log line spoofing).
func sanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || unsafeTerminalRune(r) {
			return -1
		}
		return r
	}, s)
}

func init() {
	v3SearchCmd.Flags().StringVar(&searchMode, "mode", "auto", "search mode: auto|lexical|semantic|filename")
	v3SearchCmd.Flags().StringVar(&searchDetail, "detail", "standard", "detail level: brief|standard|full")
	v3SearchCmd.Flags().IntVarP(&searchK, "limit", "k", 10, "maximum number of results")
	v3SearchCmd.Flags().StringVar(&searchKind, "kind", "", "filter results by kind: code|prose|pdf|csv (best-effort by file extension)")
	v3SearchCmd.Flags().StringVar(&searchPathPrefix, "path-prefix", "", "only return results whose path begins with this prefix")
	// NOTE: --json is the root command's persistent flag (root.go: jsonOutput);
	// search-v3 reuses it, so we do not register a second --json here.
	rootCmd.AddCommand(v3SearchCmd)
}
