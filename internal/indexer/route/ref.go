// Package route extracts controller/action references from PHP source files.
//
// A reference is any call site that names a controller (and optionally an
// action and HMVC directory) as string literals:
//
//	Route::set('x', 'pat')->defaults(['controller' => 'pages', 'action' => 'about'])
//	Route::url('x', ['controller' => 'pages', 'action' => 'about'])
//	Route::get('x')->uri(['controller' => 'pages', 'action' => 'about'])
//	Request::factory()->controller('pages')->action('about')
//	Custom helpers configured via config.RouteHelpers
//
// The package is pure: it takes an AST and returns values. It is used both by
// the indexer (find-references from a controller action) and by the LSP layer
// (go-to-definition from a route string under the cursor).
package route

import (
	"strings"

	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// Kind identifies which construct produced a Ref.
type Kind int

const (
	KindRouteDefaults Kind = iota // Route::set(...)->defaults([...])
	KindRouteURL                  // Route::url('name', [...])
	KindRouteURI                  // Route::get('name')->uri([...])
	KindRequest                   // Request::factory()->controller()->action()
	KindHelper                    // configured route helper
)

// Field is one string literal argument of a reference (controller, action or
// directory). Loc is zero when the field is absent or not a string literal.
type Field struct {
	Value string
	Loc   phputil.Location
}

// Present reports whether the field holds a usable string literal value.
func (f Field) Present() bool { return f.Value != "" }

// Contains reports whether the byte offset falls inside the field's literal.
func (f Field) Contains(offset int) bool {
	if f.Loc.Zero() {
		return false
	}
	return offset >= f.Loc.StartByte && offset < f.Loc.EndByte
}

// Ref is a single call site that references a controller and/or action.
type Ref struct {
	Kind       Kind
	Controller Field
	Action     Field
	Directory  Field
}

// ControllerClass returns the Kohana class name this reference targets, e.g.
// "Controller_Admin_Users". Returns "" when the controller is unknown.
func (r Ref) ControllerClass() string {
	if !r.Controller.Present() {
		return ""
	}
	return ControllerClassName(r.Controller.Value, r.Directory.Value)
}

// ControllerClassName constructs the Kohana PSR-0 class name for a controller,
// optionally prefixed by an HMVC directory segment.
// "pages","" → "Controller_Pages", "users","admin" → "Controller_Admin_Users".
func ControllerClassName(controller, directory string) string {
	ctrl := TitleCaseSegments(controller)
	if directory == "" {
		return "Controller_" + ctrl
	}
	return "Controller_" + TitleCaseSegments(directory) + "_" + ctrl
}

// TitleCaseSegments title-cases each underscore-separated segment.
// "pages" → "Pages", "auth_user" → "Auth_User".
func TitleCaseSegments(s string) string {
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

// ClassKey normalises a controller class name for map lookups. PHP class names
// are case-insensitive, and route strings are often lower-case ("pages") while
// class declarations are title-cased ("Controller_Pages").
func ClassKey(className string) string {
	return strings.ToLower(strings.TrimPrefix(className, "\\"))
}

// ActionMethodPrefix is the prefix Kohana prepends to action names to form the
// controller method name: action "about" → method "action_about".
const ActionMethodPrefix = "action_"

// ActionFromMethod returns the action name for a controller method name, or
// "" when the method is not an action (no "action_" prefix).
func ActionFromMethod(method string) string {
	if len(method) <= len(ActionMethodPrefix) || !strings.EqualFold(method[:len(ActionMethodPrefix)], ActionMethodPrefix) {
		return ""
	}
	return method[len(ActionMethodPrefix):]
}
