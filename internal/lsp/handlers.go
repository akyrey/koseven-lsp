package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// DocumentSymbol handles textDocument/documentSymbol.
// For view files, returns the view name as a File symbol with all inferred
// variables as Variable child symbols (visible in Neovim Telescope, VS Code
// outline, and similar tools). Returns nil for non-view PHP files.
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

	fileKind := protocol.SymbolKindFile
	varKind := protocol.SymbolKindVariable
	emptyRange := protocol.Range{}

	syms := make([]protocol.DocumentSymbol, 0, len(names))
	for _, name := range names {
		vars := idx.VarsFor(name)
		children := make([]protocol.DocumentSymbol, 0, len(vars))
		for _, v := range vars {
			detail := formatPHPType(v.Type)
			children = append(children, protocol.DocumentSymbol{
				Name:           "$" + v.Name,
				Kind:           varKind,
				Detail:         &detail,
				Range:          emptyRange,
				SelectionRange: emptyRange,
			})
		}
		sym := protocol.DocumentSymbol{
			Name:           name,
			Kind:           fileKind,
			Range:          emptyRange,
			SelectionRange: emptyRange,
		}
		if len(children) > 0 {
			sym.Children = children
		}
		syms = append(syms, sym)
	}
	return syms, nil
}
