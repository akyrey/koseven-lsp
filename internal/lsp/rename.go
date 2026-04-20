package lsp

import (
	"os"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// PrepareRename handles textDocument/prepareRename.
// Returns the range of the view name string literal under the cursor when
// the cursor is in a rename-able position (a view name string in
// View::factory, new View, or Kohana::find_file('views', ...)).
// Returns nil when rename is not possible at this position.
func (s *Server) PrepareRename(_ *glsp.Context, p *protocol.PrepareRenameParams) (any, error) {
	src, err := s.docs.Read(p.TextDocument.URI)
	if err != nil {
		return nil, nil
	}
	path := URIToPath(p.TextDocument.URI)
	offset := positionToByteOffset(src, p.Position)

	_, loc, found := findViewNameLocAtOffset(src, path, offset)
	if !found {
		return nil, nil
	}
	r := toLSPRange(loc, src)
	return &r, nil
}

// Rename handles textDocument/rename.
// Builds a WorkspaceEdit that replaces every view name string literal matching
// the old name (across all files in the usage index) with a quoted version of
// the new name. The physical view file is NOT renamed; that is left to the
// developer or a separate willRenameFiles handler.
func (s *Server) Rename(_ *glsp.Context, p *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
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

	oldName, _, found := findViewNameLocAtOffset(src, path, offset)
	if !found {
		return nil, nil
	}

	newName := p.NewName
	if newName == "" || newName == oldName {
		return nil, nil
	}

	// Collect affected files from the usage index.
	usages := idx.UsagesOf(oldName)
	if len(usages) == 0 {
		return nil, nil
	}

	// Build per-file TextEdits by re-parsing each source and finding all
	// matching view name string literals.
	changes := make(map[protocol.DocumentUri][]protocol.TextEdit)
	seen := make(map[string]bool)

	for _, u := range usages {
		if seen[u.File] {
			continue
		}
		seen[u.File] = true

		fileSrc, _ := s.docs.Read(PathToURI(u.File))
		if fileSrc == nil {
			fileSrc, _ = os.ReadFile(u.File)
		}
		if fileSrc == nil {
			continue
		}

		edits := collectViewRenameEdits(fileSrc, u.File, oldName, newName)
		if len(edits) > 0 {
			uri := PathToURI(u.File)
			changes[uri] = append(changes[uri], edits...)
		}
	}

	if len(changes) == 0 {
		return nil, nil
	}
	return &protocol.WorkspaceEdit{Changes: changes}, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// findViewNameLocAtOffset is like findViewNameAtOffset but also returns the
// phputil.Location of the matched string literal (for precise range computation
// using toLSPRange with source bytes).
func findViewNameLocAtOffset(src []byte, path string, offset int) (viewName string, loc phputil.Location, found bool) {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return
	}
	fv := &viewNameLocFinder{path: path, offset: offset}
	traverser.NewTraverser(fv).Traverse(root)
	if fv.viewName == "" {
		return
	}
	return fv.viewName, fv.nameLoc, true
}

// viewNameLocFinder is viewNameFinder extended to also capture the VKCOM
// position of the matched string literal.
type viewNameLocFinder struct {
	visitor.Null
	path     string
	offset   int
	viewName string
	nameLoc  phputil.Location
}

func (v *viewNameLocFinder) ExprStaticCall(n *ast.ExprStaticCall) {
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
		if len(n.Args) > 0 {
			v.matchArg(phputil.ArgExpr(n.Args[0]))
		}
	case (className == "Kohana" || className == "\\Kohana") && method == "find_file":
		if len(n.Args) < 2 {
			return
		}
		kind, ok := phputil.ScalarStringVal(phputil.ArgExpr(n.Args[0]))
		if !ok || kind != "views" {
			return
		}
		v.matchArg(phputil.ArgExpr(n.Args[1]))
	}
}

func (v *viewNameLocFinder) ExprNew(n *ast.ExprNew) {
	if v.viewName != "" {
		return
	}
	cn := phputil.NameToString(n.Class)
	if cn != "View" && cn != "\\View" {
		return
	}
	if len(n.Args) > 0 {
		v.matchArg(phputil.ArgExpr(n.Args[0]))
	}
}

func (v *viewNameLocFinder) matchArg(expr ast.Vertex) {
	if expr == nil {
		return
	}
	pos := expr.GetPosition()
	if pos == nil || v.offset < pos.StartPos || v.offset >= pos.EndPos {
		return
	}
	name, ok := phputil.ScalarStringVal(expr)
	if !ok {
		return
	}
	v.viewName = name
	v.nameLoc = phputil.FromPosition(v.path, pos)
}

// collectViewRenameEdits returns TextEdits that replace every view name string
// literal matching oldName (in view-construction position) with a new quoted
// string containing newName.
func collectViewRenameEdits(src []byte, path, oldName, newName string) []protocol.TextEdit {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return nil
	}
	rv := &renameVisitor{path: path, src: src, oldName: oldName, newName: newName}
	traverser.NewTraverser(rv).Traverse(root)
	return rv.edits
}

type renameVisitor struct {
	visitor.Null
	path    string
	src     []byte
	oldName string
	newName string
	edits   []protocol.TextEdit
}

func (v *renameVisitor) ExprStaticCall(n *ast.ExprStaticCall) {
	className := phputil.NameToString(n.Class)
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok {
		return
	}
	method := string(methodID.Value)

	switch {
	case (className == "View" || className == "\\View") && method == "factory":
		if len(n.Args) > 0 {
			v.addEditIfMatch(phputil.ArgExpr(n.Args[0]))
		}
	case (className == "Kohana" || className == "\\Kohana") && method == "find_file":
		if len(n.Args) < 2 {
			return
		}
		kind, ok := phputil.ScalarStringVal(phputil.ArgExpr(n.Args[0]))
		if !ok || kind != "views" {
			return
		}
		v.addEditIfMatch(phputil.ArgExpr(n.Args[1]))
	}
}

func (v *renameVisitor) ExprNew(n *ast.ExprNew) {
	cn := phputil.NameToString(n.Class)
	if cn != "View" && cn != "\\View" {
		return
	}
	if len(n.Args) > 0 {
		v.addEditIfMatch(phputil.ArgExpr(n.Args[0]))
	}
}

// addEditIfMatch checks whether expr is a string literal matching oldName and,
// if so, appends a TextEdit that replaces the entire literal (including quotes)
// with a single-quoted version of newName.
func (v *renameVisitor) addEditIfMatch(expr ast.Vertex) {
	if expr == nil {
		return
	}
	name, ok := phputil.ScalarStringVal(expr)
	if !ok || name != v.oldName {
		return
	}
	loc := phputil.FromPosition(v.path, expr.GetPosition())
	if loc.Zero() {
		return
	}
	// Preserve the original quote character (usually single quote).
	s, ok := expr.(*ast.ScalarString)
	if !ok {
		return
	}
	quoteChar := byte('\'')
	if len(s.Value) > 0 && (s.Value[0] == '\'' || s.Value[0] == '"') {
		quoteChar = s.Value[0]
	}
	newText := string(quoteChar) + v.newName + string(quoteChar)
	v.edits = append(v.edits, protocol.TextEdit{
		Range:   toLSPRange(loc, v.src),
		NewText: newText,
	})
}
