//go:build cgo

package chunker

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// DataChunk is a chunk produced by a structured-data chunker (CSV/TSV via csv.go,
// JSON/YAML/XML via data.go). Unlike ExtractedChunk it carries its own Kind
// because one data file yields multiple chunk kinds (csv_schema + csv_rows, or
// data_structured). Byte offsets are SELF-RELATIVE to each chunk's synthesized
// Text (these chunks are summaries/outlines synthesized from the parsed
// structure, NOT slices of the raw file), so [ByteStart:ByteEnd] re-slices Text.
type DataChunk struct {
	Text       string
	ByteStart  int
	ByteEnd    int
	TokenCount int
	Header     string // D18 structural summary (FTS-indexed via chunks_fts(header))
	Kind       string // "csv_schema" | "csv_rows" | "data_structured"
}

// DataStats are per-file structured-data extraction statistics persisted in the
// file node's properties JSON (D18) so retrieval quality is observable per file.
type DataStats struct {
	Rows             int    `json:"rows"`              // rows scanned (data rows, excluding header)
	Columns          int    `json:"columns"`           // CSV column count
	ScannedBytes     int    `json:"scanned_bytes"`     // bytes read from the file (<= csvScanCap)
	Truncated        bool   `json:"truncated"`         // true if the file exceeded the scan cap
	ExtractorVersion string `json:"extractor_version"` // provenance tag (R7)
}

// ErrDataFallback signals the caller (runner) that the structured-data chunker
// declined to produce structure-aware chunks (malformed/ragged CSV, unparseable
// json/yaml/xml) and the file should fall back to prose chunking with the file
// node's provenance set to 'AMBIGUOUS' (D18). It is NOT a crash and NOT a
// per-file failure — the file is still indexed, just as prose.
var ErrDataFallback = errors.New("data_fallback_prose")

const (
	// csvScanCap bounds the bytes the CSV chunker reads from a file (D18 / Phase
	// 5: "bounded scan, first 4 MB — no full-file slurp"). dtype inference and row
	// sampling run over at most this prefix regardless of file size, so a 10 GB
	// CSV never slurps into memory. A file larger than this is marked Truncated;
	// its inferred schema reflects the scanned prefix (honest, observable).
	csvScanCap = 4 * 1024 * 1024 // 4 MB
	// csvExtractorVersion tags on-disk provenance (R7: not in the embedding
	// fingerprint, so a future output change needs an explicit reindex policy).
	csvExtractorVersion = "csv-schema-rows-v1"
	// maxSampleRowChunks caps the csv_rows sample chunks one file may mint
	// (Phase 5 hard cap: "<=8 csv_rows", stratified head/middle/tail).
	maxSampleRowChunks = 8
	// sampleRowsPerChunk is how many CSV rows go into one csv_rows sample chunk.
	sampleRowsPerChunk = 10
	// minDtypeSample is the minimum non-empty values a column needs before a
	// non-string dtype is asserted (avoids "int" from a single coincidental row).
	minDtypeSample = 1
)

// boundedReader wraps an io.Reader and refuses to yield more than limit bytes,
// reporting whether the underlying stream had more (truncation). The CSV reader
// draws from this so a malformed/huge file can never make us allocate unbounded
// memory — the cap is observable via Truncated, not silent.
type boundedReader struct {
	r         io.Reader
	remaining int
	read      int
	truncated bool
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		// Peek one byte to learn whether the source had more (truncation).
		var probe [1]byte
		if n, _ := b.r.Read(probe[:]); n > 0 {
			b.truncated = true
		}
		return 0, io.EOF
	}
	if len(p) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.r.Read(p)
	b.remaining -= n
	b.read += n
	return n, err
}

// ChunkCSV reads the first csvScanCap bytes of a delimited file, sniffs the
// delimiter (, / \t / ;), detects a header, infers per-column dtypes + null
// rates, and emits one csv_schema chunk (D18 header = columns + dtypes + row
// count) plus up to maxSampleRowChunks csv_rows sample chunks (stratified
// head/middle/tail). A malformed/ragged file returns ErrDataFallback so the
// runner prose-chunks it and marks the node provenance AMBIGUOUS. Never panics.
//
// Memory is bounded by boundedReader (scan stops at csvScanCap) and by the
// fixed chunk cap, so file size never drives peak RSS.
func ChunkCSV(path, title string, cfg Config, counter Counter) ([]DataChunk, DataStats, error) {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, DataStats{}, err
	}
	defer func() { _ = f.Close() }()

	br := &boundedReader{r: bufio.NewReader(f), remaining: csvScanCap}
	delim := sniffDelimiter(path)

	cr := csv.NewReader(br)
	cr.Comma = delim
	cr.FieldsPerRecord = -1 // tolerate ragged rows; we validate raggedness ourselves
	cr.ReuseRecord = false
	cr.LazyQuotes = true

	stats := DataStats{ExtractorVersion: csvExtractorVersion}

	first, err := cr.Read()
	if err != nil {
		// Empty or unreadable as CSV → fall back to prose.
		stats.ScannedBytes = br.read
		stats.Truncated = br.truncated
		return nil, stats, fmt.Errorf("csv read header %s: %w", path, ErrDataFallback)
	}
	if len(first) < 1 {
		stats.ScannedBytes = br.read
		stats.Truncated = br.truncated
		return nil, stats, fmt.Errorf("csv no usable header %s: %w", path, ErrDataFallback)
	}
	// Header detection: an all-numeric first row is data, not a header — the file
	// is headerless. Rather than name columns "1"/"2"/"3" (which the comment on
	// looksLikeHeader promised never happens) we synthesize col_0/col_1/… and
	// treat the first row itself as a data row so it is sampled like any other.
	var header []string
	var seedRow []string // a first data row to feed the scan loop (headerless case)
	if looksLikeHeader(first) {
		header = first
	} else {
		header = syntheticHeader(len(first))
		seedRow = first
	}
	ncol := len(header)
	stats.Columns = ncol

	// Single bounded pass: infer dtypes/nulls and capture stratified sample rows.
	cols := make([]*colInfo, ncol)
	for i := range cols {
		cols[i] = &colInfo{}
	}
	var raggedRows, totalRows int
	// Stratified sampling: keep head rows, a reservoir for middle, and a ring for
	// tail, so the sample spans the scanned prefix rather than just the top.
	const headKeep = 30
	var headRows, tailRows [][]string
	tailCap := 30

	observeRow := func(rec []string) {
		totalRows++
		if len(rec) != ncol {
			raggedRows++
		}
		for i := 0; i < ncol && i < len(rec); i++ {
			cols[i].observe(rec[i])
		}
		if len(headRows) < headKeep {
			headRows = append(headRows, rec)
		} else {
			tailRows = append(tailRows, rec)
			if len(tailRows) > tailCap {
				tailRows = tailRows[1:]
			}
		}
	}

	// Headerless (all-numeric first row): the synthesized header consumed no real
	// row, so the first row IS data — observe it before the scan loop.
	if seedRow != nil {
		observeRow(seedRow)
	}

	for {
		rec, rerr := cr.Read()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			// A hard CSV parse error (bad quoting we cannot recover) → fallback.
			stats.ScannedBytes = br.read
			stats.Truncated = br.truncated
			return nil, stats, fmt.Errorf("csv parse %s: %w", path, ErrDataFallback)
		}
		observeRow(rec)
	}
	stats.Rows = totalRows
	stats.ScannedBytes = br.read
	stats.Truncated = br.truncated

	if totalRows == 0 {
		// Header only, no data rows → not useful structured data; fallback.
		return nil, stats, fmt.Errorf("csv header-only %s: %w", path, ErrDataFallback)
	}
	// Raggedness gate (D18): if a meaningful share of rows do not match the
	// header column count, this is not a clean table — fall back to prose and
	// mark the node AMBIGUOUS rather than emit a misleading schema.
	if float64(raggedRows)/float64(totalRows) > 0.10 {
		return nil, stats, fmt.Errorf("csv ragged (%d/%d rows) %s: %w", raggedRows, totalRows, path, ErrDataFallback)
	}

	var out []DataChunk

	// One csv_schema chunk (D18): column list + inferred dtypes + null rates +
	// row count. The header field carries a compact one-line summary (FTS-indexed
	// via chunks_fts(header)); the text carries the full per-column breakdown so
	// embeddings + FTS can match on column names and types.
	schemaText := buildSchemaText(title, header, cols, totalRows, stats.Truncated)
	schemaHeader := buildSchemaHeader(title, header)
	out = append(out, DataChunk{
		Text:       schemaText,
		ByteStart:  0,
		ByteEnd:    len(schemaText),
		TokenCount: counter.Count(schemaText),
		Header:     schemaHeader,
		Kind:       "csv_schema",
	})

	// Up to maxSampleRowChunks csv_rows chunks, stratified head/middle/tail. We
	// have headRows (first headKeep) + tailRows (last tailCap of the rest); build
	// sample groups spanning both so retrieval can match content anywhere in the
	// scanned prefix.
	samples := buildStratifiedSamples(headRows, tailRows)
	for gi, group := range samples {
		if gi >= maxSampleRowChunks {
			break
		}
		txt := renderRowGroup(header, group, delim)
		if strings.TrimSpace(txt) == "" {
			continue
		}
		out = append(out, DataChunk{
			Text:       txt,
			ByteStart:  0,
			ByteEnd:    len(txt),
			TokenCount: counter.Count(txt),
			Header:     fmt.Sprintf("sample rows %d — %s", gi+1, title),
			Kind:       "csv_rows",
		})
	}

	return out, stats, nil
}

// sniffDelimiter picks the field delimiter from the extension: .tsv → tab,
// everything else → comma. (No first-line semicolon sniff is implemented;
// European semicolon-delimited .csv files are read as single-column comma data
// and, being effectively ragged/uninformative, generally fall back to prose.)
func sniffDelimiter(path string) rune {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tsv"):
		return '\t'
	default:
		return ','
	}
}

// colInfo accumulates dtype evidence and null counts for one CSV column over the
// bounded scan.
type colInfo struct {
	total   int
	nulls   int
	intOK   int
	floatOK int
	dateOK  int
	nonNull int
}

func (c *colInfo) observe(v string) {
	c.total++
	t := strings.TrimSpace(v)
	if t == "" {
		c.nulls++
		return
	}
	c.nonNull++
	if _, err := strconv.ParseInt(t, 10, 64); err == nil {
		c.intOK++
	}
	if _, err := strconv.ParseFloat(t, 64); err == nil {
		c.floatOK++
	}
	if isDate(t) {
		c.dateOK++
	}
}

// dtype infers the column's dominant type from accumulated evidence. A column is
// typed only if ALL non-null values match (a single mismatch demotes to string),
// which keeps inference conservative and explainable.
func (c *colInfo) dtype() string {
	if c.nonNull < minDtypeSample {
		return "string"
	}
	switch {
	case c.intOK == c.nonNull:
		return "int"
	case c.floatOK == c.nonNull:
		return "float"
	case c.dateOK == c.nonNull:
		return "date"
	default:
		return "string"
	}
}

func (c *colInfo) nullRate() float64 {
	if c.total == 0 {
		return 0
	}
	return float64(c.nulls) / float64(c.total)
}

// isDate recognizes a few common unambiguous date layouts. Deliberately narrow:
// a false negative just demotes the column to string, which is safe.
func isDate(s string) bool {
	for _, layout := range []string{"2006-01-02", "2006/01/02", "01/02/2006", "2006-01-02T15:04:05Z07:00"} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// looksLikeHeader reports whether the candidate first row is a real header rather
// than a data row. It requires at least one non-empty cell AND rejects a row whose
// cells are ALL numeric (int/float) — an all-numeric first row is almost certainly
// data, meaning the file is headerless. In that case the caller synthesizes
// col_0/col_1/… names (see syntheticHeader) and treats the first row as data, so a
// headerless numeric CSV never produces columns literally named "1"/"2"/"3".
func looksLikeHeader(row []string) bool {
	anyNonEmpty := false
	allNumeric := true
	for _, c := range row {
		t := strings.TrimSpace(c)
		if t == "" {
			continue
		}
		anyNonEmpty = true
		if !isNumeric(t) {
			allNumeric = false
		}
	}
	if !anyNonEmpty {
		return false
	}
	// Every non-empty cell parsed as a number → data row, not a header.
	return !allNumeric
}

// isNumeric reports whether s parses as an int or float (the numeric-header test
// in looksLikeHeader). A column header like "id" or "2024_total" is not numeric;
// a bare "1" or "9.99" is.
func isNumeric(s string) bool {
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	return false
}

// syntheticHeader builds col_0..col_{n-1} names for a headerless CSV (all-numeric
// first row). The headerless table is still indexed (csv_schema/csv_rows) with
// these synthetic names rather than falling back to prose — headerless numeric
// CSVs are common and useful to retrieve.
func syntheticHeader(n int) []string {
	h := make([]string, n)
	for i := range h {
		h[i] = fmt.Sprintf("col_%d", i)
	}
	return h
}

func buildSchemaHeader(title string, header []string) string {
	cols := make([]string, len(header))
	for i, h := range header {
		cols[i] = strings.TrimSpace(h)
	}
	return fmt.Sprintf("columns: %s — %s", strings.Join(cols, ", "), title)
}

func buildSchemaText(title string, header []string, cols []*colInfo, rowCount int, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CSV schema for %s\n", title)
	fmt.Fprintf(&b, "rows scanned: %d", rowCount)
	if truncated {
		b.WriteString(" (scan truncated at 4MB — count reflects scanned prefix)")
	}
	b.WriteString("\ncolumns:\n")
	for i, h := range header {
		name := strings.TrimSpace(h)
		if name == "" {
			name = fmt.Sprintf("col%d", i+1)
		}
		ci := cols[i]
		fmt.Fprintf(&b, "  - %s: %s (null_rate=%.2f)\n", name, ci.dtype(), ci.nullRate())
	}
	return b.String()
}

// buildStratifiedSamples interleaves head and tail rows into groups of
// sampleRowsPerChunk so each csv_rows chunk spans a slice of the table.
func buildStratifiedSamples(head, tail [][]string) [][][]string {
	all := make([][]string, 0, len(head)+len(tail))
	all = append(all, head...)
	all = append(all, tail...)
	var groups [][][]string
	for i := 0; i < len(all); i += sampleRowsPerChunk {
		end := i + sampleRowsPerChunk
		if end > len(all) {
			end = len(all)
		}
		groups = append(groups, all[i:end])
	}
	return groups
}

// renderRowGroup renders a group of rows as a small delimited block prefixed by
// the header line, so the embedded text carries both column names and values.
func renderRowGroup(header []string, rows [][]string, delim rune) string {
	var b strings.Builder
	sep := string(delim)
	b.WriteString(strings.Join(header, sep))
	b.WriteByte('\n')
	for _, r := range rows {
		b.WriteString(strings.Join(r, sep))
		b.WriteByte('\n')
	}
	return b.String()
}
