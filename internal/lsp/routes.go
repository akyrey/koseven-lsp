package lsp

import (
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/phputil"
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

// findRouteAtOffset runs the four route-context finders in order. Returns the
// first match plus true, or a zero value plus false when nothing matches.
//
// Supported call sites:
//  1. Route::set('name','pat')->defaults(['controller'=>'x','action'=>'y'])
//  2. Route::url('name', ['controller'=>'x','action'=>'y'])
//  3. Request::factory()->controller('x')->action('y')
//  4. Custom helpers registered in koseven-ls.toml route_helpers
func findRouteAtOffset(src []byte, path string, offset int, helpers []config.RouteHelper) (routeMatch, bool) {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return routeMatch{}, false
	}

	finders := []interface{ result() (routeMatch, bool) }{
		&routeDefaultsFinder{offset: offset},
		&routeURLFinder{offset: offset},
		&requestFactoryFinder{offset: offset},
		&routeHelperFinder{offset: offset, helpers: helpers},
	}
	for _, f := range finders {
		switch v := f.(type) {
		case *routeDefaultsFinder:
			traverser.NewTraverser(v).Traverse(root)
		case *routeURLFinder:
			traverser.NewTraverser(v).Traverse(root)
		case *requestFactoryFinder:
			traverser.NewTraverser(v).Traverse(root)
		case *routeHelperFinder:
			traverser.NewTraverser(v).Traverse(root)
		}
		if m, ok := f.result(); ok {
			return m, true
		}
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
	className := buildControllerClassName(controller, directory)
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

// titleCaseSegments title-cases each underscore-separated segment.
// "pages" → "Pages", "auth_user" → "Auth_User".
func titleCaseSegments(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "_")
}

// buildControllerClassName constructs the Kohana PSR-0 class name for a
// controller, optionally prefixed by an HMVC directory segment.
// "pages","" → "Controller_Pages", "users","admin" → "Controller_Admin_Users".
func buildControllerClassName(controller, directory string) string {
	ctrl := titleCaseSegments(controller)
	if directory == "" {
		return "Controller_" + ctrl
	}
	return "Controller_" + titleCaseSegments(directory) + "_" + ctrl
}

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

// cursorInPos reports whether offset falls within [StartPos, EndPos).
func cursorInPos(offset int, node ast.Vertex) bool {
	if node == nil {
		return false
	}
	pos := node.GetPosition()
	if pos == nil {
		return false
	}
	return offset >= pos.StartPos && offset < pos.EndPos
}

// extractRouteArray collects controller/action/directory string values from a
// PHP array literal, and records which key the cursor is on.
func extractRouteArray(arr *ast.ExprArray, offset int) (ctrl, act, dir, cursorKey string) {
	for _, item := range arr.Items {
		elem, ok := item.(*ast.ExprArrayItem)
		if !ok || elem.Key == nil || elem.Val == nil {
			continue
		}
		key, ok := phputil.ScalarStringVal(elem.Key)
		if !ok {
			continue
		}
		val, _ := phputil.ScalarStringVal(elem.Val)
		switch key {
		case "controller":
			ctrl = val
		case "action":
			act = val
		case "directory":
			dir = val
		}
		if cursorKey == "" && cursorInPos(offset, elem.Val) {
			cursorKey = key
		}
	}
	return
}

// ─── Route::set(...)->defaults([...]) ────────────────────────────────────────

type routeDefaultsFinder struct {
	visitor.Null
	offset               int
	controller           string
	action               string
	directory            string
	cursorOn             string
	found                bool
}

func (f *routeDefaultsFinder) result() (routeMatch, bool) {
	if !f.found {
		return routeMatch{}, false
	}
	return routeMatch{Controller: f.controller, Action: f.action, Directory: f.directory, CursorOn: f.cursorOn}, true
}

func (f *routeDefaultsFinder) ExprMethodCall(n *ast.ExprMethodCall) {
	if f.found {
		return
	}
	mid, ok := n.Method.(*ast.Identifier)
	if !ok || string(mid.Value) != "defaults" {
		return
	}
	if !isRouteSetRoot(n.Var) {
		return
	}
	if len(n.Args) == 0 {
		return
	}
	arr, ok := phputil.ArgExpr(n.Args[0]).(*ast.ExprArray)
	if !ok {
		return
	}
	ctrl, act, dir, cursorKey := extractRouteArray(arr, f.offset)
	if cursorKey == "" {
		return
	}
	f.controller = ctrl
	f.action = act
	f.directory = dir
	f.cursorOn = cursorKey
	f.found = true
}

// isRouteSetRoot walks an expression chain and returns true when it roots at
// a Route::set(...) static call, possibly through intermediate method calls
// like ->filter(...).
func isRouteSetRoot(node ast.Vertex) bool {
	switch n := node.(type) {
	case *ast.ExprStaticCall:
		cn := phputil.NameToString(n.Class)
		mid, ok := n.Call.(*ast.Identifier)
		return ok && (cn == "Route" || cn == "\\Route") && string(mid.Value) == "set"
	case *ast.ExprMethodCall:
		return isRouteSetRoot(n.Var)
	}
	return false
}

// ─── Route::url('name', [...]) ────────────────────────────────────────────────

type routeURLFinder struct {
	visitor.Null
	offset     int
	controller string
	action     string
	directory  string
	cursorOn   string
	found      bool
}

func (f *routeURLFinder) result() (routeMatch, bool) {
	if !f.found {
		return routeMatch{}, false
	}
	return routeMatch{Controller: f.controller, Action: f.action, Directory: f.directory, CursorOn: f.cursorOn}, true
}

func (f *routeURLFinder) ExprStaticCall(n *ast.ExprStaticCall) {
	if f.found {
		return
	}
	cn := phputil.NameToString(n.Class)
	if cn != "Route" && cn != "\\Route" {
		return
	}
	mid, ok := n.Call.(*ast.Identifier)
	if !ok || string(mid.Value) != "url" {
		return
	}
	// Second arg is the params array.
	if len(n.Args) < 2 {
		return
	}
	arr, ok := phputil.ArgExpr(n.Args[1]).(*ast.ExprArray)
	if !ok {
		return
	}
	ctrl, act, dir, cursorKey := extractRouteArray(arr, f.offset)
	if cursorKey == "" {
		return
	}
	f.controller = ctrl
	f.action = act
	f.directory = dir
	f.cursorOn = cursorKey
	f.found = true
}

// ─── Request::factory()->controller('x')->action('y') ────────────────────────

type requestFactoryFinder struct {
	visitor.Null
	offset     int
	controller string
	action     string
	cursorOn   string
	found      bool
}

func (f *requestFactoryFinder) result() (routeMatch, bool) {
	if !f.found {
		return routeMatch{}, false
	}
	return routeMatch{Controller: f.controller, Action: f.action, CursorOn: f.cursorOn}, true
}

func (f *requestFactoryFinder) ExprMethodCall(n *ast.ExprMethodCall) {
	if f.found {
		return
	}
	mid, ok := n.Method.(*ast.Identifier)
	if !ok {
		return
	}
	method := string(mid.Value)
	if method != "controller" && method != "action" {
		return
	}
	if !isRequestChainRoot(chainVarRoot(n.Var)) {
		return
	}
	if len(n.Args) == 0 {
		return
	}
	arg := phputil.ArgExpr(n.Args[0])
	if arg == nil || !cursorInPos(f.offset, arg) {
		return
	}
	val, ok := phputil.ScalarStringVal(arg)
	if !ok {
		return
	}
	f.cursorOn = method
	f.found = true
	switch method {
	case "controller":
		f.controller = val
		// action may be downstream (outer chain) — not accessible via Var; leave empty.
	case "action":
		f.action = val
		// Walk upstream chain for a controller() call.
		f.controller = chainMethodString(n.Var, "controller")
	}
}

// chainVarRoot follows ExprMethodCall.Var recursively to the innermost Vertex.
func chainVarRoot(node ast.Vertex) ast.Vertex {
	mc, ok := node.(*ast.ExprMethodCall)
	if !ok {
		return node
	}
	return chainVarRoot(mc.Var)
}

// isRequestChainRoot returns true when node is a static call on class Request.
func isRequestChainRoot(node ast.Vertex) bool {
	sc, ok := node.(*ast.ExprStaticCall)
	if !ok {
		return false
	}
	cn := phputil.NameToString(sc.Class)
	return cn == "Request" || cn == "\\Request"
}

// chainMethodString walks a method-call chain looking for a call whose method
// name matches target, returning its first string argument or "".
func chainMethodString(node ast.Vertex, target string) string {
	mc, ok := node.(*ast.ExprMethodCall)
	if !ok {
		return ""
	}
	mid, ok := mc.Method.(*ast.Identifier)
	if ok && string(mid.Value) == target && len(mc.Args) > 0 {
		val, _ := phputil.ScalarStringVal(phputil.ArgExpr(mc.Args[0]))
		return val
	}
	return chainMethodString(mc.Var, target)
}

// ─── Configured route helpers ─────────────────────────────────────────────────

type routeHelperFinder struct {
	visitor.Null
	offset     int
	helpers    []config.RouteHelper
	controller string
	action     string
	directory  string
	cursorOn   string
	found      bool
}

func (f *routeHelperFinder) result() (routeMatch, bool) {
	if !f.found {
		return routeMatch{}, false
	}
	return routeMatch{Controller: f.controller, Action: f.action, Directory: f.directory, CursorOn: f.cursorOn}, true
}

func (f *routeHelperFinder) ExprStaticCall(n *ast.ExprStaticCall) {
	if f.found {
		return
	}
	cn := phputil.NameToString(n.Class)
	if strings.HasPrefix(cn, "\\") {
		cn = cn[1:]
	}
	mid, ok := n.Call.(*ast.Identifier)
	if !ok {
		return
	}
	fullName := cn + "::" + string(mid.Value)
	for _, h := range f.helpers {
		if h.Name == fullName {
			f.tryMatchArgs(n.Args, h)
			return
		}
	}
}

func (f *routeHelperFinder) ExprFunctionCall(n *ast.ExprFunctionCall) {
	if f.found {
		return
	}
	fnName := phputil.NameToString(n.Function)
	if strings.HasPrefix(fnName, "\\") {
		fnName = fnName[1:]
	}
	for _, h := range f.helpers {
		if !strings.Contains(h.Name, "::") && h.Name == fnName {
			f.tryMatchArgs(n.Args, h)
			return
		}
	}
}

func (f *routeHelperFinder) tryMatchArgs(args []ast.Vertex, h config.RouteHelper) {
	strAtIdx := func(idxPtr *int) (string, bool) {
		if idxPtr == nil || *idxPtr < 0 || *idxPtr >= len(args) {
			return "", false
		}
		return phputil.ScalarStringVal(phputil.ArgExpr(args[*idxPtr]))
	}
	cursorAtIdx := func(idxPtr *int) bool {
		if idxPtr == nil || *idxPtr < 0 || *idxPtr >= len(args) {
			return false
		}
		return cursorInPos(f.offset, phputil.ArgExpr(args[*idxPtr]))
	}

	// Collect all values up-front.
	ctrl, _ := strAtIdx(h.Controller)
	act, _ := strAtIdx(h.Action)
	dir, _ := strAtIdx(h.Directory)

	switch {
	case cursorAtIdx(h.Controller):
		f.cursorOn = "controller"
	case cursorAtIdx(h.Action):
		f.cursorOn = "action"
	case cursorAtIdx(h.Directory):
		f.cursorOn = "directory"
	default:
		return
	}
	f.controller = ctrl
	f.action = act
	f.directory = dir
	f.found = true
}
