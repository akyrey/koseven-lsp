package lsp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/phputil"
	"github.com/akyrey/koseven-lsp/internal/project"
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

	// Build documentChanges: TextDocumentEdit per file + RenameFile per view
	// definition. Clients that support documentChanges use this; others fall
	// back to the changes map (text edits only).
	docChanges := textEditsToDocChanges(changes)
	for _, def := range idx.Resolve(oldName) {
		newPath := viewFileNewPath(def.Path, oldName, newName)
		if newPath == "" {
			continue
		}
		docChanges = append(docChanges, protocol.RenameFile{
			Kind:   "rename",
			OldURI: string(PathToURI(def.Path)),
			NewURI: string(PathToURI(newPath)),
		})
	}

	return &protocol.WorkspaceEdit{
		Changes:         changes,
		DocumentChanges: docChanges,
	}, nil
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

// ─── workspace/willRenameFiles ────────────────────────────────────────────────

// WillRenameFiles handles workspace/willRenameFiles.
// When the user renames a view file in their file manager (nvim-tree, etc.),
// the editor sends this notification before performing the rename. We return
// a WorkspaceEdit that updates all View::factory / new View /
// Kohana::find_file('views',...) string literals referencing the old view name.
// The physical file rename itself is handled by the editor — we only update
// string references.
func (s *Server) WillRenameFiles(_ *glsp.Context, p *protocol.RenameFilesParams) (*protocol.WorkspaceEdit, error) {
	idx := s.viewIndex()
	if idx == nil {
		return nil, nil
	}
	root, cfg, modules := s.cascadeState()
	if root == "" {
		return nil, nil
	}
	viewRoots := project.BuildViewRoots(root, cfg, modules)

	changes := make(map[protocol.DocumentUri][]protocol.TextEdit)

	for _, fr := range p.Files {
		oldPath := URIToPath(protocol.DocumentUri(fr.OldURI))
		newPath := URIToPath(protocol.DocumentUri(fr.NewURI))

		oldNames := idx.NamesForFile(oldPath)
		if len(oldNames) == 0 {
			continue // not a tracked view file
		}

		newName := viewNameFromPath(newPath, viewRoots)
		if newName == "" {
			continue // can't determine target view name — skip safely
		}

		for _, oldName := range oldNames {
			if oldName == newName {
				continue
			}
			seen := make(map[string]bool)
			for _, u := range idx.UsagesOf(oldName) {
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
		}
	}

	if len(changes) == 0 {
		return nil, nil
	}
	return &protocol.WorkspaceEdit{Changes: changes}, nil
}

// viewNameFromPath returns the logical view name for an absolute file path by
// finding which view root it lives under and computing the path relative to
// that root (with forward slashes, no .php extension).
// Returns "" when the path is not under any known view root.
func viewNameFromPath(filePath string, viewRoots []project.ViewRoot) string {
	for _, vr := range viewRoots {
		rel, err := filepath.Rel(vr.Path, filePath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		return filepath.ToSlash(strings.TrimSuffix(rel, ".php"))
	}
	return ""
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

// ─── File rename helpers ──────────────────────────────────────────────────────

// viewFileNewPath computes the new on-disk path for a view definition file
// when the view is renamed from oldName to newName. Returns "" when defPath
// does not end with the expected suffix (safe no-op for unexpected index entries).
func viewFileNewPath(defPath, oldName, newName string) string {
	oldSuffix := string(filepath.Separator) + filepath.FromSlash(oldName) + ".php"
	if !strings.HasSuffix(defPath, oldSuffix) {
		return ""
	}
	return strings.TrimSuffix(defPath, oldSuffix) +
		string(filepath.Separator) + filepath.FromSlash(newName) + ".php"
}

// textEditsToDocChanges converts a URI→[]TextEdit changes map into
// []TextDocumentEdit values for WorkspaceEdit.DocumentChanges.
func textEditsToDocChanges(changes map[protocol.DocumentUri][]protocol.TextEdit) []any {
	out := make([]any, 0, len(changes))
	for uri, edits := range changes {
		editsAny := make([]any, len(edits))
		for i, e := range edits {
			editsAny[i] = e
		}
		out = append(out, protocol.TextDocumentEdit{
			TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
				TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
			},
			Edits: editsAny,
		})
	}
	return out
}
