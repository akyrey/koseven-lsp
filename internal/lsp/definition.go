package lsp

import (
	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/phputil"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// Definition handles textDocument/definition.
//
// Supported patterns (tried in order, first match wins):
//
//  1. View name string — View::factory('x'), new View('x'), Kohana::find_file('views','x')
//     → jump to view file(s) via the view index cascade.
//
//  2. ORM/Model factory — ORM::factory('Member'), Model::factory('Member')
//     → jump to classes/Model/Member.php in the cascade.
//
//  3. Kohana::find_file for non-view types — find_file('classes','Model_X'),
//     find_file('i18n','en'), find_file('messages','user'), find_file('config','database')
//     → jump to <type>/<resolved_path> in the cascade.
func (s *Server) Definition(_ *glsp.Context, p *protocol.DefinitionParams) (any, error) {
	src, err := s.docs.Read(p.TextDocument.URI)
	if err != nil {
		return nil, nil
	}
	path := URIToPath(p.TextDocument.URI)
	offset := positionToByteOffset(src, p.Position)

	// Pattern 1: view names (uses the in-memory index).
	if idx := s.viewIndex(); idx != nil {
		if viewName := findViewNameAtOffset(src, path, offset); viewName != "" {
			defs := idx.Resolve(viewName)
			if len(defs) > 0 {
				locs := make([]protocol.Location, len(defs))
				for i, d := range defs {
					locs[i] = protocol.Location{URI: PathToURI(d.Path)}
				}
				return locs, nil
			}
		}
	}

	// Patterns 2 & 3 need the cascade state for direct filesystem lookups.
	root, cfg, modules := s.cascadeState()
	if root == "" {
		return nil, nil
	}

	// Pattern 2: ORM::factory / Model::factory.
	if className, relPath := findClassFactoryAtOffset(src, path, offset); relPath != "" {
		_ = className
		if locs := cascadeLocs(root, cfg, modules, "classes", relPath); len(locs) > 0 {
			return locs, nil
		}
	}

	// Pattern 3: Kohana::find_file for all non-view types.
	if fileType, relPath := findFindFileAtOffset(src, path, offset); fileType != "" && fileType != "views" {
		if locs := cascadeLocs(root, cfg, modules, fileType, relPath); len(locs) > 0 {
			return locs, nil
		}
	}

	return nil, nil
}

// cascadeLocs calls project.FindCascadeFiles and converts the results to LSP
// locations (pointing to the top of each file).
func cascadeLocs(root string, cfg config.Config, modules []project.Module, subdir, relPath string) []protocol.Location {
	paths := project.FindCascadeFiles(root, cfg, modules, subdir, relPath)
	if len(paths) == 0 {
		return nil
	}
	locs := make([]protocol.Location, len(paths))
	for i, p := range paths {
		locs[i] = protocol.Location{URI: PathToURI(p)}
	}
	return locs
}

// ─── View name finder ─────────────────────────────────────────────────────────

// findViewNameAtOffset parses src and returns the logical view name of the
// string literal that contains offset, when that literal is in a view-construction
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
		if len(n.Args) > 0 {
			if expr := phputil.ArgExpr(n.Args[0]); expr != nil && v.cursorIn(expr) {
				v.viewName, _ = phputil.ScalarStringVal(expr)
			}
		}

	case (className == "Kohana" || className == "\\Kohana") && method == "find_file":
		if len(n.Args) < 2 {
			return
		}
		kind, ok := phputil.ScalarStringVal(phputil.ArgExpr(n.Args[0]))
		if !ok || kind != "views" {
			return
		}
		if expr := phputil.ArgExpr(n.Args[1]); expr != nil && v.cursorIn(expr) {
			v.viewName, _ = phputil.ScalarStringVal(expr)
		}
	}
}

func (v *viewNameFinder) ExprNew(n *ast.ExprNew) {
	if v.viewName != "" {
		return
	}
	if phputil.NameToString(n.Class) != "View" && phputil.NameToString(n.Class) != "\\View" {
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

// ─── Class factory finder ─────────────────────────────────────────────────────

// findClassFactoryAtOffset returns the full class name and its PSR-0 relative
// path when the cursor is on the string literal of ORM::factory('X') or
// Model::factory('X'). Both fields are "" when no match.
func findClassFactoryAtOffset(src []byte, path string, offset int) (className, relPath string) {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return
	}
	cf := &classFactoryFinder{offset: offset}
	traverser.NewTraverser(cf).Traverse(root)
	return cf.className, cf.relPath
}

// classFactoryFinder detects ORM::factory('X') and Model::factory('X') when
// the cursor is on the model-name string argument.
type classFactoryFinder struct {
	visitor.Null
	offset    int
	className string
	relPath   string
}

func (v *classFactoryFinder) ExprStaticCall(n *ast.ExprStaticCall) {
	if v.className != "" {
		return
	}
	className := phputil.NameToString(n.Class)
	if className != "ORM" && className != "Model" &&
		className != "\\ORM" && className != "\\Model" {
		return
	}
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok || string(methodID.Value) != "factory" {
		return
	}
	if len(n.Args) == 0 {
		return
	}
	expr := phputil.ArgExpr(n.Args[0])
	if expr == nil {
		return
	}
	pos := expr.GetPosition()
	if pos == nil || v.offset < pos.StartPos || v.offset >= pos.EndPos {
		return
	}
	modelName, ok := phputil.ScalarStringVal(expr)
	if !ok || modelName == "" {
		return
	}
	v.className = "Model_" + modelName
	v.relPath = project.ClassNameToRelPath(v.className)
}

// ─── Kohana::find_file finder (non-view types) ───────────────────────────────

// findFindFileAtOffset returns the file type and resolved relative path when
// the cursor is on the name argument of Kohana::find_file('type', 'name') for
// any type other than 'views' (which is handled by findViewNameAtOffset).
func findFindFileAtOffset(src []byte, path string, offset int) (fileType, relPath string) {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return
	}
	ff := &findFileFinder{offset: offset}
	traverser.NewTraverser(ff).Traverse(root)
	return ff.fileType, ff.relPath
}

type findFileFinder struct {
	visitor.Null
	offset   int
	fileType string
	relPath  string
}

func (v *findFileFinder) ExprStaticCall(n *ast.ExprStaticCall) {
	if v.fileType != "" {
		return
	}
	className := phputil.NameToString(n.Class)
	if className != "Kohana" && className != "\\Kohana" {
		return
	}
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok || string(methodID.Value) != "find_file" {
		return
	}
	if len(n.Args) < 2 {
		return
	}
	kind, ok := phputil.ScalarStringVal(phputil.ArgExpr(n.Args[0]))
	if !ok || kind == "" {
		return
	}
	expr := phputil.ArgExpr(n.Args[1])
	if expr == nil {
		return
	}
	pos := expr.GetPosition()
	if pos == nil || v.offset < pos.StartPos || v.offset >= pos.EndPos {
		return
	}
	name, ok := phputil.ScalarStringVal(expr)
	if !ok || name == "" {
		return
	}
	v.fileType = kind
	// For 'classes', convert PSR-0 underscores to path separators.
	// For all other types (i18n, messages, config, etc.) the name is used as-is.
	if kind == "classes" {
		v.relPath = project.ClassNameToRelPath(name)
	} else {
		v.relPath = name + ".php"
	}
}
