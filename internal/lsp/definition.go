package lsp

import (
	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// Definition handles textDocument/definition.
// When the cursor is on a view name string literal inside View::factory,
// new View, or Kohana::find_file('views', ...), it returns the resolved view
// file(s) in cascade order. Returns all candidates (LSP allows []Location) so
// HMVC overrides are visible.
func (s *Server) Definition(_ *glsp.Context, p *protocol.DefinitionParams) (any, error) {
	idx := s.viewIndex()
	if idx == nil {
		return nil, nil
	}

	src, err := s.docs.Read(p.TextDocument.URI)
	if err != nil {
		return nil, nil
	}

	path := URIToPath(p.TextDocument.URI)
	offset := positionToByteOffset(src, p.Position)

	viewName := findViewNameAtOffset(src, path, offset)
	if viewName == "" {
		return nil, nil
	}

	defs := idx.Resolve(viewName)
	if len(defs) == 0 {
		return nil, nil
	}

	locs := make([]protocol.Location, 0, len(defs))
	for _, d := range defs {
		locs = append(locs, protocol.Location{
			URI:   PathToURI(d.Path),
			Range: protocol.Range{}, // jump to top of file
		})
	}
	return locs, nil
}

// findViewNameAtOffset parses src and returns the logical view name of the
// string literal that contains offset, if that literal is in view-construction
// position. Returns "" when the cursor is not on a recognised view name.
func findViewNameAtOffset(src []byte, path string, offset int) string {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return ""
	}
	fv := &viewNameFinder{offset: offset}
	traverser.NewTraverser(fv).Traverse(root)
	return fv.viewName
}

// viewNameFinder is a single-pass AST visitor that checks whether the cursor
// position falls within a view name string literal.
type viewNameFinder struct {
	visitor.Null
	offset   int
	viewName string
}

func (v *viewNameFinder) ExprStaticCall(n *ast.ExprStaticCall) {
	if v.viewName != "" {
		return
	}
	className := phputil.NameToString(n.Class)
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok {
		return
	}
	method := string(methodID.Value)

	switch {
	case (className == "View" || className == "\\View") && method == "factory":
		// View::factory('name', ...)
		if len(n.Args) > 0 {
			if expr := phputil.ArgExpr(n.Args[0]); expr != nil {
				if v.cursorIn(expr) {
					v.viewName, _ = phputil.ScalarStringVal(expr)
				}
			}
		}

	case (className == "Kohana" || className == "\\Kohana") && method == "find_file":
		// Kohana::find_file('views', 'name')
		if len(n.Args) < 2 {
			return
		}
		kind, ok := phputil.ScalarStringVal(phputil.ArgExpr(n.Args[0]))
		if !ok || kind != "views" {
			return
		}
		expr := phputil.ArgExpr(n.Args[1])
		if expr != nil && v.cursorIn(expr) {
			v.viewName, _ = phputil.ScalarStringVal(expr)
		}
	}
}

func (v *viewNameFinder) ExprNew(n *ast.ExprNew) {
	if v.viewName != "" {
		return
	}
	className := phputil.NameToString(n.Class)
	if className != "View" && className != "\\View" {
		return
	}
	if len(n.Args) > 0 {
		if expr := phputil.ArgExpr(n.Args[0]); expr != nil && v.cursorIn(expr) {
			v.viewName, _ = phputil.ScalarStringVal(expr)
		}
	}
}

func (v *viewNameFinder) cursorIn(node ast.Vertex) bool {
	if node == nil {
		return false
	}
	pos := node.GetPosition()
	if pos == nil {
		return false
	}
	return v.offset >= pos.StartPos && v.offset < pos.EndPos
}
