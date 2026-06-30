//go:build harness

package main

import (
	"math"
	"testing"
)

// ── Reformulation ────────────────────────────────────────

func TestSplitCamel(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"parseHTTPResponse", []string{"parse", "HTTP", "Response"}},
		{"getUserName", []string{"get", "User", "Name"}},
		{"lowercase", []string{"lowercase"}},
		{"HTTP", []string{"HTTP"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := splitCamel(tt.in)
		if !equalStrings(got, tt.want) {
			t.Errorf("splitCamel(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestSplitIdentifiers(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"parseHTTPResponse_code", []string{"parse", "http", "response", "code"}},
		{"snake_case_name", []string{"snake", "case", "name"}},
		{"kebab-case-term", []string{"kebab", "case", "term"}},
		{"plain words here", []string{"plain", "words", "here"}},
	}
	for _, tt := range tests {
		got := splitIdentifiers(tt.in)
		if !equalStrings(got, tt.want) {
			t.Errorf("splitIdentifiers(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestExtractKeywords(t *testing.T) {
	// "for" and "with" are stopwords; "a" is too short; rest are content terms.
	got := extractKeywords("exponential backoff for retry with a HTTP client")
	want := []string{"exponential", "backoff", "retry", "http", "client"}
	if !equalStrings(got, want) {
		t.Errorf("extractKeywords = %v, want %v", got, want)
	}
}

func TestReformulateDeterministic(t *testing.T) {
	a := reformulate("exponential backoff retry logic for HTTP")
	b := reformulate("exponential backoff retry logic for HTTP")
	if len(a) != len(b) {
		t.Fatalf("reformulate not deterministic: len %d != %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("reformulate not deterministic at %d: %+v != %+v", i, a[i], b[i])
		}
	}
	// Core keyword terms must be present at full weight.
	w := map[string]float64{}
	for _, st := range a {
		w[st.Text] = st.Weight
	}
	for _, core := range []string{"exponential", "backoff", "retry", "logic", "http"} {
		if w[core] != weightKeyword {
			t.Errorf("term %q weight = %v, want %v (full keyword weight)", core, w[core], weightKeyword)
		}
	}
	// Synonyms of "retry" / "http" should appear at synonym weight (lower).
	if _, ok := w["retries"]; !ok {
		t.Errorf("expected synonym 'retries' from 'retry' in reformulation")
	}
	if w["retries"] != weightSyn {
		t.Errorf("synonym 'retries' weight = %v, want %v", w["retries"], weightSyn)
	}
	// No stopwords in output.
	if _, ok := w["for"]; ok {
		t.Errorf("stopword 'for' leaked into reformulation")
	}
}

func TestReformulateNoDuplicates(t *testing.T) {
	got := reformulate("cache cache caching")
	seen := map[string]bool{}
	for _, st := range got {
		if seen[st.Text] {
			t.Errorf("duplicate term %q in reformulation", st.Text)
		}
		seen[st.Text] = true
	}
}

// ── Budget ───────────────────────────────────────────────

func TestClampBudget(t *testing.T) {
	cases := map[int]int{0: callBudgetFloor, 1: callBudgetFloor, 7: callBudgetFloor, 8: 8, 20: 20, -5: callBudgetFloor}
	for in, want := range cases {
		if got := clampBudget(in); got != want {
			t.Errorf("clampBudget(%d) = %d, want %d", in, got, want)
		}
	}
}

// ── rg --json parsing ────────────────────────────────────

const rgJSONSample = `{"type":"begin","data":{"path":{"text":"micro/code-python/session_manager.py"}}}
{"type":"match","data":{"path":{"text":"micro/code-python/session_manager.py"},"lines":{"text":"class RetryConfig:\n"},"line_number":17,"absolute_offset":432,"submatches":[{"match":{"text":"RetryConfig"},"start":6,"end":17}]}}
{"type":"match","data":{"path":{"text":"micro/code-python/session_manager.py"},"lines":{"text":"        config = RetryConfig(max_retries=5)\n"},"line_number":89,"absolute_offset":2580,"submatches":[{"match":{"text":"RetryConfig"},"start":17,"end":28}]}}
{"type":"end","data":{"path":{"text":"micro/code-python/session_manager.py"},"binary_offset":null,"stats":{"matched_lines":2,"matches":2}}}
{"data":{"elapsed_total":{"human":"0.01s"}},"type":"summary"}`

func TestParseRgJSON(t *testing.T) {
	got := parseRgJSON([]byte(rgJSONSample), "retryconfig")
	if len(got) != 2 {
		t.Fatalf("parseRgJSON returned %d matches, want 2", len(got))
	}
	if got[0].Path != "micro/code-python/session_manager.py" {
		t.Errorf("match[0].Path = %q", got[0].Path)
	}
	if got[0].Line != 17 {
		t.Errorf("match[0].Line = %d, want 17", got[0].Line)
	}
	if got[0].Term != "retryconfig" {
		t.Errorf("match[0].Term = %q, want retryconfig", got[0].Term)
	}
	if got[0].Text != "class RetryConfig:" {
		t.Errorf("match[0].Text = %q (trailing newline should be trimmed)", got[0].Text)
	}
	if got[1].Line != 89 {
		t.Errorf("match[1].Line = %d, want 89", got[1].Line)
	}
}

func TestParseRgJSONJunk(t *testing.T) {
	// Non-JSON / empty lines are skipped, not fatal.
	got := parseRgJSON([]byte("not json\n\n{\"type\":\"summary\"}\n"), "x")
	if len(got) != 0 {
		t.Errorf("parseRgJSON(junk) = %d matches, want 0", len(got))
	}
}

// ── Candidate ranking ────────────────────────────────────

func TestRankCandidatesOrder(t *testing.T) {
	terms := []SearchTerm{
		{Text: "alpha", Weight: 1.0},
		{Text: "beta", Weight: 1.0},
	}
	matches := []GrepMatch{
		// fileA: both terms, co-located (high evidence)
		{Path: "fileA.go", Line: 10, Term: "alpha"},
		{Path: "fileA.go", Line: 11, Term: "beta"},
		// fileB: one term once (low evidence)
		{Path: "fileB.go", Line: 5, Term: "alpha"},
	}
	got := rankCandidates(matches, terms)
	if len(got) != 2 {
		t.Fatalf("rankCandidates returned %d candidates, want 2", len(got))
	}
	if got[0].Path != "fileA.go" {
		t.Errorf("top candidate = %q, want fileA.go (more distinct terms + proximity)", got[0].Path)
	}
	if got[0].Score <= got[1].Score {
		t.Errorf("expected fileA score (%v) > fileB score (%v)", got[0].Score, got[1].Score)
	}
}

func TestRankCandidatesDeterministicTie(t *testing.T) {
	terms := []SearchTerm{{Text: "x", Weight: 1.0}}
	matches := []GrepMatch{
		{Path: "z.go", Line: 1, Term: "x"},
		{Path: "a.go", Line: 1, Term: "x"},
	}
	got := rankCandidates(matches, terms)
	// Equal scores → tie-break by path ascending.
	if got[0].Path != "a.go" {
		t.Errorf("tie-break failed: top = %q, want a.go", got[0].Path)
	}
}

func TestScanTextForTerms(t *testing.T) {
	terms := []SearchTerm{{Text: "quasar", Weight: 1.0}, {Text: "telemetry", Weight: 1.0}}
	text := "intro line\nQuasar Telemetry readings\nother\n"
	got := scanTextForTerms("doc.pdf", text, terms)
	if len(got) != 2 { // both terms match line 2 (case-insensitive)
		t.Fatalf("scanTextForTerms returned %d matches, want 2", len(got))
	}
	for _, m := range got {
		if m.Path != "doc.pdf" || m.Line != 2 {
			t.Errorf("unexpected match %+v", m)
		}
	}
}

// ── Stratum bucketing ────────────────────────────────────

func TestStratumOf(t *testing.T) {
	cases := map[string]string{
		"code-python/cache.py":  stratumCode,
		"code-go/config.go":     stratumCode,
		"docs-markdown/arch.md": stratumProse,
		"notes.txt":             stratumProse,
		"docs/multi_page.pdf":   stratumPDF,
		"report.docx":           stratumPDF,
		"resources/data.csv":    stratumCSV,
		"config.yaml":           stratumCSV,
		"data.json":             stratumCSV,
		"image.png":             stratumOther,
		"noext":                 stratumOther,
		"":                      stratumOther,
	}
	for path, want := range cases {
		if got := stratumOf(path); got != want {
			t.Errorf("stratumOf(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestQueryStratum(t *testing.T) {
	// highest-grade relevant judgment drives the stratum
	q := Query{Judgments: []Judgment{
		{Path: "code-go/x.go", Grade: 1},
		{Path: "docs/y.pdf", Grade: 2},
	}}
	if got := queryStratum(q); got != stratumPDF {
		t.Errorf("queryStratum = %q, want pdf (highest grade judgment)", got)
	}
	// all grade-0 → other
	q0 := Query{Judgments: []Judgment{{Path: "a.go", Grade: 0}}}
	if got := queryStratum(q0); got != stratumOther {
		t.Errorf("queryStratum(all grade 0) = %q, want other", got)
	}
}

// ── Token counting ───────────────────────────────────────

func TestApproxTokenCount(t *testing.T) {
	if approxTokenCount("") != 0 {
		t.Errorf("approxTokenCount(empty) != 0")
	}
	// ~4 chars per token, rounded up.
	if got := approxTokenCount("abcd"); got != 1 {
		t.Errorf("approxTokenCount(4 chars) = %d, want 1", got)
	}
	if got := approxTokenCount("abcde"); got != 2 {
		t.Errorf("approxTokenCount(5 chars) = %d, want 2", got)
	}
	// monotonic
	if approxTokenCount("a longer string here") <= approxTokenCount("short") {
		t.Errorf("approxTokenCount not monotonic with length")
	}
}

func TestTokenLedger(t *testing.T) {
	l := &tokenLedger{}
	l.addQuery("retry backoff")     // 13 chars -> 4 tokens
	l.addRead("some response body") // 18 chars -> 5 tokens
	if l.query == 0 || l.read == 0 {
		t.Fatalf("ledger did not accumulate: %+v", l)
	}
	if l.total() != l.query+l.read {
		t.Errorf("total() = %d, want query+read = %d", l.total(), l.query+l.read)
	}
}

// ── Aggregation ──────────────────────────────────────────

func TestAggregate(t *testing.T) {
	evals := []queryEval{
		{Stratum: stratumCode, Recall5: 1.0, Recall10: 1.0, NDCG10: 1.0, Tokens: 100},
		{Stratum: stratumCode, Recall5: 0.0, Recall10: 0.0, NDCG10: 0.0, Tokens: 200},
		{Stratum: stratumPDF, Recall5: 0.5, Recall10: 1.0, NDCG10: 0.8, Tokens: 300},
	}
	got := aggregate(evals)

	// aggregate means
	if math.Abs(got.Aggregate.RecallAt10-(1.0+0.0+1.0)/3) > 1e-9 {
		t.Errorf("aggregate recall@10 = %v", got.Aggregate.RecallAt10)
	}
	if got.Aggregate.TokensTotal != 600 {
		t.Errorf("aggregate tokens_total = %d, want 600", got.Aggregate.TokensTotal)
	}
	if got.Aggregate.QueriesEvaluated != 3 {
		t.Errorf("aggregate queries = %d, want 3", got.Aggregate.QueriesEvaluated)
	}
	if math.Abs(got.Aggregate.TokensPerQuery-200) > 1e-9 {
		t.Errorf("aggregate tokens_per_query = %v, want 200", got.Aggregate.TokensPerQuery)
	}

	// per-stratum
	code, ok := got.PerStratum[stratumCode]
	if !ok {
		t.Fatalf("missing code stratum")
	}
	if math.Abs(code.RecallAt10-0.5) > 1e-9 {
		t.Errorf("code recall@10 = %v, want 0.5", code.RecallAt10)
	}
	if code.QueriesEvaluated != 2 {
		t.Errorf("code queries = %d, want 2", code.QueriesEvaluated)
	}
	pdf, ok := got.PerStratum[stratumPDF]
	if !ok {
		t.Fatalf("missing pdf stratum")
	}
	if math.Abs(pdf.RecallAt5-0.5) > 1e-9 {
		t.Errorf("pdf recall@5 = %v, want 0.5", pdf.RecallAt5)
	}
}

func TestAggregateEmpty(t *testing.T) {
	got := aggregate(nil)
	if got.Aggregate.QueriesEvaluated != 0 {
		t.Errorf("empty aggregate queries = %d, want 0", got.Aggregate.QueriesEvaluated)
	}
	if len(got.PerStratum) != 0 {
		t.Errorf("empty aggregate per_stratum should be empty, got %v", got.PerStratum)
	}
}

// ── Path-based metric cores (recall@5 cutoff) ────────────

func TestRecallAtKPathsCutoff(t *testing.T) {
	judgments := []Judgment{
		{Path: "a.go", Grade: 2},
		{Path: "b.go", Grade: 2},
	}
	// a.go at rank 1, b.go at rank 7
	paths := []string{
		"a.go", "x1", "x2", "x3", "x4", "x5", "b.go", "x7", "x8", "x9",
	}
	r5 := recallAtKPaths(paths, judgments, 5)
	r10 := recallAtKPaths(paths, judgments, 10)
	if math.Abs(r5-0.5) > 1e-9 {
		t.Errorf("recall@5 = %v, want 0.5 (only a.go within top 5)", r5)
	}
	if math.Abs(r10-1.0) > 1e-9 {
		t.Errorf("recall@10 = %v, want 1.0 (both within top 10)", r10)
	}
}

func TestNdcgAtKPathsMatchesWrapper(t *testing.T) {
	judgments := []Judgment{
		{Path: "code-python/session_manager.py", Grade: 2},
		{Path: "code-go/config.go", Grade: 1},
	}
	paths := []string{
		"/workspace/code-python/session_manager.py",
		"/workspace/code-go/config.go",
	}
	got := ndcgAtKPaths(paths, judgments, 10)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("ndcgAtKPaths perfect ranking = %v, want 1.0", got)
	}
}

// ── helpers ──────────────────────────────────────────────

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
