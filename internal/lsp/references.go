package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/indexer/view"
)

// usageRange returns the best available range for a view usage location.
// Prefers NameRange (the string literal) over the full expression Range.
func usageRange(u view.ViewUsage) protocol.Range {
	zero := protocol.Range{}
	if u.NameRange != zero {
		return u.NameRange
	}
	return u.Range
}

// References handles textDocument/references.
//
// Modes, tried in order (cursor-precise modes before whole-file fallbacks):
//
//  1. Cursor is on a view name string → every PHP site that constructs that
//     view (View::factory, new View, Kohana::find_file('views', ...)).
//
//  2. Cursor is on a controller/action string in a route call site
//     (Route defaults, Route::url, Route::get()->uri(), Request chain, route
//     helper) → every call site targeting the same controller, or the same
//     action when the cursor is on the action string.
//
//  3. Cursor is anywhere else inside a view file → every PHP site that
//     constructs that view.
//
//  4. Cursor is on a controller class name or an action_<name> method name
//     in a controller file → every call site targeting that controller/action.
func (s *Server) References(_ *glsp.Context, p *protocol.ReferenceParams) ([]protocol.Location, error) {
	idx := s.viewIndex()
	if idx == nil {
		return nil, nil
	}

	path := URIToPath(p.TextDocument.URI)
	src, err := s.docs.Read(p.TextDocument.URI)
	if err != nil {
		return nil, nil
	}
	offset := positionToByteOffset(src, p.Position)

	// Mode 1: cursor is on a view name string.
	if viewName := findViewNameAtOffset(src, path, offset); viewName != "" {
		return viewUsageLocations(idx, viewName), nil
	}

	// Mode 2: cursor is on a controller/action string in a route call site.
	_, cfg, _ := s.cascadeState()
	if m, ok := findRouteAtOffset(src, path, offset, cfg.RouteHelpers); ok {
		if t, ok := routeMatchTarget(m); ok {
			return controllerRefLocations(idx, t), nil
		}
	}

	// Mode 3: cursor is elsewhere in a view file.
	if names := idx.NamesForFile(path); len(names) > 0 {
		var locs []protocol.Location
		for _, name := range names {
			locs = append(locs, viewUsageLocations(idx, name)...)
		}
		return locs, nil
	}

	// Mode 4: cursor is on a controller class or action method declaration.
	if t, ok := findControllerDeclAtOffset(src, path, offset); ok {
		return controllerRefLocations(idx, t), nil
	}
	return nil, nil
}

// viewUsageLocations returns the location of every call site constructing viewName.
func viewUsageLocations(idx view.Index, viewName string) []protocol.Location {
	usages := idx.UsagesOf(viewName)
	locs := make([]protocol.Location, 0, len(usages))
	for _, u := range usages {
		locs = append(locs, protocol.Location{
			URI:   PathToURI(u.File),
			Range: usageRange(u),
		})
	}
	return locs
}
