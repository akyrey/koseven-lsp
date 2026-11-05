package lsp

import (
	"strings"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/visitor"
	"github.com/VKCOM/php-parser/pkg/visitor/traverser"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/indexer/route"
	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
)

// controllerClassPrefix is the class-name prefix that marks a Kohana controller.
const controllerClassPrefix = "controller_"

// controllerTarget identifies the controller class, and optionally one of its
// actions, whose call sites should be listed by find-references.
type controllerTarget struct {
	Class  string // e.g. "Controller_Pages"
	Action string // "" means every reference to the controller
}

// findControllerDeclAtOffset reports the controller target when the cursor is
// on a controller class name (class Controller_Pages) or on an action method
// name (function action_about) inside a controller class.
func findControllerDeclAtOffset(src []byte, path string, offset int) (controllerTarget, bool) {
	root, err := phpparse.Bytes(src, path)
	if err != nil || root == nil {
		return controllerTarget{}, false
	}
	f := &controllerDeclFinder{offset: offset}
	traverser.NewTraverser(f).Traverse(root)
	return f.target, f.found
}

type controllerDeclFinder struct {
	visitor.Null
	offset int
	class  string // current enclosing controller class; "" when not a controller
	target controllerTarget
	found  bool
}

func (f *controllerDeclFinder) StmtClass(n *ast.StmtClass) {
	if f.found {
		return
	}
	id, ok := n.Name.(*ast.Identifier)
	if !ok {
		f.class = "" // anonymous class
		return
	}
	name := string(id.Value)
	if !strings.HasPrefix(strings.ToLower(name), controllerClassPrefix) {
		f.class = ""
		return
	}
	f.class = name
	if cursorInNode(f.offset, id) {
		f.target = controllerTarget{Class: name}
		f.found = true
	}
}

func (f *controllerDeclFinder) StmtClassMethod(n *ast.StmtClassMethod) {
	if f.found || f.class == "" {
		return
	}
	id, ok := n.Name.(*ast.Identifier)
	if !ok || !cursorInNode(f.offset, id) {
		return
	}
	if action := route.ActionFromMethod(string(id.Value)); action != "" {
		f.target = controllerTarget{Class: f.class, Action: action}
		f.found = true
	}
}

// cursorInNode reports whether offset falls within [StartPos, EndPos) of node.
func cursorInNode(offset int, node ast.Vertex) bool {
	pos := node.GetPosition()
	return pos != nil && offset >= pos.StartPos && offset < pos.EndPos
}

// controllerRefLocations returns the locations of every call site targeting t.
// With an action, only matching references are returned (case-insensitive,
// like PHP method names) and the location points at the action literal;
// otherwise every reference to the class is returned, pointing at the
// controller literal.
func controllerRefLocations(idx view.Index, t controllerTarget) []protocol.Location {
	var locs []protocol.Location
	for _, r := range idx.ControllerRefs(t.Class) {
		rng := r.ControllerRange
		if t.Action != "" {
			if !strings.EqualFold(r.Action, t.Action) {
				continue
			}
			rng = r.ActionRange
		}
		locs = append(locs, protocol.Location{URI: PathToURI(r.File), Range: rng})
	}
	return locs
}

// routeMatchTarget converts a route string under the cursor into a reference
// target: cursor on the action literal targets that action, otherwise the
// whole controller.
func routeMatchTarget(m routeMatch) (controllerTarget, bool) {
	if m.Controller == "" {
		return controllerTarget{}, false
	}
	t := controllerTarget{Class: route.ControllerClassName(m.Controller, m.Directory)}
	if m.CursorOn == "action" {
		t.Action = m.Action
	}
	return t, true
}
