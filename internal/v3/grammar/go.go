//go:build cgo

package grammar

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// GoExtractor extracts Go symbols (functions, methods, structs, interfaces,
// type declarations) using the tree-sitter Go grammar.
type GoExtractor struct {
	parser *sitter.Parser
}

func NewGoExtractor() (*GoExtractor, error) {
	p := sitter.NewParser()
	lang := sitter.NewLanguage(tsgo.Language())
	if err := p.SetLanguage(lang); err != nil {
		return nil, err
	}
	return &GoExtractor{parser: p}, nil
}

func (e *GoExtractor) Language() string { return "go" }

func (e *GoExtractor) Extract(source []byte) ([]Symbol, error) {
	tree := e.parser.Parse(source, nil)
	defer tree.Close()

	root := tree.RootNode()
	var out []Symbol

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		kind := n.Kind()
		switch kind {
		case "function_declaration":
			name := childText(n, "name", source)
			out = append(out, mkSymbol("function", name, n, source))
		case "method_declaration":
			name := childText(n, "name", source)
			out = append(out, mkSymbol("method", name, n, source))
		case "type_declaration":
			for i := uint(0); i < n.ChildCount(); i++ {
				c := n.Child(i)
				if c.Kind() == "type_spec" {
					name := childText(c, "name", source)
					inner := c.ChildByFieldName("type")
					symKind := "type"
					if inner != nil {
						switch inner.Kind() {
						case "struct_type":
							symKind = "struct"
						case "interface_type":
							symKind = "interface"
						}
					}
					out = append(out, mkSymbol(symKind, name, c, source))
				}
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out, nil
}

func mkSymbol(kind, name string, n *sitter.Node, source []byte) Symbol {
	start, end := n.StartByte(), n.EndByte()
	sig := string(source[start:end])
	if len(sig) > 256 {
		sig = sig[:256]
	}
	return Symbol{
		Kind:      kind,
		Name:      name,
		ByteStart: start,
		ByteEnd:   end,
		LineStart: int(n.StartPosition().Row) + 1,
		LineEnd:   int(n.EndPosition().Row) + 1,
		Signature: sig,
	}
}

func childText(n *sitter.Node, field string, source []byte) string {
	c := n.ChildByFieldName(field)
	if c == nil {
		return ""
	}
	return string(source[c.StartByte():c.EndByte()])
}
