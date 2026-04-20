package view

import (
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// ExtractFileUsages parses a single PHP file's AST and returns all discovered
// ViewUsages and any globally exposed variables. Exported for use by the LSP
// diagnostic layer.
func ExtractFileUsages(path string, astRoot ast.Vertex) ([]ViewUsage, []ExposedVar) {
	ev := &extractVisitor{
		path: path,
		seen: make(map[int]struct{}),
	}
	traverser.NewTraverser(ev).Traverse(astRoot)
	return ev.usages, ev.globals
}

// extractVisitor is a single-pass AST visitor that collects view construction
// call sites and their chained variable exposure calls.
//
// Chain deduplication: DFS pre-order visits the outermost ExprMethodCall first.
// tryExtractChain is called on every ExprStaticCall, ExprNew, and ExprMethodCall.
// The first (outermost) call that resolves to a view base registers the full
// chain in `seen` (keyed on the base node's StartPos); subsequent inner nodes
// see the key and skip.
//
// Limitation: split-assignment patterns ($view = View::factory(...); $view->set(...);)
// are not supported. Only single-expression chains are extracted.
type extractVisitor struct {
	visitor.Null
	path    string
	seen    map[int]struct{} // base StartPos → already recorded
	usages  []ViewUsage
	globals []ExposedVar
}

func (v *extractVisitor) ExprStaticCall(n *ast.ExprStaticCall) {
	v.tryGlobalStatic(n)
	v.record(n)
}

func (v *extractVisitor) ExprNew(n *ast.ExprNew) {
	v.record(n)
}

func (v *extractVisitor) ExprMethodCall(n *ast.ExprMethodCall) {
	v.record(n)
}

func (v *extractVisitor) record(node ast.Vertex) {
	viewName, construct, vars, basePos, nameRange, found := tryExtractChain(node)
	if !found {
		return
	}
	if _, already := v.seen[basePos]; already {
		return
	}
	v.seen[basePos] = struct{}{}

	pos := node.GetPosition()
	var r protocol.Range
	if pos != nil && pos.StartLine > 0 {
		r = protocol.Range{
			Start: protocol.Position{Line: uint32(pos.StartLine - 1)},
			End:   protocol.Position{Line: uint32(pos.EndLine - 1)},
		}
	}
	v.usages = append(v.usages, ViewUsage{
		Name:        viewName,
		File:        v.path,
		Range:       r,
		NameRange:   nameRange,
		Construct:   construct,
		ExposedVars: vars,
	})
}

func (v *extractVisitor) tryGlobalStatic(n *ast.ExprStaticCall) {
	className := phputil.NameToString(n.Class)
	if className != "View" && className != "\\View" {
		return
	}
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok {
		return
	}
	var source VarSource
	switch string(methodID.Value) {
	case "set_global":
		source = SourceSetGlobal
	case "bind_global":
		source = SourceBindGlobal
	default:
		return
	}
	var vars []ExposedVar
	if source == SourceSetGlobal {
		vars = extractSetArgs(n.Args)
	} else {
		vars = extractBindArgs(n.Args)
	}
	for i := range vars {
		vars[i].Source = source
	}
	v.globals = append(v.globals, vars...)
}

// — Chain extraction —

// tryExtractChain recursively unwraps a method chain and returns the view name,
// construct type, all exposed vars, the StartPos of the base construction node
// (for deduplication), and the LSP range of the name string literal.
func tryExtractChain(node ast.Vertex) (viewName string, c Construct, vars []ExposedVar, basePos int, nameRange protocol.Range, found bool) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.ExprStaticCall:
		if name := staticCallViewName(n); name != "" {
			pos := n.GetPosition()
			if pos == nil {
				return
			}
			return name, ConstructViewFactory, extractSecondArgVars(n.Args), pos.StartPos, argLineRange(n.Args, 0), true
		}
		if name := findFileViewName(n); name != "" {
			pos := n.GetPosition()
			if pos == nil {
				return
			}
			// find_file's view name is the second arg
			return name, ConstructFindFile, nil, pos.StartPos, argLineRange(n.Args, 1), true
		}

	case *ast.ExprNew:
		if name := newViewName(n); name != "" {
			pos := n.GetPosition()
			if pos == nil {
				return
			}
			return name, ConstructNewView, extractSecondArgVars(n.Args), pos.StartPos, argLineRange(n.Args, 0), true
		}

	case *ast.ExprMethodCall:
		vn, cn, baseVars, bp, nr, ok := tryExtractChain(n.Var)
		if !ok {
			return
		}
		return vn, cn, append(baseVars, extractChainMethodVars(n)...), bp, nr, true
	}
	return
}

// argLineRange returns a line-level LSP Range for the expression at args[i].
// Character offsets are 0; callers needing column precision must compute from
// source bytes separately.
func argLineRange(args []ast.Vertex, i int) protocol.Range {
	if i >= len(args) {
		return protocol.Range{}
	}
	expr := phputil.ArgExpr(args[i])
	if expr == nil {
		return protocol.Range{}
	}
	pos := expr.GetPosition()
	if pos == nil || pos.StartLine == 0 {
		return protocol.Range{}
	}
	line := uint32(pos.StartLine - 1)
	return protocol.Range{
		Start: protocol.Position{Line: line},
		End:   protocol.Position{Line: line},
	}
}

// — View construction matchers —

func staticCallViewName(n *ast.ExprStaticCall) string {
	className := phputil.NameToString(n.Class)
	if className != "View" && className != "\\View" {
		return ""
	}
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok || string(methodID.Value) != "factory" {
		return ""
	}
	name, ok := firstArgString(n.Args)
	if !ok {
		return ""
	}
	return name
}

func findFileViewName(n *ast.ExprStaticCall) string {
	className := phputil.NameToString(n.Class)
	if className != "Kohana" && className != "\\Kohana" {
		return ""
	}
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok || string(methodID.Value) != "find_file" {
		return ""
	}
	if len(n.Args) < 2 {
		return ""
	}
	kind, ok := firstArgString(n.Args)
	if !ok || kind != "views" {
		return ""
	}
	expr := phputil.ArgExpr(n.Args[1])
	if expr == nil {
		return ""
	}
	name, ok := phputil.ScalarStringVal(expr)
	if !ok {
		return ""
	}
	return name
}

func newViewName(n *ast.ExprNew) string {
	className := phputil.NameToString(n.Class)
	if className != "View" && className != "\\View" {
		return ""
	}
	name, ok := firstArgString(n.Args)
	if !ok {
		return ""
	}
	return name
}

// — Variable extraction from chain method calls —

func extractChainMethodVars(n *ast.ExprMethodCall) []ExposedVar {
	methodID, ok := n.Method.(*ast.Identifier)
	if !ok {
		return nil
	}
	switch string(methodID.Value) {
	case "set", "set_global":
		vars := extractSetArgs(n.Args)
		if string(methodID.Value) == "set_global" {
			for i := range vars {
				vars[i].Source = SourceSetGlobal
			}
		}
		return vars
	case "bind", "bind_global":
		vars := extractBindArgs(n.Args)
		if string(methodID.Value) == "bind_global" {
			for i := range vars {
				vars[i].Source = SourceBindGlobal
			}
		}
		return vars
	}
	return nil
}

// extractSecondArgVars extracts variables from the second argument of
// View::factory('name', ['x' => $val, ...]) or new View('name', [...]).
func extractSecondArgVars(args []ast.Vertex) []ExposedVar {
	if len(args) < 2 {
		return nil
	}
	expr := phputil.ArgExpr(args[1])
	if expr == nil {
		return nil
	}
	arr, ok := expr.(*ast.ExprArray)
	if !ok {
		return nil
	}
	return extractArrayItems(arr, SourceFactoryArray)
}

// extractSetArgs handles ->set('key', $val) and ->set(['key' => $val, ...]).
func extractSetArgs(args []ast.Vertex) []ExposedVar {
	if len(args) == 0 {
		return nil
	}
	expr := phputil.ArgExpr(args[0])
	if expr == nil {
		return nil
	}
	// ->set('key', $val)
	if key, ok := phputil.ScalarStringVal(expr); ok {
		var typ PHPType
		if len(args) >= 2 {
			typ = inferType(phputil.ArgExpr(args[1]))
		}
		return []ExposedVar{{Name: key, Source: SourceSet, Type: typ}}
	}
	// ->set(['key' => $val, ...])
	arr, ok := expr.(*ast.ExprArray)
	if !ok {
		return nil
	}
	return extractArrayItems(arr, SourceSet)
}

// extractBindArgs handles ->bind('key', $ref).
func extractBindArgs(args []ast.Vertex) []ExposedVar {
	if len(args) == 0 {
		return nil
	}
	key, ok := phputil.ScalarStringVal(phputil.ArgExpr(args[0]))
	if !ok {
		return nil
	}
	return []ExposedVar{{Name: key, Source: SourceBind}}
}

func extractArrayItems(arr *ast.ExprArray, source VarSource) []ExposedVar {
	var vars []ExposedVar
	for _, item := range arr.Items {
		elem, ok := item.(*ast.ExprArrayItem)
		if !ok || elem.Key == nil {
			continue
		}
		key, ok := phputil.ScalarStringVal(elem.Key)
		if !ok {
			continue
		}
		vars = append(vars, ExposedVar{
			Name:   key,
			Type:   inferType(elem.Val),
			Source: source,
		})
	}
	return vars
}

// — Type inference —

// inferType performs lightweight type inference on a PHP expression node.
// Handles literals, true/false/null constants, new Foo(), ORM::factory('X'),
// and Foo::factory('X') → Model_X. Everything else is TypeUnknown.
func inferType(node ast.Vertex) PHPType {
	if node == nil {
		return PHPType{Kind: TypeUnknown}
	}
	switch n := node.(type) {
	case *ast.ScalarString:
		return PHPType{Kind: TypeString}
	case *ast.ScalarLnumber:
		return PHPType{Kind: TypeInt}
	case *ast.ScalarDnumber:
		return PHPType{Kind: TypeFloat}
	case *ast.ExprConstFetch:
		switch strings.ToUpper(phputil.NameToString(n.Const)) {
		case "TRUE":
			return PHPType{Kind: TypeBool}
		case "FALSE":
			return PHPType{Kind: TypeBool}
		case "NULL":
			return PHPType{Kind: TypeNull}
		}
	case *ast.ExprNew:
		className := phputil.NameToString(n.Class)
		if className != "" {
			return PHPType{Kind: TypeClass, Class: className}
		}
	case *ast.ExprStaticCall:
		className := phputil.NameToString(n.Class)
		methodID, ok := n.Call.(*ast.Identifier)
		if !ok || string(methodID.Value) != "factory" {
			break
		}
		// ORM::factory('Member') or Model::factory('Member') → Model_Member
		if className == "ORM" || className == "Model" {
			if modelName, ok := firstArgString(n.Args); ok {
				return PHPType{Kind: TypeClass, Class: "Model_" + modelName}
			}
		}
		// Foo::factory('name') → Foo_name (Kohana cascading pattern)
		if modelName, ok := firstArgString(n.Args); ok && className != "" {
			return PHPType{Kind: TypeClass, Class: className + "_" + modelName}
		}
	}
	return PHPType{Kind: TypeUnknown}
}

// — Argument helpers —

func firstArgString(args []ast.Vertex) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	expr := phputil.ArgExpr(args[0])
	if expr == nil {
		return "", false
	}
	return phputil.ScalarStringVal(expr)
}
