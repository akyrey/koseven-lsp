package lsp

import (
	"fmt"
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
)

// Hover handles textDocument/hover.
//
// Two modes:
//
//  1. Cursor is on a view name string (in any PHP file) → show resolved file
//     path(s) and which module each comes from. Useful for spotting HMVC overrides.
//
//  2. Cursor is on a bare $variable inside a view file → show the inferred PHP
//     type and which call sites exposed it.
func (s *Server) Hover(_ *glsp.Context, p *protocol.HoverParams) (*protocol.Hover, error) {
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

	// Mode 2: $var inside a view file.
	if names := idx.NamesForFile(path); len(names) > 0 {
		varName := findVarAtOffset(src, path, offset)
		if varName != "" {
			for _, ev := range idx.VarsFor(names[0]) {
				if ev.Name == varName {
					return &protocol.Hover{
						Contents: protocol.MarkupContent{
							Kind:  protocol.MarkupKindMarkdown,
							Value: formatVarHover(ev),
						},
					}, nil
				}
			}
		}
	}

	// Mode 1: view name string.
	viewName := findViewNameAtOffset(src, path, offset)
	if viewName == "" {
		return nil, nil
	}
	defs := idx.Resolve(viewName)
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: formatViewHover(viewName, defs),
		},
	}, nil
}

// formatViewHover produces a Markdown hover message for a view name.
func formatViewHover(viewName string, defs []view.ViewDefinition) string {
	if len(defs) == 0 {
		return fmt.Sprintf("`%s` — view not found in cascade", viewName)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**View** `%s`\n\n", viewName)
	for _, d := range defs {
		source := d.ModuleName
		if source == "" {
			switch d.RootKind {
			case view.RootApplication:
				source = "application"
			case view.RootSystem:
				source = "system"
			}
		}
		fmt.Fprintf(&sb, "- `%s` *(cascade order %d, %s)*\n", d.Path, d.RootOrder, source)
	}
	return sb.String()
}

// formatVarHover produces a Markdown hover message for a view variable.
func formatVarHover(ev view.ExposedVar) string {
	typ := formatPHPType(ev.Type)
	source := varSourceLabel(ev.Source)
	return fmt.Sprintf("**`$%s`** `%s`  \n*exposed via %s*", ev.Name, typ, source)
}

func varSourceLabel(s view.VarSource) string {
	switch s {
	case view.SourceFactoryArray:
		return "factory array"
	case view.SourceSet:
		return "->set()"
	case view.SourceBind:
		return "->bind()"
	case view.SourceMagicSet:
		return "$view->prop ="
	case view.SourceSetGlobal:
		return "View::set_global()"
	case view.SourceBindGlobal:
		return "View::bind_global()"
	}
	return "unknown"
}

// formatPHPType returns a human-readable PHP type string.
func formatPHPType(t view.PHPType) string {
	switch t.Kind {
	case view.TypeString:
		return "string"
	case view.TypeInt:
		return "int"
	case view.TypeFloat:
		return "float"
	case view.TypeBool:
		return "bool"
	case view.TypeNull:
		return "null"
	case view.TypeArray:
		if t.Element != nil {
			return formatPHPType(*t.Element) + "[]"
		}
		return "array"
	case view.TypeClass:
		if t.Class != "" {
			return t.Class
		}
		return "object"
	}
	return "mixed"
}

// findVarAtOffset parses src and returns the bare variable name (without $)
// if the cursor falls within an ExprVariable node. Returns "" otherwise.
func findVarAtOffset(src []byte, path string, offset int) string {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return ""
	}
	vf := &varFinder{offset: offset}
	traverser.NewTraverser(vf).Traverse(root)
	return vf.varName
}

type varFinder struct {
	visitor.Null
	offset  int
	varName string
}

func (v *varFinder) ExprVariable(n *ast.ExprVariable) {
	if v.varName != "" {
		return
	}
	id, ok := n.Name.(*ast.Identifier)
	if !ok {
		return
	}
	pos := n.GetPosition()
	if pos == nil || v.offset < pos.StartPos || v.offset >= pos.EndPos {
		return
	}
	name := string(id.Value)
	// id.Value includes the $ for $this; bare variables do not.
	name = strings.TrimPrefix(name, "$")
	v.varName = name
}
