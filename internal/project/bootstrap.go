package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// ParseModules reads application/bootstrap.php (or the path overridden by cfg)
// and extracts the module name → path mapping from the static Kohana::modules([...])
// call. Returns modules in declaration order.
func ParseModules(root string, cfg config.Config) ([]Module, error) {
	bootstrapPath := filepath.Join(root, cfg.ApplicationPath, "bootstrap.php")
	astRoot, err := phpparse.File(bootstrapPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	bv := &bootstrapVisitor{root: root, cfg: cfg}
	traverser.NewTraverser(bv).Traverse(astRoot)
	return bv.modules, nil
}

type bootstrapVisitor struct {
	visitor.Null
	root    string
	cfg     config.Config
	modules []Module
}

// ExprStaticCall fires on every static method call in the file.
// We look for exactly: Kohana::modules([...])
func (v *bootstrapVisitor) ExprStaticCall(n *ast.ExprStaticCall) {
	if v.modules != nil {
		return // already found it
	}
	className := phputil.NameToString(n.Class)
	if className != "Kohana" && className != "\\Kohana" {
		return
	}
	methodID, ok := n.Call.(*ast.Identifier)
	if !ok || string(methodID.Value) != "modules" {
		return
	}
	if len(n.Args) == 0 {
		v.modules = []Module{}
		return
	}
	arg := phputil.ArgExpr(n.Args[0])
	if arg == nil {
		return
	}
	arr, ok := arg.(*ast.ExprArray)
	if !ok {
		return
	}
	for _, item := range arr.Items {
		elem, ok := item.(*ast.ExprArrayItem)
		if !ok || elem.Key == nil || elem.Val == nil {
			continue
		}
		name := scalarStringValue(elem.Key)
		if name == "" {
			continue
		}
		path := resolveModulePath(elem.Val, v.root, v.cfg)
		if path == "" {
			continue
		}
		v.modules = append(v.modules, Module{Name: name, Path: path})
	}
	if v.modules == nil {
		v.modules = []Module{} // empty array is still a valid result
	}
}

// resolveModulePath resolves a PHP expression to an absolute module directory
// path. Handles the most common patterns:
//
//	MODPATH.'name'           → <root>/<modules_path>/name
//	APPPATH.'name'           → <root>/<application_path>/name
//	SYSPATH.'name'           → <root>/<system_path>/name
//	DOCROOT.'name'           → <root>/name
//	'absolute/path'          → absolute path as-is
//	'relative/path'          → joined with root
func resolveModulePath(node ast.Vertex, root string, cfg config.Config) string {
	switch n := node.(type) {
	case *ast.ScalarString:
		s, _ := phputil.ScalarStringVal(n)
		if filepath.IsAbs(s) {
			return s
		}
		return filepath.Join(root, s)

	case *ast.ExprBinaryConcat:
		prefix := resolveConstBase(n.Left, root, cfg)
		if prefix == "" {
			return ""
		}
		// Right side may be another concat (e.g. MODPATH.'name'.DIRECTORY_SEPARATOR)
		// We strip any trailing DIRECTORY_SEPARATOR constant and only take the suffix.
		suffix := concatRightSuffix(n.Right)
		if suffix == "" {
			return ""
		}
		return filepath.Join(prefix, suffix)
	}
	return ""
}

// resolveConstBase maps known Kohana path constants to their absolute equivalents.
func resolveConstBase(node ast.Vertex, root string, cfg config.Config) string {
	fetch, ok := node.(*ast.ExprConstFetch)
	if !ok {
		return ""
	}
	switch phputil.NameToString(fetch.Const) {
	case "MODPATH":
		return filepath.Join(root, cfg.ModulesPath)
	case "APPPATH":
		return filepath.Join(root, cfg.ApplicationPath)
	case "SYSPATH":
		return filepath.Join(root, cfg.SystemPath)
	case "DOCROOT":
		return root
	}
	return ""
}

// concatRightSuffix extracts the string portion of a right-hand concat operand.
// For MODPATH.'blog'.DIRECTORY_SEPARATOR it returns "blog" (ignoring the
// DIRECTORY_SEPARATOR constant at the end of the chain).
func concatRightSuffix(node ast.Vertex) string {
	switch n := node.(type) {
	case *ast.ScalarString:
		s, _ := phputil.ScalarStringVal(n)
		return strings.TrimRight(s, "/\\")
	case *ast.ExprBinaryConcat:
		// Strip trailing constant (e.g. DIRECTORY_SEPARATOR)
		if _, ok := n.Right.(*ast.ExprConstFetch); ok {
			return concatRightSuffix(n.Left)
		}
		return ""
	}
	return ""
}

// scalarStringValue returns the unquoted value of a ScalarString node, or "".
func scalarStringValue(node ast.Vertex) string {
	s, ok := phputil.ScalarStringVal(node)
	if !ok {
		return ""
	}
	return s
}
