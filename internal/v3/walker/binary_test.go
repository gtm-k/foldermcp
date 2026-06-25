package walker

import "testing"

// IsBinaryContent uses git's heuristic: a NUL byte anywhere in the sampled
// prefix means the file is not indexable UTF-8 text. Real source/prose never
// contains a raw 0x00; only binary blobs and NUL-interleaved encodings (e.g.
// BOM-less UTF-16) do, and M1 cannot index those as text regardless.
func TestIsBinaryContent(t *testing.T) {
	cases := []struct {
		name   string
		sample []byte
		want   bool
	}{
		{"plain ascii", []byte("hello world\nsecond line"), false},
		{"utf8 multibyte", []byte("café résumé — naïve"), false},
		{"empty", []byte{}, false},
		{"nil", nil, false},
		{"nul at start", []byte{0x00, 'a', 'b'}, true},
		{"nul in middle", []byte("readme intro line\n\x00\x01\x02"), true},
		{"nul at end", append([]byte("trailing"), 0x00), true},
		{"high bytes no nul (latin-1) is not flagged", []byte{0xe9, 0xe8, 'a', 'b'}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsBinaryContent(c.sample); got != c.want {
				t.Errorf("IsBinaryContent(%q) = %v, want %v", c.sample, got, c.want)
			}
		})
	}
}

// IsBinaryDocumentExt is the single source of truth for the binary-document
// extension set the M1 pipeline cannot extract text from. The runner consumes
// this instead of re-deriving the list (no drift between walker and pipeline).
func TestIsBinaryDocumentExt(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"report.pdf", true},
		{"notes.docx", true},
		{"sheet.odt", true},
		{"REPORT.PDF", true}, // case-insensitive
		{"a/b/c.DocX", true},
		{"readme.md", false},
		{"main.go", false},
		{"data.txt", false},
		{"no_extension", false},
		{"archive.pdf.txt", false}, // only the final extension counts
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := IsBinaryDocumentExt(c.path); got != c.want {
				t.Errorf("IsBinaryDocumentExt(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}
