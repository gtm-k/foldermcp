//go:build cgo

package chunker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp writes content to a temp file under t.TempDir and returns the path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// TestChunkCSVSchemaAndRows: a well-formed CSV produces exactly one csv_schema
// chunk (header = column list + dtypes + row count, D18) and up to 8 csv_rows
// sample chunks (stratified head/middle/tail).
func TestChunkCSVSchema(t *testing.T) {
	var b strings.Builder
	b.WriteString("id,name,price,active,created\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "%d,item-%d,%.2f,%t,2024-01-%02d\n", i, i, float64(i)*1.5, i%2 == 0, (i%28)+1)
	}
	path := writeTemp(t, "data.csv", b.String())

	chunks, stats, err := ChunkCSV(path, "data.csv", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkCSV: %v", err)
	}
	var schema, rows int
	for _, c := range chunks {
		switch c.Kind {
		case "csv_schema":
			schema++
			// Header must contain column names and the row count.
			for _, col := range []string{"id", "name", "price", "active", "created"} {
				if !strings.Contains(c.Header, col) && !strings.Contains(c.Text, col) {
					t.Errorf("csv_schema missing column %q in header/text: %q / %q", col, c.Header, c.Text)
				}
			}
		case "csv_rows":
			rows++
		default:
			t.Errorf("unexpected chunk kind %q", c.Kind)
		}
	}
	if schema != 1 {
		t.Errorf("csv_schema chunks = %d, want 1", schema)
	}
	if rows < 1 || rows > 8 {
		t.Errorf("csv_rows chunks = %d, want 1..8", rows)
	}
	if stats.Rows == 0 {
		t.Errorf("stats.Rows = 0, want >0")
	}
	// Re-slicing each chunk's own text by [ByteStart:ByteEnd] reproduces Text.
	for _, c := range chunks {
		if c.ByteStart != 0 || c.ByteEnd != len(c.Text) {
			t.Errorf("chunk byte span (%d,%d) not self-relative for kind %s", c.ByteStart, c.ByteEnd, c.Kind)
		}
	}
}

// TestChunkCSVBoundedScanOnLargeFile: a CSV larger than the 4 MB scan cap must
// still produce a bounded number of chunks (1 schema + <=8 rows) and must not
// slurp the whole file (proven by the bounded-reader: scan stops at the cap, so
// the inferred row count reflects only the scanned prefix, never the full file).
func TestChunkCSVBoundedScanOnLargeFile(t *testing.T) {
	var b strings.Builder
	b.WriteString("a,b,c\n")
	// ~ > 4 MB: each row ~ 30 bytes; 200k rows ~ 6 MB.
	for i := 0; i < 200000; i++ {
		fmt.Fprintf(&b, "%d,value-%d,2024-06-25\n", i, i)
	}
	full := b.String()
	if len(full) <= csvScanCap {
		t.Fatalf("fixture is only %d bytes, need > scan cap %d to exercise bounding", len(full), csvScanCap)
	}
	path := writeTemp(t, "big.csv", full)

	chunks, stats, err := ChunkCSV(path, "big.csv", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkCSV: %v", err)
	}
	var schema, rows int
	for _, c := range chunks {
		switch c.Kind {
		case "csv_schema":
			schema++
		case "csv_rows":
			rows++
		}
	}
	if schema != 1 {
		t.Errorf("csv_schema = %d, want 1", schema)
	}
	if rows > 8 {
		t.Errorf("csv_rows = %d, want <=8 (bounded regardless of size)", rows)
	}
	// Bounded-scan proof: stats.Scanned must be <= scan cap even though the file
	// is larger (no full-file slurp).
	if stats.ScannedBytes > csvScanCap {
		t.Errorf("ScannedBytes = %d, exceeds scan cap %d — full-file slurp suspected", stats.ScannedBytes, csvScanCap)
	}
	if stats.Truncated != true {
		t.Errorf("Truncated = false on a >4MB file, want true (scan should have been bounded)")
	}
}

// TestChunkCSVMalformedFallsBack: a ragged CSV (inconsistent column counts)
// returns ErrDataFallback so the runner prose-chunks it and marks the node
// provenance AMBIGUOUS (D18). Never crashes.
func TestChunkCSVMalformedFallsBack(t *testing.T) {
	// Ragged: header has 3 columns, rows vary wildly.
	ragged := "a,b,c\n1,2\n3,4,5,6,7,8\nx\n,,\n9,10,11,12\n"
	path := writeTemp(t, "ragged.csv", ragged)

	_, _, err := ChunkCSV(path, "ragged.csv", DefaultConfig(), wordCounter{})
	if !errors.Is(err, ErrDataFallback) {
		t.Fatalf("ragged CSV: err = %v, want ErrDataFallback", err)
	}
}

// TestChunkCSVTSV: tab-delimited files are sniffed and chunked.
func TestChunkCSVTSV(t *testing.T) {
	tsv := "name\tage\tcity\nalice\t30\tNYC\nbob\t25\tLA\ncarol\t40\tSF\n"
	path := writeTemp(t, "people.tsv", tsv)
	chunks, _, err := ChunkCSV(path, "people.tsv", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkCSV tsv: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks from TSV")
	}
	foundSchema := false
	for _, c := range chunks {
		if c.Kind == "csv_schema" {
			foundSchema = true
			if !strings.Contains(c.Text, "age") {
				t.Errorf("tsv schema text missing column 'age': %q", c.Text)
			}
		}
	}
	if !foundSchema {
		t.Error("no csv_schema chunk from TSV")
	}
}

// TestChunkCSVAllNumericFirstRowHeaderless (FIX 2): a CSV whose first row is
// all-numeric is HEADERLESS — looksLikeHeader must reject it so the file is not
// given columns literally named "1"/"2"/"3". The chunker synthesizes col_0/col_1/…
// names instead, still emits csv_schema/csv_rows (headerless numeric CSVs are
// useful to index), and treats the first row as data.
func TestChunkCSVAllNumericFirstRowHeaderless(t *testing.T) {
	// All-numeric first row → headerless. No row is a string header.
	csvData := "1,2,3\n4,5,6\n7,8,9\n10,11,12\n"
	path := writeTemp(t, "numeric.csv", csvData)
	chunks, stats, err := ChunkCSV(path, "numeric.csv", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkCSV headerless-numeric: %v", err)
	}
	var schemaText, schemaHeader string
	var schema, rows int
	for _, c := range chunks {
		switch c.Kind {
		case "csv_schema":
			schema++
			schemaText = c.Text
			schemaHeader = c.Header
		case "csv_rows":
			rows++
		}
	}
	if schema != 1 {
		t.Errorf("csv_schema = %d, want 1 (headerless still emits a schema)", schema)
	}
	if rows == 0 {
		t.Error("no csv_rows from headerless numeric CSV")
	}
	// The column names must be synthetic, NEVER the literal numbers "1"/"2"/"3".
	for _, bad := range []string{"- 1:", "- 2:", "- 3:"} {
		if strings.Contains(schemaText, bad) {
			t.Errorf("headerless CSV produced a column literally named from data (%q) in schema: %q", bad, schemaText)
		}
	}
	for _, want := range []string{"col_0", "col_1", "col_2"} {
		if !strings.Contains(schemaText, want) && !strings.Contains(schemaHeader, want) {
			t.Errorf("synthetic column %q missing from schema header/text: %q / %q", want, schemaHeader, schemaText)
		}
	}
	// The first numeric row is treated as DATA: 4 rows total (none consumed as a
	// header), so the inferred row count is 4, not 3.
	if stats.Rows != 4 {
		t.Errorf("stats.Rows = %d, want 4 (first row counted as data, not header)", stats.Rows)
	}
}

// TestChunkCSVDtypeInference: dtypes are inferred per column.
func TestChunkCSVDtypeInference(t *testing.T) {
	csv := "n,f,d,s\n1,1.5,2024-01-01,hello\n2,2.5,2024-01-02,world\n3,3.5,2024-01-03,foo\n"
	path := writeTemp(t, "typed.csv", csv)
	chunks, _, err := ChunkCSV(path, "typed.csv", DefaultConfig(), wordCounter{})
	if err != nil {
		t.Fatalf("ChunkCSV: %v", err)
	}
	var schemaText string
	for _, c := range chunks {
		if c.Kind == "csv_schema" {
			schemaText = c.Text
		}
	}
	for _, want := range []string{"int", "float", "date", "string"} {
		if !strings.Contains(schemaText, want) {
			t.Errorf("schema text missing dtype %q: %q", want, schemaText)
		}
	}
}
