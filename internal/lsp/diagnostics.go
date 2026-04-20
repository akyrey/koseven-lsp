package lsp

import (
	"fmt"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
)

const diagSource = "koseven-lsp"

// publishViewDiagnostics pushes a textDocument/publishDiagnostics notification.
// When cfg.Diagnostics.MissingViews is false (the default) it always sends an
// empty list, clearing any stale diagnostics left from a previous session.
// When enabled, it emits a Warning for every view name that resolves to no
// file in the cascade.
func publishViewDiagnostics(ctx *glsp.Context, uri protocol.DocumentUri, src []byte, path string, idx view.Index, cfg config.Config) {
	var diags []protocol.Diagnostic
	if cfg.Diagnostics.MissingViews && idx != nil && len(src) > 0 {
		diags = collectMissingViewDiags(src, path, idx)
	}
	ctx.Notify(
		string(protocol.ServerTextDocumentPublishDiagnostics),
		protocol.PublishDiagnosticsParams{URI: uri, Diagnostics: diags},
	)
}

// collectMissingViewDiags extracts every view construction call in src and
// returns a Warning diagnostic for each view name that idx cannot resolve to
// any file in the cascade.
func collectMissingViewDiags(src []byte, path string, idx view.Index) []protocol.Diagnostic {
	astRoot, err := phpparse.Bytes(src, path)
	if err != nil || astRoot == nil {
		return nil
	}

	usages, _ := view.ExtractFileUsages(path, astRoot)

	sev := protocol.DiagnosticSeverityWarning
	source := diagSource

	var diags []protocol.Diagnostic
	seen := make(map[string]struct{})
	for _, u := range usages {
		if _, already := seen[u.Name]; already {
			continue // one diagnostic per unique view name per file
		}
		if len(idx.Resolve(u.Name)) > 0 {
			continue
		}
		seen[u.Name] = struct{}{}
		diags = append(diags, protocol.Diagnostic{
			Range:    usageRange(u),
			Severity: &sev,
			Source:   &source,
			Message:  fmt.Sprintf("view %q not found in cascade", u.Name),
		})
	}
	return diags
}
