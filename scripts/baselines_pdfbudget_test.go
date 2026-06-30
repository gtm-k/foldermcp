//go:build harness

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestOrderPDFsByQuery verifies query-matching PDFs sort ahead of the rest, with
// the remainder in deterministic alphabetical order (Finding-1 ordering fix).
func TestOrderPDFsByQuery(t *testing.T) {
	pdfs := []string{
		"/corpus/docs/zeta.pdf",
		"/corpus/docs/alpha_quasar.pdf",
		"/corpus/docs/beta.pdf",
	}
	terms := []SearchTerm{{Text: "quasar", Weight: 1.0}}
	got := orderPDFsByQuery(pdfs, terms)
	if got[0] != "/corpus/docs/alpha_quasar.pdf" {
		t.Errorf("query-matching PDF must sort first, got order %v", got)
	}
	// The two non-matching PDFs keep deterministic alphabetical order.
	if got[1] != "/corpus/docs/beta.pdf" || got[2] != "/corpus/docs/zeta.pdf" {
		t.Errorf("non-matching PDFs must be alphabetical, got %v", got)
	}
}

// TestGatherMatchesPDFBudgetNotStarvedByRgTerms is the regression for Finding 1:
// a natural-language query expanding to >= the call-budget floor of rg terms must
// NOT starve PDF extraction. Under the old single shared budget (rg first), the
// rg loop consumed the whole budget and ZERO PDFs were extracted; with the
// separate PDF budget all PDFs are extracted regardless of rg term count.
func TestGatherMatchesPDFBudgetNotStarvedByRgTerms(t *testing.T) {
	dir := t.TempDir()
	pdfNames := []string{"empty_text.pdf", "multi_page.pdf", "scanned_like.pdf"}
	for _, name := range pdfNames {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("%PDF-1.4\n"), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	// This query reformulates to >= callBudgetFloor terms (precondition asserted).
	terms := reformulate("exponential backoff retry logic for HTTP requests over the network")
	if len(terms) < callBudgetFloor {
		t.Fatalf("precondition: need >= %d reformulated terms, got %d", callBudgetFloor, len(terms))
	}

	origRg, origPdf := rgRun, pdftotextRun
	defer func() { rgRun, pdftotextRun = origRg, origPdf }()
	// rg finds nothing here — we are isolating PDF extraction. It still consumes
	// its full (separate) budget of search calls.
	rgRun = func(_ context.Context, _, _, _ string) ([]byte, error) { return nil, nil }
	var extracted []string
	pdftotextRun = func(_ context.Context, _, path string) (string, error) {
		extracted = append(extracted, filepath.Base(path))
		return "exponential backoff retry text\n", nil
	}

	cfg := baselineConfig{
		corpus:          dir,
		rgBin:           "rg",
		pdftotextBin:    "pdftotext",
		callBudget:      defaultCallBudget,
		readWindowLines: defaultReadWindowLines,
		readTopN:        defaultReadTopN,
	}
	matches, _, err := gatherMatches(context.Background(), cfg, terms, &tokenLedger{})
	if err != nil {
		t.Fatalf("gatherMatches: %v", err)
	}
	if len(extracted) != len(pdfNames) {
		t.Errorf("expected all %d PDFs extracted despite %d rg terms, got %d: %v",
			len(pdfNames), len(terms), len(extracted), extracted)
	}
	if len(matches) == 0 {
		t.Errorf("expected matches from extracted PDF text, got none")
	}
}
