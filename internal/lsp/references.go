package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// References handles textDocument/references.
//
// Two modes depending on the open file:
//
//  1. Cursor is inside a view file → return every PHP site that constructs
//     that view (View::factory, new View, Kohana::find_file('views', ...)).
//
//  2. Cursor is on a view name string inside a PHP file → same result as
//     hovering over the string in the view file.
func (s *Server) References(_ *glsp.Context, p *protocol.ReferenceParams) ([]protocol.Location, error) {
	idx := s.viewIndex()
	if idx == nil {
		return nil, nil
	}

	path := URIToPath(p.TextDocument.URI)

	// Mode 1: cursor is in a view file.
	if names := idx.NamesForFile(path); len(names) > 0 {
		var locs []protocol.Location
		for _, name := range names {
			for _, u := range idx.UsagesOf(name) {
				locs = append(locs, protocol.Location{
					URI:   PathToURI(u.File),
					Range: u.Range,
				})
			}
		}
		return locs, nil
	}

	// Mode 2: cursor is on a view name string in a PHP file.
	src, err := s.docs.Read(p.TextDocument.URI)
	if err != nil {
		return nil, nil
	}
	offset := positionToByteOffset(src, p.Position)
	viewName := findViewNameAtOffset(src, path, offset)
	if viewName == "" {
		return nil, nil
	}

	var locs []protocol.Location
	for _, u := range idx.UsagesOf(viewName) {
		locs = append(locs, protocol.Location{
			URI:   PathToURI(u.File),
			Range: u.Range,
		})
	}
	return locs, nil
}
