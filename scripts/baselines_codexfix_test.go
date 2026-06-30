//go:build harness

package main

// Regression tests for the Codex post-code review (REVISE-CODE) of M2 Phase 9.
// Each test fails against the pre-fix code and passes after the fix.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNdcgAtKPathsDedupesDuplicateHits — Codex HIGH (metrics.go): a relevant file
// appearing as two hits in the top-k must be credited ONCE; IDCG must be over
// distinct relevant docs. Otherwise NDCG exceeds 1.0 and the reported metric is
// corrupted.
func TestNdcgAtKPathsDedupesDuplicateHits(t *testing.T) {
	judgments := []Judgment{{Path: "a.go", Grade: 2}}
	paths := []string{"/w/a.go", "/w/a.go"} // same relevant doc twice (two chunks)
	got := ndcgAtKPaths(paths, judgments, 10)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("ndcg with duplicate hits of one relevant doc = %v, want exactly 1.0", got)
	}
}

// TestNdcgAtKPathsTwoDocsNoInflation — a perfect ranking of two distinct relevant
// docs stays exactly 1.0 even when an earlier-ranked doc reappears later in the
// list.
func TestNdcgAtKPathsTwoDocsNoInflation(t *testing.T) {
	judgments := []Judgment{{Path: "a.go", Grade: 2}, {Path: "b.go", Grade: 1}}
	paths := []string{"/w/a.go", "/w/b.go", "/w/a.go"} // a.go repeats at rank 3
	got := ndcgAtKPaths(paths, judgments, 10)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("ndcg = %v, want 1.0 (duplicate of a.go must not add gain)", got)
	}
}

// TestStratumProseIsMarkdownKey — Codex LOW: the prose stratum's emitted key must
// be "md" so JSON per_stratum keys match the published methodology/schema/manifests.
func TestStratumProseIsMarkdownKey(t *testing.T) {
	if stratumProse != "md" {
		t.Errorf("stratumProse = %q, want \"md\"", stratumProse)
	}
	if got := stratumOf("docs/readme.md"); got != "md" {
		t.Errorf("stratumOf(.md) = %q, want \"md\"", got)
	}
}

// TestRawReadCountsPDFExtractedText — Codex HIGH (baselines.go): the raw-read
// denominator for a PDF candidate must include its FULL extracted text, not just
// the query. Skipping PDFs left the denominator ~0 for PDF wins, making the
// tokens<=0.1x raw-read gate meaningless for the PDF stratum.
func TestRawReadCountsPDFExtractedText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "multi_page.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	origRg, origPdf := rgRun, pdftotextRun
	defer func() { rgRun, pdftotextRun = origRg, origPdf }()
	rgRun = func(_ context.Context, _, _, _ string) ([]byte, error) { return nil, nil } // no code matches
	bigText := strings.Repeat("quasar telemetry readings ", 80)                          // ~2000 chars
	pdftotextRun = func(_ context.Context, _, _ string) (string, error) { return bigText, nil }

	cfg := baselineConfig{
		corpus: dir, rgBin: "rg", pdftotextBin: "pdftotext",
		callBudget: defaultCallBudget, pdfBudget: defaultCallBudget,
		readWindowLines: defaultReadWindowLines, readTopN: defaultReadTopN,
	}
	q := Query{ID: "p1", Text: "quasar telemetry", Judgments: []Judgment{{Path: "multi_page.pdf", Grade: 2}}}

	paths, tokens, err := runRawRead(context.Background(), cfg, q)
	if err != nil {
		t.Fatalf("runRawRead: %v", err)
	}
	if recallAtKPaths(paths, q.Judgments, 10) == 0 {
		t.Fatalf("PDF not surfaced as a raw-read candidate: %v", paths)
	}
	if want := approxTokenCount(bigText); tokens < want {
		t.Errorf("raw-read tokens = %d, want >= %d (full extracted PDF text must be counted)", tokens, want)
	}
}

// TestGatherMatchesPDFBudgetIsSeparateField — Codex MEDIUM: PDF extraction draws
// from cfg.pdfBudget, independent of cfg.callBudget (the two budgets are now
// explicit). With rg finding nothing and pdfBudget covering all PDFs, every PDF
// is still extracted even when callBudget is exhausted by rg terms.
func TestGatherMatchesPDFBudgetIsSeparateField(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.pdf", "b.pdf"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("%PDF-1.4\n"), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	origRg, origPdf := rgRun, pdftotextRun
	defer func() { rgRun, pdftotextRun = origRg, origPdf }()
	rgRun = func(_ context.Context, _, _, _ string) ([]byte, error) { return nil, nil }
	var extracted int
	pdftotextRun = func(_ context.Context, _, _ string) (string, error) {
		extracted++
		return "quasar telemetry\n", nil
	}

	cfg := baselineConfig{
		corpus: dir, rgBin: "rg", pdftotextBin: "pdftotext",
		callBudget: defaultCallBudget, pdfBudget: defaultCallBudget,
		readWindowLines: defaultReadWindowLines, readTopN: defaultReadTopN,
	}
	terms := reformulate("quasar telemetry")
	if _, _, err := gatherMatches(context.Background(), cfg, terms, &tokenLedger{}); err != nil {
		t.Fatalf("gatherMatches: %v", err)
	}
	if extracted != 2 {
		t.Errorf("expected both PDFs extracted under the separate pdfBudget, got %d", extracted)
	}
}

// TestFindPDFsCountsWalkErrors — Codex re-review Finding 4: per-entry PDF
// discovery walk errors must be observable (path logged) and counted, not
// silently swallowed. A missing root triggers exactly one walk-error callback.
func TestFindPDFsCountsWalkErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	pdfs, walkErrs, err := findPDFs(missing)
	if err != nil {
		t.Fatalf("findPDFs returned a hard error: %v", err)
	}
	if len(pdfs) != 0 {
		t.Errorf("expected no pdfs under a missing root, got %v", pdfs)
	}
	if walkErrs == 0 {
		t.Errorf("expected walkErrs > 0 for an unreadable/missing root, got 0")
	}
}

// TestFindPDFsCleanDir — a clean directory yields its .pdf files with zero walk
// errors (the common path stays correct after the signature change).
func TestFindPDFsCleanDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdfs, walkErrs, err := findPDFs(dir)
	if err != nil || walkErrs != 0 {
		t.Fatalf("clean dir: err=%v walkErrs=%d", err, walkErrs)
	}
	if len(pdfs) != 1 || filepath.Base(pdfs[0]) != "a.pdf" {
		t.Errorf("expected [a.pdf], got %v", pdfs)
	}
}
