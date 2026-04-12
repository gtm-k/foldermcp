//go:build cgo

package grammar

// Symbol represents a named code construct extracted from source by a
// tree-sitter grammar. The byte offsets and line numbers refer to the
// original source slice.
type Symbol struct {
	Kind      string // "function" | "method" | "class" | "struct" | "interface" | "const" | "var" | "type"
	Name      string
	ByteStart uint
	ByteEnd   uint
	LineStart int
	LineEnd   int
	Signature string // raw source slice of the node header (truncated to 256 bytes)
}

// Extractor extracts top-level symbols from source code in a specific language.
type Extractor interface {
	Language() string
	Extract(source []byte) ([]Symbol, error)
}
