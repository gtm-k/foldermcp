//go:build harness

// baselines.go — deterministic, non-LLM retrieval baselines for the harness.
//
//   - agentic-grep : mimics an agent driving ripgrep + bounded window reads +
//     pdftotext, with deterministic query reformulation (fixed templates) and a
//     per-query tool-call budget. Emits ranked file:line candidates scored by
//     evidence, comparable to SearchHit paths so recall@k/ndcg@k reuse the same
//     metric functions.
//   - raw-read     : same candidate discovery but reads candidate files WHOLESALE
//     (never bounded). Used only to measure the token-efficiency ratio.
//
// The PURE pieces (reformulation, rg-JSON parsing, candidate ranking, text
// scanning, budget clamping) are unit-tested with in-memory fixtures and require
// no live server, no rg, and no pdftotext. The thin I/O wrappers (runRipgrep,
// readWindowLines, runPdftotext, findPDFs) shell out / touch the filesystem and
// are exercised only in the live e2e run.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// ── Tunable constants ────────────────────────────────────

const (
	// Reformulation term weights (keyword > identifier-split > synonym).
	weightKeyword = 1.0
	weightIdent   = 0.8
	weightSyn     = 0.5

	// Candidate scoring.
	defaultTermWeight = 0.5  // weight for a matched term not in the term set
	extraMatchBonus   = 0.1  // bonus per additional match of the same term
	maxExtraMatches   = 5    // cap on counted repeat matches
	proximityWindow   = 3    // lines: co-occurring distinct terms within window
	proximityBonus    = 0.25 // bonus per extra distinct term co-occurring

	// Per-query tool-call budget. Floor 8 (D16). The default is a PILOT
	// PLACEHOLDER — the calibrated value comes from real agent transcripts in
	// step 9d. Do not treat 8 as tuned.
	defaultCallBudget = 8
	callBudgetFloor   = 8

	// Bounded reads.
	defaultReadWindowLines = 20 // ± lines around a candidate line (agentic-grep)
	defaultReadTopN        = 3  // candidates confirmed (agentic) / read whole (raw)
)

// stopwords is a small fixed English/query stopword set used by reformulation.
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "for": true, "of": true, "to": true,
	"in": true, "on": true, "and": true, "or": true, "with": true, "without": true,
	"vs": true, "versus": true, "is": true, "are": true, "be": true, "by": true,
	"as": true, "at": true, "into": true, "via": true, "per": true,
}

func isStopword(w string) bool { return stopwords[w] }

// defaultSynonyms is a small fixed domain synonym table (template 2). Kept
// deliberately small and documented; not learned, not exhaustive.
var defaultSynonyms = map[string][]string{
	"retry":      {"backoff", "retries"},
	"cache":      {"caching", "lru"},
	"config":     {"configuration", "settings"},
	"concurrent": {"concurrency", "parallel"},
	"auth":       {"authentication", "authorization"},
	"http":       {"rest", "api"},
	"validate":   {"validation", "schema"},
	"queue":      {"pool", "worker"},
	"limit":      {"throttle", "rate"},
	"log":        {"logging", "logger"},
}

// ── Query reformulation (deterministic, fixed templates) ─

// SearchTerm is a reformulated query term with an evidence weight.
type SearchTerm struct {
	Text   string
	Weight float64
}

// splitCamel splits a single word on camelCase / acronym / letter-digit
// boundaries, preserving original casing (callers lowercase). e.g.
// "parseHTTPResponse" -> ["parse","HTTP","Response"].
func splitCamel(word string) []string {
	if word == "" {
		return nil
	}
	r := []rune(word)
	var parts []string
	start := 0
	for i := 1; i < len(r); i++ {
		boundary := false
		switch {
		case unicode.IsUpper(r[i]) && unicode.IsLower(r[i-1]):
			boundary = true // aB -> a | B
		case unicode.IsUpper(r[i]) && unicode.IsUpper(r[i-1]) && i+1 < len(r) && unicode.IsLower(r[i+1]):
			boundary = true // ABc -> A | Bc (acronym end)
		case unicode.IsDigit(r[i]) != unicode.IsDigit(r[i-1]):
			boundary = true // letter<->digit
		}
		if boundary {
			parts = append(parts, string(r[start:i]))
			start = i
		}
	}
	return append(parts, string(r[start:]))
}

// splitIdentifiers (template 1) finds identifier-like runs and splits them on
// snake_case, kebab-case, and camelCase into lowercased sub-terms.
func splitIdentifiers(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		tok := cur.String()
		cur.Reset()
		for _, sub := range strings.FieldsFunc(tok, func(r rune) bool { return r == '_' || r == '-' }) {
			for _, c := range splitCamel(sub) {
				out = append(out, strings.ToLower(c))
			}
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// extractKeywords (template 3) tokenizes on non-alphanumeric, lowercases, and
// drops stopwords and length-1 tokens.
func extractKeywords(query string) []string {
	var out []string
	for _, tok := range strings.FieldsFunc(query, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	}) {
		t := strings.ToLower(tok)
		if len(t) < 2 || isStopword(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// applySynonymsList (template 2) returns the synonyms of any terms present in
// the table (in input order).
func applySynonymsList(terms []string, table map[string][]string) []string {
	var out []string
	for _, t := range terms {
		if syns, ok := table[t]; ok {
			out = append(out, syns...)
		}
	}
	return out
}

// reformulate applies the three templates in FIXED order — (1) identifier-split,
// (2) synonym expansion, (3) keyword extraction — deduping by lowercased text
// (keeping the maximum weight) and dropping stopwords. Deterministic.
func reformulate(query string) []SearchTerm {
	var out []SearchTerm
	idx := make(map[string]int)
	add := func(text string, w float64) {
		text = strings.ToLower(strings.TrimSpace(text))
		if len(text) < 2 || isStopword(text) {
			return
		}
		if i, ok := idx[text]; ok {
			if w > out[i].Weight {
				out[i].Weight = w
			}
			return
		}
		idx[text] = len(out)
		out = append(out, SearchTerm{Text: text, Weight: w})
	}

	// (1) identifier-split
	for _, t := range splitIdentifiers(query) {
		add(t, weightIdent)
	}
	// (2) synonyms of terms gathered so far
	base := make([]string, len(out))
	for i := range out {
		base[i] = out[i].Text
	}
	for _, t := range applySynonymsList(base, defaultSynonyms) {
		add(t, weightSyn)
	}
	// (3) keyword extraction (core content terms get full weight)
	for _, t := range extractKeywords(query) {
		add(t, weightKeyword)
	}
	return out
}

// clampBudget enforces the floor (D16): a per-query tool-call budget below the
// floor is raised to it.
func clampBudget(n int) int {
	if n < callBudgetFloor {
		return callBudgetFloor
	}
	return n
}

// ── ripgrep --json parsing (pure) ────────────────────────

// GrepMatch is one file:line match attributed to the search term that found it.
type GrepMatch struct {
	Path string
	Line int
	Text string
	Term string
}

// rgLine is the subset of ripgrep's --json message we consume.
type rgLine struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

// parseRgJSON parses ripgrep --json output (newline-delimited JSON) into matches,
// attributing each to term. Non-match / malformed lines are skipped.
func parseRgJSON(data []byte, term string) []GrepMatch {
	var out []GrepMatch
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rl rgLine
		if err := json.Unmarshal(line, &rl); err != nil {
			continue
		}
		if rl.Type != "match" {
			continue
		}
		out = append(out, GrepMatch{
			Path: rl.Data.Path.Text,
			Line: rl.Data.LineNumber,
			Text: strings.TrimRight(rl.Data.Lines.Text, "\r\n"),
			Term: term,
		})
	}
	return out
}

// scanTextForTerms (used for extracted PDF text, which rg cannot read directly)
// produces a match per (line, term) where the lowercased line contains the term.
func scanTextForTerms(path, text string, terms []SearchTerm) []GrepMatch {
	var out []GrepMatch
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		ll := strings.ToLower(line)
		for _, t := range terms {
			if strings.Contains(ll, t.Text) {
				out = append(out, GrepMatch{Path: path, Line: i + 1, Text: strings.TrimRight(line, "\r"), Term: t.Text})
			}
		}
	}
	return out
}

// ── Candidate ranking (pure) ─────────────────────────────

// Candidate is a ranked file:line answer scored by evidence.
type Candidate struct {
	Path  string
	Line  int
	Score float64
}

// bestProximity finds the line maximizing the count of DISTINCT terms whose
// matches fall within ±window lines, returning that line and the count. Ties
// resolve to the lowest line number.
func bestProximity(ms []GrepMatch, window int) (int, int) {
	bestLine, best := 0, 0
	// stable order so ties are deterministic
	order := append([]GrepMatch(nil), ms...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Line < order[j].Line })
	for _, center := range order {
		seen := make(map[string]bool)
		for _, m := range order {
			if m.Line >= center.Line-window && m.Line <= center.Line+window {
				seen[m.Term] = true
			}
		}
		if len(seen) > best {
			best = len(seen)
			bestLine = center.Line
		}
	}
	return bestLine, best
}

// rankCandidates aggregates matches by file into ranked file:line candidates.
// Score = Σ over distinct matched terms of weight*(1+bonus*repeats) plus a
// proximity bonus for co-occurring distinct terms. Sorted by score desc, then
// path asc, then line asc (deterministic). One candidate per file.
func rankCandidates(matches []GrepMatch, terms []SearchTerm) []Candidate {
	weight := make(map[string]float64, len(terms))
	for _, t := range terms {
		weight[t.Text] = t.Weight
	}

	byPath := make(map[string][]GrepMatch)
	var order []string
	for _, m := range matches {
		if _, ok := byPath[m.Path]; !ok {
			order = append(order, m.Path)
		}
		byPath[m.Path] = append(byPath[m.Path], m)
	}

	var cands []Candidate
	for _, p := range order {
		ms := byPath[p]
		termCount := make(map[string]int)
		for _, m := range ms {
			termCount[m.Term]++
		}
		score := 0.0
		for term, cnt := range termCount {
			w, ok := weight[term]
			if !ok {
				w = defaultTermWeight
			}
			extra := cnt - 1
			if extra > maxExtraMatches {
				extra = maxExtraMatches
			}
			score += w * (1 + extraMatchBonus*float64(extra))
		}
		bestLine, prox := bestProximity(ms, proximityWindow)
		if prox >= 2 {
			score += float64(prox-1) * proximityBonus
		}
		cands = append(cands, Candidate{Path: p, Line: bestLine, Score: score})
	}

	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		if cands[i].Path != cands[j].Path {
			return cands[i].Path < cands[j].Path
		}
		return cands[i].Line < cands[j].Line
	})
	return cands
}

// candidatePaths flattens ranked candidates to their paths (ranked order),
// normalized to forward slashes so the substring match in recallAtKPaths works
// against forward-slash judgment paths on every OS (Windows walks/rg emit
// backslashes). File I/O upstream still uses the native Candidate.Path.
func candidatePaths(cands []Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = filepath.ToSlash(c.Path)
	}
	return out
}

// ── Live I/O wrappers (exercised in e2e only) ────────────

// baselineConfig holds the resolved tool paths and budgets for a baseline run.
type baselineConfig struct {
	corpus          string
	rgBin           string
	pdftotextBin    string // "" if unavailable
	callBudget      int
	readWindowLines int
	readTopN        int
}

// rgRun / pdftotextRun are indirection seams so the baseline orchestration
// (gatherMatches, runAgenticGrep, runRawRead) can be unit-tested offline with an
// in-memory grep, without a real ripgrep/pdftotext binary on the box. Production
// uses the real subprocess wrappers below.
var (
	rgRun        = runRipgrep
	pdftotextRun = runPdftotext
)

// runRipgrep runs `rg --json --no-ignore --hidden -F -i -- <pattern> <root>`.
// ripgrep exit code 1 (no matches) is not an error; exit code 2 is a real
// failure.
//
// --no-ignore and --hidden are REQUIRED for D16 reproducibility: by default
// ripgrep honors .gitignore/.ignore files, the user's GLOBAL git excludes
// (core.excludesFile / ~/.config/git/ignore), and skips dotfiles. Those ambient
// rules apply regardless of where the corpus lives, so without these flags grep
// matches would silently depend on the host's git config and the corpus's
// location — under-counting grep recall and inflating hybrid's lift on a
// different reviewer's machine. With them, matching depends ONLY on corpus
// content. See docs/bench-methodology.md §5.
func runRipgrep(ctx context.Context, rgBin, pattern, root string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, rgBin, "--json", "--no-ignore", "--hidden", "-F", "-i", "--", pattern, root)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return out, nil // no matches
		}
		return out, err
	}
	return out, nil
}

// readWindowLines reads ONLY lines [center-window, center+window] (1-indexed)
// and stops scanning past the window — a bounded read, never whole-file.
func readWindowLines(path string, center, window int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	lo := center - window
	if lo < 1 {
		lo = 1
	}
	hi := center + window

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var b strings.Builder
	ln := 0
	for sc.Scan() {
		ln++
		if ln < lo {
			continue
		}
		if ln > hi {
			break
		}
		b.WriteString(sc.Text())
		b.WriteByte('\n')
	}
	return b.String(), sc.Err()
}

// runPdftotext extracts a PDF's text layer to stdout.
func runPdftotext(ctx context.Context, bin, pdfPath string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, pdfPath, "-").Output()
	return string(out), err
}

// findPDFs returns the .pdf files under root (bounded discovery walk).
func findPDFs(root string) ([]string, error) {
	var pdfs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, don't abort
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".pdf") {
			pdfs = append(pdfs, p)
		}
		return nil
	})
	sort.Strings(pdfs) // deterministic order
	return pdfs, err
}

// orderPDFsByQuery reorders discovered PDFs so that those whose PATH matches a
// reformulated query term are extracted first, with the remainder in stable
// alphabetical order. This is what an agent "using grep well" (D16) does — it
// extracts the query-relevant PDFs before unrelated ones — and it stops
// alphabetically-earlier negative fixtures (e.g. empty_text.pdf) from crowding
// out a relevant target (e.g. multi_page.pdf) when the PDF budget is tight.
//
// The ordering is QUERY-driven (a shared, fair input), never JUDGMENT-driven, so
// it does not advantage any baseline under D16. Deterministic: ties break on path.
func orderPDFsByQuery(pdfs []string, terms []SearchTerm) []string {
	type scoredPDF struct {
		path  string
		match bool
	}
	scored := make([]scoredPDF, len(pdfs))
	for i, p := range pdfs {
		lp := strings.ToLower(filepath.ToSlash(p))
		hit := false
		for _, t := range terms {
			if strings.Contains(lp, t.Text) {
				hit = true
				break
			}
		}
		scored[i] = scoredPDF{path: p, match: hit}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].match != scored[j].match {
			return scored[i].match // path-matching PDFs first
		}
		return scored[i].path < scored[j].path
	})
	out := make([]string, len(scored))
	for i, s := range scored {
		out[i] = s.path
	}
	return out
}

// gatherMatches drives ripgrep (and pdftotext for PDFs) over the reformulated
// terms, accumulating matches and tool-I/O tokens. Returns matches, the number
// of rg SEARCH calls spent (used to bound the agentic confirm-read loop), or a
// visible error.
//
// Budget split (D16 fairness — Finding-1 fix): the rg search loop and the
// pdftotext extraction loop draw from SEPARATE budgets. Previously both shared a
// single budget with rg running first, so a natural-language query that expands
// to >=8 terms consumed the whole budget on rg and ZERO PDFs were extracted —
// structurally giving grep recall 0 on every PDF-target query for reasons
// unrelated to grep's concept-matching weakness. Extracting a PDF's text layer is
// not a search-refinement step competing with rg synonym calls; an agent always
// does it once per relevant PDF. So pdftotext gets its own clamped budget, and
// PDFs are ordered query-first (orderPDFsByQuery) so a relevant target is never
// crowded out by alphabetically-earlier negative fixtures. The returned call
// count reflects rg search calls only; PDF extraction has its own allowance and
// all of its read I/O is still recorded in the token ledger.
func gatherMatches(ctx context.Context, cfg baselineConfig, terms []SearchTerm, ledger *tokenLedger) ([]GrepMatch, int, error) {
	budget := clampBudget(cfg.callBudget)
	calls := 0
	var matches []GrepMatch

	for _, t := range terms {
		if calls >= budget {
			break
		}
		ledger.addQuery(t.Text)
		out, err := rgRun(ctx, cfg.rgBin, t.Text, cfg.corpus)
		calls++
		if err != nil {
			return nil, calls, fmt.Errorf("ripgrep %q failed: %w", t.Text, err)
		}
		ledger.addRead(string(out))
		matches = append(matches, parseRgJSON(out, t.Text)...)
	}

	// PDF stratum: rg cannot read PDF text, so extract with pdftotext (if present).
	// SEPARATE budget so a multi-term rg expansion can never starve PDF extraction.
	if cfg.pdftotextBin != "" {
		pdfs, _ := findPDFs(cfg.corpus)
		pdfs = orderPDFsByQuery(pdfs, terms)
		pdfBudget := clampBudget(cfg.callBudget)
		pdfCalls := 0
		for _, p := range pdfs {
			if pdfCalls >= pdfBudget {
				break
			}
			txt, err := pdftotextRun(ctx, cfg.pdftotextBin, p)
			pdfCalls++
			if err != nil {
				continue // pdftotext failure on one file is non-fatal (warned upfront)
			}
			ledger.addRead(txt)
			matches = append(matches, scanTextForTerms(p, txt, terms)...)
		}
	}
	return matches, calls, nil
}

// runAgenticGrep executes the agentic-grep baseline for one query and returns
// ranked candidate paths plus total tool-I/O tokens. Bounded window reads of the
// top candidates add realistic read tokens without ever reading whole files.
func runAgenticGrep(ctx context.Context, cfg baselineConfig, q Query) ([]string, int, error) {
	terms := reformulate(q.Text)
	ledger := &tokenLedger{}
	matches, calls, err := gatherMatches(ctx, cfg, terms, ledger)
	if err != nil {
		return nil, 0, err
	}
	cands := rankCandidates(matches, terms)

	budget := clampBudget(cfg.callBudget)
	readErrs := 0
	for i := 0; i < len(cands) && i < cfg.readTopN; i++ {
		if calls >= budget {
			break
		}
		if strings.EqualFold(filepath.Ext(cands[i].Path), ".pdf") {
			continue // PDF text already ingested via pdftotext
		}
		w, err := readWindowLines(cands[i].Path, cands[i].Line, cfg.readWindowLines)
		calls++
		if err != nil {
			// Surface the failure VISIBLY (Finding-9 fix): a swallowed read silently
			// under-counts the token tally for this query with no observable signal.
			readErrs++
			fmt.Fprintf(os.Stderr, "  [agentic-grep] window read %q: %v\n", cands[i].Path, err)
			continue
		}
		ledger.addRead(w)
	}
	if readErrs > 0 {
		fmt.Fprintf(os.Stderr, "  [agentic-grep] %d candidate window read(s) failed for %q — token tally is partial\n", readErrs, q.Text)
	}
	return candidatePaths(cands), ledger.total(), nil
}

// runRawRead executes the raw-read baseline: the no-index token-cost ceiling.
// Candidate discovery (grep + pdftotext) is used ONLY to PICK which files an
// index-less agent reads; reads of the top candidates are WHOLESALE (never
// bounded). The returned token count is the denominator of the token-efficiency
// ratio.
//
// Counted surface (Findings 2 & 7 fix): the token tally counts ONLY the model-
// ingested surface — the query in plus the full file contents out — and EXCLUDES
// the rg-scan / pdftotext discovery I/O. This mirrors the hybrid surface (query
// in + path/title/snippet out), which likewise excludes the index's internal
// retrieval work, so the hybrid-vs-raw-read comparison is over an IDENTICAL
// surface per D16 §5. Previously the discovery ledger (≈one full-corpus rg scan
// per reformulated term, plus every PDF's extracted text) was folded into the
// raw-read denominator, inflating it and flattering the index's cost ratio. The
// discovery ledger is now kept separate and discarded. See docs/bench-methodology.md §5.
func runRawRead(ctx context.Context, cfg baselineConfig, q Query) ([]string, int, error) {
	terms := reformulate(q.Text)
	// Discovery I/O is NOT part of the counted surface — a throwaway ledger.
	discovery := &tokenLedger{}
	matches, _, err := gatherMatches(ctx, cfg, terms, discovery)
	if err != nil {
		return nil, 0, err
	}
	cands := rankCandidates(matches, terms)

	// Counted surface: the query issued + the full file contents read wholesale.
	ledger := &tokenLedger{}
	ledger.addQuery(q.Text)
	reads, readErrs := 0, 0
	seen := make(map[string]bool)
	for _, c := range cands {
		if reads >= cfg.readTopN {
			break
		}
		if seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		if strings.EqualFold(filepath.Ext(c.Path), ".pdf") {
			continue // PDF text is not a wholesale text read (extracted in discovery)
		}
		data, err := os.ReadFile(c.Path) // wholesale read — the point of this baseline
		if err != nil {
			// Surface the failure VISIBLY (Finding-9 fix) rather than silently
			// omitting the file's tokens from the denominator.
			readErrs++
			fmt.Fprintf(os.Stderr, "  [raw-read] read %q: %v\n", c.Path, err)
			continue
		}
		ledger.addRead(string(data))
		reads++
	}
	if readErrs > 0 {
		fmt.Fprintf(os.Stderr, "  [raw-read] %d candidate file(s) unreadable for %q — denominator is partial\n", readErrs, q.Text)
	}
	return candidatePaths(cands), ledger.total(), nil
}
