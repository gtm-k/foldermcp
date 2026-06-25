//go:build cgo

package main

import (
	"context"
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
	searchMode   string
	searchDetail string
	searchK      int
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
		return runV3Search(ctx, cmd.OutOrStdout(), strings.Join(args, " "))
	},
}

func runV3Search(ctx context.Context, out io.Writer, query string) error {
	if !validSearchMode(searchMode) {
		return fmt.Errorf("invalid --mode %q: want auto|lexical|semantic|filename", searchMode)
	}

	storeDir := os.Getenv("FOLDERMCP_STORE")
	if storeDir == "" {
		home, _ := os.UserHomeDir()
		storeDir = filepath.Join(home, ".foldermcp", "store", "default")
	}
	dbPath := filepath.Join(storeDir, "index.db")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no index at %s — run `foldermcp index-v3 <folder>` first", dbPath)
		}
		return fmt.Errorf("cannot access index at %s: %w", dbPath, err)
	}

	db, err := store.Open(store.Options{Path: dbPath, Tier: store.DetectTier(detectRAMMB()), ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Optional semantic search: nil embedder degrades to lexical (FTS+filename).
	configureOrtLib()
	var emb *embed.Embedder
	if modelPath, tokenizerPath, modelErr := resolveModelPaths(); modelErr != nil {
		fmt.Fprintf(os.Stderr, "foldermcp search: WARNING semantic search disabled: %v\n", modelErr)
	} else {
		emb = embed.NewEmbedder(modelPath, tokenizerPath)
		defer func() { _ = emb.Close() }() // free native ONNX session/tensors
	}

	// Read-side fingerprint gate: don't run fixed-scale query codes against an
	// index quantized with a different scheme (e.g. an old per-vector "int8"
	// store) — that silently returns garbage rankings. Degrade to lexical and
	// say why. `emb` (and its deferred Close above) stays the resource owner;
	// only the server's view is cleared, so cleanup still runs correctly.
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
		return fmt.Errorf("search: %w", err)
	}
	_, err = io.WriteString(out, formatSearchResults(resp, query))
	return err
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
	rootCmd.AddCommand(v3SearchCmd)
}
