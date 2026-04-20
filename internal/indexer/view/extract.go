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
	ev := newExtractVisitor(path)
	traverser.NewTraverser(ev).Traverse(astRoot)
	return ev.usages, ev.globals
}

// extractVisitor is a single-pass AST visitor that collects view construction
// call sites and their chained and split-assignment variable exposure calls.
//
// # Inline chain deduplication
//
// DFS pre-order visits the outermost ExprMethodCall first. tryExtractChain is
// called on every ExprStaticCall, ExprNew, and ExprMethodCall. The first
// (outermost) call that resolves to a view construction registers the full chain
// in `seen` (keyed on the base node's StartPos); subsequent inner nodes skip.
//
// # Split-assignment scope tracking
//
// When the visitor encounters ExprAssign with a view construction on the RHS, it:
//  1. Records the ViewUsage immediately (marking basePos as seen to prevent
//     double-recording by the later ExprStaticCall/ExprNew visitors).
//  2. Stores the usage index in `scope` keyed by the LHS variable name.
//
// Subsequent ExprMethodCall nodes whose receiver is that variable look up the
// scope and append any exposed vars directly to the stored usage.
//
// Scope is cleared on entry to any function/method/closure boundary so that
// variables from one method do not bleed into another.
type extractVisitor struct {
	visitor.Null
	path    string
	seen    map[int]struct{}   // base StartPos → already recorded
	scope   map[string]int     // variable name → index in v.usages
	usages  []ViewUsage
	globals []ExposedVar
}

func newExtractVisitor(path string) *extractVisitor {
	return &extractVisitor{
		path:  path,
		seen:  make(map[int]struct{}),
		scope: make(map[string]int),
	}
}

// ─── Scope boundaries ─────────────────────────────────────────────────────────

func (v *extractVisitor) StmtClassMethod(_ *ast.StmtClassMethod) { v.scope = make(map[string]int) }
func (v *extractVisitor) StmtFunction(_ *ast.StmtFunction)       { v.scope = make(map[string]int) }
func (v *extractVisitor) ExprClosure(_ *ast.ExprClosure)         { v.scope = make(map[string]int) }
func (v *extractVisitor) ExprArrowFunction(_ *ast.ExprArrowFunction) {
	v.scope = make(map[string]int)
}

// ─── Primary visitors ────────────────────────────────────────────────────────

func (v *extractVisitor) ExprStaticCall(n *ast.ExprStaticCall) {
	v.tryGlobalStatic(n)
	v.record(n)
}

func (v *extractVisitor) ExprNew(n *ast.ExprNew) {
	v.record(n)
}

// ExprAssign handles split-assignment view construction:
//
//	$view = View::factory('pages/about');
//	$view = View::factory('pages/about')->set('x', $val);
//
// The usage is recorded immediately (marking seen) and the scope entry is set
// so subsequent ExprMethodCall visitors can append vars.
func (v *extractVisitor) ExprAssign(n *ast.ExprAssign) {
	varName := exprVariableName(n.Var)
	if varName == "" {
		return
	}

	viewName, construct, vars, basePos, nameRange, found := tryExtractChain(n.Expr)
	if !found {
		// Variable reassigned to a non-view — invalidate its scope entry.
		delete(v.scope, varName)
		return
	}
	if _, already := v.seen[basePos]; already {
		// Rare: already recorded by a child visitor (shouldn't happen in pre-order).
		return
	}
	v.seen[basePos] = struct{}{}

	pos := n.Expr.GetPosition()
	var r protocol.Range
	if pos != nil && pos.StartLine > 0 {
		r = protocol.Range{
			Start: protocol.Position{Line: uint32(pos.StartLine - 1)},
			End:   protocol.Position{Line: uint32(pos.EndLine - 1)},
		}
	}

	idx := len(v.usages)
	v.usages = append(v.usages, ViewUsage{
		Name:        viewName,
		File:        v.path,
		Range:       r,
		NameRange:   nameRange,
		Construct:   construct,
		ExposedVars: vars,
	})
	v.scope[varName] = idx
}

// ExprMethodCall handles both inline chains and scope-variable method calls.
func (v *extractVisitor) ExprMethodCall(n *ast.ExprMethodCall) {
	v.record(n)
	v.tryScopeAttribution(n)
}

// ─── Inline chain recording ───────────────────────────────────────────────────

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

// ─── Scope-variable attribution ───────────────────────────────────────────────

// tryScopeAttribution appends exposed vars to a previously-scope-tracked usage
// when the method call's direct receiver is a known view variable:
//
//	$view->set('key', $val);   ← handled
//	$view->set('a')->set('b'); ← only 'a' is captured (direct receiver only)
func (v *extractVisitor) tryScopeAttribution(n *ast.ExprMethodCall) {
	varName := exprVariableName(n.Var)
	if varName == "" {
		return
	}
	usageIdx, tracked := v.scope[varName]
	if !tracked {
		return
	}
	chainVars := extractChainMethodVars(n)
	if len(chainVars) > 0 {
		v.usages[usageIdx].ExposedVars = append(v.usages[usageIdx].ExposedVars, chainVars...)
	}
}

// ─── Global static var exposure ───────────────────────────────────────────────

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

// ─── Variable name helper ─────────────────────────────────────────────────────

// exprVariableName extracts the variable name from an ExprVariable node,
// normalised to always include the $ prefix. Returns "" for dynamic variables
// ($$foo) and non-variable expressions.
func exprVariableName(node ast.Vertex) string {
	ev, ok := node.(*ast.ExprVariable)
	if !ok {
		return ""
	}
	id, ok := ev.Name.(*ast.Identifier)
	if !ok {
		return "" // dynamic variable ($$foo) — skip
	}
	name := string(id.Value)
	if !strings.HasPrefix(name, "$") {
		name = "$" + name
	}
	return name
}

// ─── Chain extraction ─────────────────────────────────────────────────────────

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

// ─── View construction matchers ───────────────────────────────────────────────

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

// ─── Variable extraction from chain method calls ──────────────────────────────

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
	if key, ok := phputil.ScalarStringVal(expr); ok {
		var typ PHPType
		if len(args) >= 2 {
			typ = inferType(phputil.ArgExpr(args[1]))
		}
		return []ExposedVar{{Name: key, Source: SourceSet, Type: typ}}
	}
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

// ─── Type inference ───────────────────────────────────────────────────────────

// inferType performs lightweight type inference on a PHP expression node.
// Handles literals, true/false/null constants, new Foo(), ORM::factory('X'),
// and Foo::factory('X') → Foo_X. Everything else is TypeUnknown.
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
		case "TRUE", "FALSE":
			return PHPType{Kind: TypeBool}
		case "NULL":
			return PHPType{Kind: TypeNull}
		}
	case *ast.ExprNew:
		if className := phputil.NameToString(n.Class); className != "" {
			return PHPType{Kind: TypeClass, Class: className}
		}
	case *ast.ExprStaticCall:
		className := phputil.NameToString(n.Class)
		methodID, ok := n.Call.(*ast.Identifier)
		if !ok || string(methodID.Value) != "factory" {
			break
		}
		if className == "ORM" || className == "Model" {
			if modelName, ok := firstArgString(n.Args); ok {
				return PHPType{Kind: TypeClass, Class: "Model_" + modelName}
			}
		}
		if modelName, ok := firstArgString(n.Args); ok && className != "" {
			return PHPType{Kind: TypeClass, Class: className + "_" + modelName}
		}
	}
	return PHPType{Kind: TypeUnknown}
}

// ─── Argument helpers ─────────────────────────────────────────────────────────

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
