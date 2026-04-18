package phputil

import (
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
)

// ClassName extracts the string name from an ast.Identifier or ast.Name node.
func ClassName(node ast.Vertex) string {
	switch n := node.(type) {
	case *ast.Identifier:
		return string(n.Value)
	case *ast.Name:
		return joinNameParts(n.Parts)
	case *ast.NameFullyQualified:
		return joinNameParts(n.Parts)
	case *ast.NameRelative:
		return joinNameParts(n.Parts)
	}
	return ""
}

// NameToString converts an ast.Name (or variants) to a string using backslash
// as separator.
func NameToString(node ast.Vertex) string {
	switch n := node.(type) {
	case *ast.Name:
		return joinNameParts(n.Parts)
	case *ast.NameFullyQualified:
		return "\\" + joinNameParts(n.Parts)
	case *ast.NameRelative:
		return joinNameParts(n.Parts)
	case *ast.Identifier:
		return string(n.Value)
	}
	return ""
}

// AddUsesToContext adds use-declaration items into fc.Uses.
// prefix is prepended for group uses (StmtGroupUseList); pass "" for regular uses.
func AddUsesToContext(fc *FileContext, uses []ast.Vertex, prefix string) {
	for _, item := range uses {
		u, ok := item.(*ast.StmtUse)
		if !ok {
			continue
		}
		name := NameToString(u.Use)
		if name == "" {
			continue
		}
		fqn := name
		if prefix != "" {
			fqn = prefix + "\\" + name
		}
		var alias string
		if u.Alias != nil {
			alias = NameToString(u.Alias)
		} else {
			parts := strings.Split(name, "\\")
			alias = parts[len(parts)-1]
		}
		fc.Uses[alias] = FQN(fqn)
	}
}

// ClassNodeFQN returns the fully-qualified name for a class or interface
// declaration node, using fc for namespace context.
func ClassNodeFQN(name ast.Vertex, fc *FileContext) FQN {
	if name == nil {
		return ""
	}
	id, ok := name.(*ast.Identifier)
	if !ok {
		return ""
	}
	short := string(id.Value)
	if fc.Namespace == "" {
		return FQN(short)
	}
	return FQN(string(fc.Namespace) + "\\" + short)
}

// ArgExpr returns the Expr field of an *ast.Argument node, or nil when the
// vertex is not an Argument.
func ArgExpr(v ast.Vertex) ast.Vertex {
	a, ok := v.(*ast.Argument)
	if !ok {
		return nil
	}
	return a.Expr
}

// ScalarStringVal returns the unquoted content of a *ast.ScalarString node.
// Single and double quoted strings are supported; heredoc/nowdoc return
// ("", false). The ok return mirrors strconv conventions.
func ScalarStringVal(node ast.Vertex) (string, bool) {
	s, ok := node.(*ast.ScalarString)
	if !ok {
		return "", false
	}
	b := s.Value
	if len(b) < 2 {
		return string(b), true
	}
	if b[0] == '\'' || b[0] == '"' {
		return string(b[1 : len(b)-1]), true
	}
	return "", false // heredoc / nowdoc
}

func joinNameParts(parts []ast.Vertex) string {
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteByte('\\')
		}
		if id, ok := part.(*ast.NamePart); ok {
			b.Write(id.Value)
		}
	}
	return b.String()
}
