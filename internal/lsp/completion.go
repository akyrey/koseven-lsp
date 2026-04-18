package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Completion handles textDocument/completion.
// Inside a view file, offers every variable exposed to that view as a
// completion item, with the inferred PHP type shown as the detail string.
// Returns nil when the cursor is not inside a known view file.
func (s *Server) Completion(_ *glsp.Context, p *protocol.CompletionParams) (any, error) {
	idx := s.viewIndex()
	if idx == nil {
		return nil, nil
	}

	path := URIToPath(p.TextDocument.URI)
	names := idx.NamesForFile(path)
	if len(names) == 0 {
		return nil, nil
	}

	vars := idx.VarsFor(names[0])
	if len(vars) == 0 {
		return nil, nil
	}

	kind := protocol.CompletionItemKindVariable
	items := make([]protocol.CompletionItem, 0, len(vars))
	for _, v := range vars {
		label := "$" + v.Name
		detail := formatPHPType(v.Type)
		items = append(items, protocol.CompletionItem{
			Label:  label,
			Kind:   &kind,
			Detail: &detail,
		})
	}
	return items, nil
}
