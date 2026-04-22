package lsp

import (
	"bytes"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// DocumentSymbol handles textDocument/documentSymbol.
// For view files, returns the view name as a File symbol whose Range spans the
// entire file, with all inferred variables as Variable child symbols (visible
// in Neovim Telescope, VS Code outline, and similar tools).
// Returns nil for non-view PHP files.
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

	// Read the file content to compute a proper spanning range. Falls back to
	// a zero range when the document is not open and cannot be read from disk.
	src, _ := s.docs.Read(p.TextDocument.URI)
	fRange := fullFileRange(src)

	fileKind := protocol.SymbolKindFile
	varKind := protocol.SymbolKindVariable
	var emptyRange protocol.Range

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
			Range:          fRange,
			SelectionRange: fRange,
		}
		if len(children) > 0 {
			sym.Children = children
		}
		syms = append(syms, sym)
	}
	return syms, nil
}

// fullFileRange returns a Range spanning all lines in src.
// End.Line is set to the number of newlines in src (0-based last line index).
// Returns a zero range when src is nil or empty.
func fullFileRange(src []byte) protocol.Range {
	if len(src) == 0 {
		return protocol.Range{}
	}
	lastLine := uint32(bytes.Count(src, []byte{'\n'}))
	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: lastLine, Character: 0},
	}
}
