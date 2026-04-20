package lsp

import (
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// WorkspaceSymbol handles workspace/symbol requests. It returns every indexed
// view definition whose name contains the query string (case-insensitive).
// An empty query returns all definitions — useful for fuzzy-finders like
// Telescope that send "" on first open.
func (s *Server) WorkspaceSymbol(_ *glsp.Context, p *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	idx := s.viewIndex()
	if idx == nil {
		return nil, nil
	}

	query := strings.ToLower(p.Query)
	all := idx.AllDefinitions()
	infos := make([]protocol.SymbolInformation, 0, len(all))
	kind := protocol.SymbolKindFile

	for _, def := range all {
		if query != "" && !strings.Contains(strings.ToLower(def.Name), query) {
			continue
		}
		infos = append(infos, protocol.SymbolInformation{
			Name: def.Name,
			Kind: kind,
			Location: protocol.Location{
				URI:   PathToURI(def.Path),
				Range: protocol.Range{},
			},
		})
	}

	return infos, nil
}
