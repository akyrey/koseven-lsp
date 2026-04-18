package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// DocumentSymbol handles textDocument/documentSymbol.
// For view files, lists the view name as a document symbol so fuzzy-finders
// can navigate to it. Full implementation (listing nested View::factory calls
// inside views) is deferred to a future iteration.
func (s *Server) DocumentSymbol(_ *glsp.Context, p *protocol.DocumentSymbolParams) (any, error) {
	idx := s.viewIndex()
	if idx == nil {
		return nil, nil
	}

	path := URIToPath(p.TextDocument.URI)
	names := idx.NamesForFile(path)
	if len(names) == 0 {
		return nil, nil
	}

	kind := protocol.SymbolKindFile
	syms := make([]protocol.DocumentSymbol, 0, len(names))
	for _, name := range names {
		n := name // copy
		syms = append(syms, protocol.DocumentSymbol{
			Name:  n,
			Kind:  kind,
			Range: protocol.Range{},
			SelectionRange: protocol.Range{},
		})
	}
	return syms, nil
}
