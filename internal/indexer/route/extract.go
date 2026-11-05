package route

import (
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// Extract walks a parsed PHP file and returns every controller/action
// reference it contains, in source order. Only references carrying at least
// one string-literal field (controller, action or directory) are returned.
func Extract(path string, astRoot ast.Vertex, helpers []config.RouteHelper) []Ref {
	if astRoot == nil {
		return nil
	}
	v := &extractVisitor{
		path:        path,
		helpers:     helpers,
		seenRequest: make(map[int]struct{}),
	}
	traverser.NewTraverser(v).Traverse(astRoot)
	return v.refs
}

type extractVisitor struct {
	visitor.Null
	path    string
	helpers []config.RouteHelper
	refs    []Ref
	// seenRequest holds the StartPos of Request chain roots already recorded.
	// DFS pre-order visits the outermost call of a chain first, so the first
	// visit sees the whole chain and inner calls are skipped.
	seenRequest map[int]struct{}
}

func (v *extractVisitor) add(r Ref) {
	if r.Controller.Present() || r.Action.Present() || r.Directory.Present() {
		v.refs = append(v.refs, r)
	}
}

// ─── Method calls: ->defaults(), ->uri(), Request chains ─────────────────────

func (v *extractVisitor) ExprMethodCall(n *ast.ExprMethodCall) {
	method := identifierValue(n.Method)
	switch {
	case method == "defaults" && isRouteSetRoot(n.Var):
		v.addArrayRef(KindRouteDefaults, n.Args, 0)
		return
	case method == "uri" && isStaticCall(n.Var, "Route", "get"):
		v.addArrayRef(KindRouteURI, n.Args, 0)
		return
	}
	v.tryRequestChain(n)
}

func (v *extractVisitor) tryRequestChain(n *ast.ExprMethodCall) {
	root := chainVarRoot(n)
	if !isStaticCallOn(root, "Request") {
		return
	}
	pos := root.GetPosition()
	if pos == nil {
		return
	}
	if _, seen := v.seenRequest[pos.StartPos]; seen {
		return
	}
	v.seenRequest[pos.StartPos] = struct{}{}

	v.add(Ref{
		Kind:       KindRequest,
		Controller: v.chainMethodField(n, "controller"),
		Action:     v.chainMethodField(n, "action"),
		Directory:  v.chainMethodField(n, "directory"),
	})
}

// chainMethodField walks a method-call chain looking for a call named target
// and returns its first argument as a Field. The outermost (last applied) call
// wins when the same setter appears more than once.
func (v *extractVisitor) chainMethodField(node ast.Vertex, target string) Field {
	for {
		mc, ok := node.(*ast.ExprMethodCall)
		if !ok {
			return Field{}
		}
		if identifierValue(mc.Method) == target && len(mc.Args) > 0 {
			return v.stringField(phputil.ArgExpr(mc.Args[0]))
		}
		node = mc.Var
	}
}

// ─── Static calls: Route::url(), helpers ──────────────────────────────────────

func (v *extractVisitor) ExprStaticCall(n *ast.ExprStaticCall) {
	if isStaticCall(n, "Route", "url") {
		v.addArrayRef(KindRouteURL, n.Args, 1)
		return
	}
	if len(v.helpers) == 0 {
		return
	}
	name := strings.TrimPrefix(phputil.NameToString(n.Class), "\\") + "::" + identifierValue(n.Call)
	for _, h := range v.helpers {
		if h.Name == name {
			v.addHelperRef(n.Args, h)
			return
		}
	}
}

func (v *extractVisitor) ExprFunctionCall(n *ast.ExprFunctionCall) {
	if len(v.helpers) == 0 {
		return
	}
	name := strings.TrimPrefix(phputil.NameToString(n.Function), "\\")
	for _, h := range v.helpers {
		if !strings.Contains(h.Name, "::") && h.Name == name {
			v.addHelperRef(n.Args, h)
			return
		}
	}
}

func (v *extractVisitor) addHelperRef(args []ast.Vertex, h config.RouteHelper) {
	v.add(Ref{
		Kind:       KindHelper,
		Controller: v.argField(args, h.Controller),
		Action:     v.argField(args, h.Action),
		Directory:  v.argField(args, h.Directory),
	})
}

func (v *extractVisitor) argField(args []ast.Vertex, idx *int) Field {
	if idx == nil || *idx < 0 || *idx >= len(args) {
		return Field{}
	}
	return v.stringField(phputil.ArgExpr(args[*idx]))
}

// ─── Array params ─────────────────────────────────────────────────────────────

// addArrayRef records a reference built from the route-params array found at
// args[argIdx] (['controller' => ..., 'action' => ..., 'directory' => ...]).
func (v *extractVisitor) addArrayRef(kind Kind, args []ast.Vertex, argIdx int) {
	if argIdx >= len(args) {
		return
	}
	arr, ok := phputil.ArgExpr(args[argIdx]).(*ast.ExprArray)
	if !ok {
		return
	}
	r := Ref{Kind: kind}
	for _, item := range arr.Items {
		elem, ok := item.(*ast.ExprArrayItem)
		if !ok || elem.Key == nil || elem.Val == nil {
			continue
		}
		key, ok := phputil.ScalarStringVal(elem.Key)
		if !ok {
			continue
		}
		switch key {
		case "controller":
			r.Controller = v.stringField(elem.Val)
		case "action":
			r.Action = v.stringField(elem.Val)
		case "directory":
			r.Directory = v.stringField(elem.Val)
		}
	}
	v.add(r)
}

// stringField returns a Field for a string literal node, or a zero Field when
// node is not a non-empty string literal.
func (v *extractVisitor) stringField(node ast.Vertex) Field {
	val, ok := phputil.ScalarStringVal(node)
	if !ok || val == "" {
		return Field{}
	}
	return Field{Value: val, Loc: phputil.FromPosition(v.path, node.GetPosition())}
}

// ─── AST helpers ──────────────────────────────────────────────────────────────

func identifierValue(node ast.Vertex) string {
	id, ok := node.(*ast.Identifier)
	if !ok {
		return ""
	}
	return string(id.Value)
}

// isStaticCallOn reports whether node is a static call on className.
func isStaticCallOn(node ast.Vertex, className string) bool {
	sc, ok := node.(*ast.ExprStaticCall)
	if !ok {
		return false
	}
	return strings.TrimPrefix(phputil.NameToString(sc.Class), "\\") == className
}

// isStaticCall reports whether node is className::method(...).
func isStaticCall(node ast.Vertex, className, method string) bool {
	if !isStaticCallOn(node, className) {
		return false
	}
	return identifierValue(node.(*ast.ExprStaticCall).Call) == method
}

// isRouteSetRoot reports whether an expression chain roots at Route::set(...),
// possibly through intermediate method calls like ->filter(...).
func isRouteSetRoot(node ast.Vertex) bool {
	return isStaticCall(chainVarRoot(node), "Route", "set")
}

// chainVarRoot follows ExprMethodCall.Var to the innermost receiver.
func chainVarRoot(node ast.Vertex) ast.Vertex {
	for {
		mc, ok := node.(*ast.ExprMethodCall)
		if !ok {
			return node
		}
		node = mc.Var
	}
}
