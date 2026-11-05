package view

import (
	"github.com/VKCOM/php-parser/pkg/ast"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/route"
	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// extractControllerRefs returns the controller/action call sites found in a
// parsed PHP file. References without a controller string are dropped: they
// cannot be attributed to a class (e.g. Route::url with only an 'action').
func extractControllerRefs(path string, astRoot ast.Vertex, src []byte, helpers []config.RouteHelper) []ControllerRef {
	var out []ControllerRef
	for _, r := range route.Extract(path, astRoot, helpers) {
		class := r.ControllerClass()
		if class == "" {
			continue
		}
		cr := ControllerRef{
			Class:           class,
			File:            path,
			Kind:            r.Kind,
			ControllerRange: locRange(r.Controller.Loc, src),
		}
		if r.Action.Present() {
			cr.Action = r.Action.Value
			cr.ActionRange = locRange(r.Action.Loc, src)
		}
		out = append(out, cr)
	}
	return out
}

// locRange converts a byte-offset Location into an LSP range with UTF-16
// columns computed from src.
func locRange(loc phputil.Location, src []byte) protocol.Range {
	if loc.StartLine == 0 {
		return protocol.Range{}
	}
	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(loc.StartLine - 1),
			Character: phputil.UTF16ColFromOffset(src, loc.StartLine, loc.StartByte),
		},
		End: protocol.Position{
			Line:      uint32(loc.EndLine - 1),
			Character: phputil.UTF16ColFromOffset(src, loc.EndLine, loc.EndByte),
		},
	}
}
