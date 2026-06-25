// This file is intentionally NOT tagged //go:build cgo (unlike the rest of the
// walker package): IsBinaryContent/IsBinaryDocumentExt are pure Go with no cgo
// dependency, so leaving them untagged lets the no-cgo CI matrix exercise them
// without a C toolchain and keeps the predicates reusable from non-cgo callers.
package walker

import (
	"bytes"
	"path/filepath"
	"strings"
)

// IsBinaryContent reports whether sample looks like binary (non-indexable)
// content rather than UTF-8 text. It uses git's heuristic — a NUL byte (0x00)
// anywhere in the sampled prefix — which is the single most reliable, lowest
// false-positive binary signal:
//
//   - Real source code and prose never contain a raw NUL byte. (A C source with
//     the escape sequence "\0" stores the bytes '\\','0', not 0x00.)
//   - Binary containers (images, archives, parquet, ELF, sqlite, …) reliably
//     carry NULs early — even when their magic bytes happen to sniff as text/*
//     under http.DetectContentType, which only inspects the first 512 bytes.
//   - UTF-16/UTF-32 text is caught WHEN it carries a NUL — true for any content
//     with ASCII characters (whose high byte is 0x00) or a UTF-16/32 BOM, i.e.
//     the common cases. Fully non-ASCII UTF-16 with no NULs is NOT caught here
//     and would still chunk as mojibake; real transcoding is M2 scope. M1 has no
//     transcoder, so skipping the caught cases is the honest outcome.
//
// Caller passes a bounded prefix (the walker samples binarySniffBytes); the
// pipeline also re-checks the full file body as defense in depth.
func IsBinaryContent(sample []byte) bool {
	return bytes.IndexByte(sample, 0x00) >= 0
}

// binaryDocumentExts is the single source of truth for document-class container
// formats whose raw bytes the M1 pipeline cannot extract text from. Both the
// walker classifier and the pipeline runner consult IsBinaryDocumentExt so the
// set never drifts between the two (M2 Phase 4 replaces this with real PDF/Office
// text extraction).
var binaryDocumentExts = map[string]struct{}{
	".pdf":  {},
	".docx": {},
	".odt":  {},
}

// IsBinaryDocumentExt reports whether path's final extension names a
// binary-document container the M1 pipeline cannot extract text from.
func IsBinaryDocumentExt(path string) bool {
	_, ok := binaryDocumentExts[strings.ToLower(filepath.Ext(path))]
	return ok
}

// structuredDataExts is the single source of truth (A5/R3) for structure-aware
// TEXT data formats the runner routes to the csv/data chunker instead of
// skipping. These keep content_class 'data' (no new class value — the 0001
// files.content_class CHECK is not widened and A5 adds no migration), so the
// walker classifier and the runner must agree on the set via this one predicate.
// .sqlite is deliberately EXCLUDED: it is a binary DB container, not a text
// format, so it stays in the skipped 'data' class.
var structuredDataExts = map[string]struct{}{
	".csv":  {},
	".tsv":  {},
	".json": {},
	".yaml": {},
	".yml":  {},
	".xml":  {},
}

// IsStructuredDataExt reports whether path's final extension names a
// structure-aware text data format (csv/tsv/json/yaml/yml/xml) that the runner
// indexes via the csv/data chunker. Pure-Go (untagged) so the no-cgo CI matrix
// and non-cgo callers can use it, mirroring IsBinaryDocumentExt.
func IsStructuredDataExt(path string) bool {
	_, ok := structuredDataExts[strings.ToLower(filepath.Ext(path))]
	return ok
}
