package main

import (
	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/tliron/glsp/server"

	"github.com/akyrey/koseven-lsp/internal/lsp"
)

const lsName = "koseven-lsp"

var version = "0.0.0-dev"

func main() {
	// Log at level 1 (info) to stderr. Editors forward stderr to their
	// developer console or log file.
	commonlog.Configure(1, nil)

	s := lsp.NewServer(commonlog.GetLogger(lsName), version)
	handler := buildHandler(s)
	srv := server.NewServer(handler, lsName, false)
	if err := srv.RunStdio(); err != nil {
		commonlog.GetLogger(lsName).Errorf("%s", err)
	}
}

func buildHandler(s *lsp.Server) *protocol.Handler {
	return &protocol.Handler{
		Initialize:  s.Initialize,
		Initialized: s.Initialized,
		Shutdown:    s.Shutdown,
		SetTrace:    s.SetTrace,

		TextDocumentDidOpen:   s.DidOpen,
		TextDocumentDidChange: s.DidChange,
		TextDocumentDidClose:  s.DidClose,

		TextDocumentDefinition:     s.Definition,
		TextDocumentReferences:     s.References,
		TextDocumentHover:          s.Hover,
		TextDocumentCompletion:     s.Completion,
		TextDocumentDocumentSymbol: s.DocumentSymbol,
		WorkspaceSymbol:            s.WorkspaceSymbol,
		TextDocumentCodeAction:     s.CodeAction,
		TextDocumentPrepareRename:  s.PrepareRename,
		TextDocumentRename:         s.Rename,
		WorkspaceWillRenameFiles:   s.WillRenameFiles,
	}
}
