package main

import (
	"os"

	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/tliron/glsp/server"

	"github.com/akyrey/koseven-lsp/internal/logging"
	"github.com/akyrey/koseven-lsp/internal/lsp"
)

const lsName = "koseven-lsp"

var version = "0.0.0-dev"

func main() {
	// Log to stderr, warnings and errors only unless KOSEVEN_LSP_LOG_VERBOSITY
	// says otherwise; see internal/logging.
	commonlog.Configure(logging.Verbosity(os.Getenv), nil)

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
