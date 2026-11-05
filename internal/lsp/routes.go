package lsp

import (
	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/route"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// routeMatch holds the controller/action/directory strings found near a cursor
// position and which of the three fields the cursor is actually on.
type routeMatch struct {
	Controller string
	Action     string
	Directory  string
	CursorOn   string // "controller" | "action" | "directory"
}

// findRouteAtOffset returns the controller/action reference whose controller,
// action or directory string literal contains offset. Supported call sites are
// those recognised by route.Extract:
//  1. Route::set('name','pat')->defaults(['controller'=>'x','action'=>'y'])
//  2. Route::url('name', ['controller'=>'x','action'=>'y'])
//  3. Route::get('name')->uri(['controller'=>'x','action'=>'y'])
//  4. Request::factory()->controller('x')->action('y')
//  5. Custom helpers registered in koseven-ls.toml route_helpers
func findRouteAtOffset(src []byte, path string, offset int, helpers []config.RouteHelper) (routeMatch, bool) {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return routeMatch{}, false
	}
	for _, r := range route.Extract(path, root, helpers) {
		var on string
		switch {
		case r.Controller.Contains(offset):
			on = "controller"
		case r.Action.Contains(offset):
			on = "action"
		case r.Directory.Contains(offset):
			on = "directory"
		default:
			continue
		}
		return routeMatch{
			Controller: r.Controller.Value,
			Action:     r.Action.Value,
			Directory:  r.Directory.Value,
			CursorOn:   on,
		}, true
	}
	return routeMatch{}, false
}

// locateControllerAction resolves controller/action/directory strings to LSP
// Locations. The controller file is found via the cascade; when action is
// non-empty the location points at the action_<name> method line. Falls back
// to file-top when the method is not found. Returns nil when no file exists.
func locateControllerAction(root string, cfg config.Config, modules []project.Module, controller, action, directory string) []protocol.Location {
	if controller == "" {
		return nil
	}
	className := route.ControllerClassName(controller, directory)
	relPath := project.ClassNameToRelPath(className)
	paths := project.FindCascadeFiles(root, cfg, modules, "classes", relPath)
	if len(paths) == 0 {
		return nil
	}
	locs := make([]protocol.Location, 0, len(paths))
	for _, p := range paths {
		line := uint32(0)
		if action != "" {
			if l, ok := findActionMethodLine(p, action); ok {
				line = l
			}
		}
		locs = append(locs, protocol.Location{
			URI: PathToURI(p),
			Range: protocol.Range{
				Start: protocol.Position{Line: line},
				End:   protocol.Position{Line: line},
			},
		})
	}
	return locs
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// findActionMethodLine parses filePath and returns the 0-based line number of
// the action_<actionName> method declaration. Returns (0, false) when the file
// cannot be parsed or the method is not found.
func findActionMethodLine(filePath, actionName string) (uint32, bool) {
	root, err := phpparse.File(filePath)
	if err != nil || root == nil {
		return 0, false
	}
	f := &actionMethodFinder{targetName: "action_" + actionName}
	traverser.NewTraverser(f).Traverse(root)
	return f.line, f.found
}

type actionMethodFinder struct {
	visitor.Null
	targetName string
	line       uint32
	found      bool
}

func (f *actionMethodFinder) StmtClassMethod(n *ast.StmtClassMethod) {
	if f.found {
		return
	}
	nameID, ok := n.Name.(*ast.Identifier)
	if !ok || string(nameID.Value) != f.targetName {
		return
	}
	pos := n.GetPosition()
	if pos != nil && pos.StartLine > 0 {
		f.line = uint32(pos.StartLine - 1)
		f.found = true
	}
}
