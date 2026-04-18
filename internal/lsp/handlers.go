package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Definition handles textDocument/definition.
// Returns jump targets when the cursor is on a view name string.
// Full implementation requires the view index (next iteration).
func (s *Server) Definition(_ *glsp.Context, p *protocol.DefinitionParams) (any, error) {
	s.log.Debugf("koseven-lsp: definition at %s:%d:%d (stub)",
		URIToPath(p.TextDocument.URI), p.Position.Line, p.Position.Character)
	return nil, nil
}

// References handles textDocument/references.
// Returns all call sites that construct the view file the cursor is in.
// Full implementation requires the view index (next iteration).
func (s *Server) References(_ *glsp.Context, p *protocol.ReferenceParams) ([]protocol.Location, error) {
	s.log.Debugf("koseven-lsp: references at %s:%d:%d (stub)",
		URIToPath(p.TextDocument.URI), p.Position.Line, p.Position.Character)
	return nil, nil
}

// Hover handles textDocument/hover.
// On a view string: shows resolved file path and module.
// On a bare $var inside a view: shows inferred type and originating call sites.
// Full implementation requires the view index (next iteration).
func (s *Server) Hover(_ *glsp.Context, p *protocol.HoverParams) (*protocol.Hover, error) {
	s.log.Debugf("koseven-lsp: hover at %s:%d:%d (stub)",
		URIToPath(p.TextDocument.URI), p.Position.Line, p.Position.Character)
	return nil, nil
}

// Completion handles textDocument/completion.
// Inside a view file, offers all inferred vars with type as detail.
// Full implementation requires the view index (next iteration).
func (s *Server) Completion(_ *glsp.Context, p *protocol.CompletionParams) (any, error) {
	s.log.Debugf("koseven-lsp: completion at %s:%d:%d (stub)",
		URIToPath(p.TextDocument.URI), p.Position.Line, p.Position.Character)
	return nil, nil
}

// DocumentSymbol handles textDocument/documentSymbol.
// For view files, lists the file itself and any nested View::factory calls.
// Full implementation requires the view index (next iteration).
func (s *Server) DocumentSymbol(_ *glsp.Context, p *protocol.DocumentSymbolParams) (any, error) {
	s.log.Debugf("koseven-lsp: documentSymbol for %s (stub)",
		URIToPath(p.TextDocument.URI))
	return nil, nil
}
