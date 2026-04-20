package view

import (
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/project"
)

// Re-export RootKind constants so callers can use view.RootApplication etc.
// without a separate project import.
const (
	RootApplication = project.RootApplication
	RootModule      = project.RootModule
	RootSystem      = project.RootSystem
)

// RootKind is an alias for the project-level cascade kind.
type RootKind = project.RootKind

// ViewDefinition is a resolved view file on disk.
type ViewDefinition struct {
	Name       string          // logical name, e.g. "pages/about"
	Path       string          // absolute path to views/pages/about.php
	RootKind   project.RootKind
	RootOrder  int    // position in cascade; lower = higher priority
	ModuleName string // "" for application/system roots; module name otherwise
}

// Construct identifies which PHP construct created the ViewUsage.
type Construct int

const (
	ConstructViewFactory Construct = iota // View::factory('x', [...])
	ConstructNewView                      // new View('x', [...])
	ConstructFindFile                     // Kohana::find_file('views', 'x')
)

// ViewUsage is a call site in a PHP file that constructs or references a view.
type ViewUsage struct {
	Name        string         // logical view name
	File        string         // absolute PHP file path containing this call
	Range       protocol.Range // range of the full construction expression
	NameRange   protocol.Range // range of the view name string literal (precise)
	Construct   Construct
	ExposedVars []ExposedVar // vars injected at this call site
}

// VarSource identifies which View API exposed the variable.
type VarSource int

const (
	SourceFactoryArray VarSource = iota // second arg of View::factory / new View
	SourceSet                           // ->set('x', val) or ->set(['x' => val, ...])
	SourceBind                          // ->bind('x', $ref)
	SourceMagicSet                      // $view->x = val
	SourceSetGlobal                     // View::set_global / ->set_global
	SourceBindGlobal                    // View::bind_global / ->bind_global
)

// TypeKind is a coarse type classification used for hover/completion detail.
type TypeKind int

const (
	TypeUnknown TypeKind = iota
	TypeClass
	TypeString
	TypeInt
	TypeFloat
	TypeBool
	TypeArray
	TypeNull
)

// PHPType is a coarse, extensible PHP type representation sufficient for
// view variable hover and completion detail.
type PHPType struct {
	Kind    TypeKind
	Class   string   // FQN when Kind == TypeClass, e.g. "Model_Member"
	Element *PHPType // element type when Kind == TypeArray
}

// ExposedVar is a variable made available inside the view scope at a call site.
type ExposedVar struct {
	Name   string         // variable name without leading $
	Type   PHPType        // inferred; may be TypeUnknown
	Source VarSource      // which API exposed it
	Range  protocol.Range // location of the expression that set it
}

// Index is the primary contract consumed by all three MVP LSP features.
// Implementations must be safe for concurrent reads.
type Index interface {
	// Resolve returns every matching view file for viewName in cascade order
	// (application > modules in load order > system). Returns multiple results
	// when the same name exists in more than one layer (HMVC overrides).
	Resolve(viewName string) []ViewDefinition

	// UsagesOf returns every call site that constructs or references viewName.
	UsagesOf(viewName string) []ViewUsage

	// VarsFor returns the union of variables exposed to viewName across all
	// call sites that construct it, plus any set_global/bind_global sites.
	VarsFor(viewName string) []ExposedVar

	// NamesForFile returns the logical view name(s) that map to an absolute
	// view file path. Usually returns one entry; may return more for symlinked
	// or aliased files.
	NamesForFile(path string) []string

	// AllDefinitions returns all known view definitions for workspace/symbol
	// and document symbol providers.
	AllDefinitions() []ViewDefinition
}
