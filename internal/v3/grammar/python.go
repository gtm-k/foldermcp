//go:build cgo

package grammar

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// PythonExtractor extracts Python symbols (functions, methods, classes)
// using the tree-sitter Python grammar.
type PythonExtractor struct{ parser *sitter.Parser }

func NewPythonExtractor() (*PythonExtractor, error) {
	p := sitter.NewParser()
	lang := sitter.NewLanguage(tspy.Language())
	if err := p.SetLanguage(lang); err != nil {
		return nil, err
	}
	return &PythonExtractor{parser: p}, nil
}

func (e *PythonExtractor) Language() string { return "python" }

func (e *PythonExtractor) Extract(source []byte) ([]Symbol, error) {
	tree := e.parser.Parse(source, nil)
	defer tree.Close()
	var out []Symbol

	var walk func(n *sitter.Node, inClass bool)
	walk = func(n *sitter.Node, inClass bool) {
		switch n.Kind() {
		case "function_definition":
			name := childText(n, "name", source)
			kind := "function"
			if inClass {
				kind = "method"
			}
			out = append(out, mkSymbol(kind, name, n, source))
		case "class_definition":
			name := childText(n, "name", source)
			out = append(out, mkSymbol("class", name, n, source))
			body := n.ChildByFieldName("body")
			if body != nil {
				for i := uint(0); i < body.ChildCount(); i++ {
					walk(body.Child(i), true)
				}
			}
			return
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i), inClass)
		}
	}
	walk(tree.RootNode(), false)
	return out, nil
}
