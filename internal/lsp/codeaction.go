package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// CodeAction handles textDocument/codeAction.
//
// When the cursor is on a view name string literal (inside View::factory,
// new View, or Kohana::find_file('views',...)), a single "Rename view '...'"
// action is returned. Its command triggers the editor's built-in rename
// workflow, which calls textDocument/prepareRename then textDocument/rename.
// The rename handler updates every string literal reference AND renames the
// physical view file(s) in the cascade in one atomic workspace edit.
func (s *Server) CodeAction(_ *glsp.Context, p *protocol.CodeActionParams) (any, error) {
	src, err := s.docs.Read(p.TextDocument.URI)
	if err != nil {
		return nil, nil
	}
	path := URIToPath(p.TextDocument.URI)
	offset := positionToByteOffset(src, p.Range.Start)

	viewName, _, found := findViewNameLocAtOffset(src, path, offset)
	if !found {
		return nil, nil
	}

	kind := protocol.CodeActionKindRefactorRewrite
	return []protocol.CodeAction{
		{
			Title: "Rename view '" + viewName + "'...",
			Kind:  &kind,
			Command: &protocol.Command{
				Title:   "Rename view",
				Command: "editor.action.rename",
			},
		},
	}, nil
}
