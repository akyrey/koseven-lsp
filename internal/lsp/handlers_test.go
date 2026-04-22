package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// buildStockServer creates a Server with the stock fixture index pre-loaded.
func buildStockServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(stockRoot, cfg)
	require.NoError(t, err)
	idx, err := view.Walk(stockRoot, cfg, modules)
	require.NoError(t, err)
	return &Server{
		cfg:     cfg,
		modules: modules,
		root:    stockRoot,
		viewIdx: idx,
		docs:    newDocumentStore(),
	}
}

// ─── DocumentSymbol ───────────────────────────────────────────────────────────

func TestDocumentSymbol_ViewFile_ReturnsSymbol(t *testing.T) {
	s := buildStockServer(t)
	viewPath := filepath.Join(stockRoot, "application", "views", "pages", "about.php")
	uri := PathToURI(viewPath)

	result, err := s.DocumentSymbol(nil, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	syms, ok := result.([]protocol.DocumentSymbol)
	require.True(t, ok)
	require.NotEmpty(t, syms)

	assert.Equal(t, "pages/about", syms[0].Name)
	assert.Equal(t, protocol.SymbolKindFile, syms[0].Kind)
}

func TestDocumentSymbol_ViewFile_HasVariableChildren(t *testing.T) {
	s := buildStockServer(t)
	viewPath := filepath.Join(stockRoot, "application", "views", "pages", "about.php")
	uri := PathToURI(viewPath)

	result, err := s.DocumentSymbol(nil, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	require.NoError(t, err)
	syms := result.([]protocol.DocumentSymbol)
	require.NotEmpty(t, syms)
	require.NotEmpty(t, syms[0].Children, "view symbol must have variable children")

	childNames := make([]string, len(syms[0].Children))
	for i, c := range syms[0].Children {
		assert.Equal(t, protocol.SymbolKindVariable, c.Kind)
		childNames[i] = c.Name
	}
	assert.Contains(t, childNames, "$user")
	assert.Contains(t, childNames, "$show_contact")
	assert.Contains(t, childNames, "$email")
}

func TestDocumentSymbol_ViewFile_RangeSpansWholeFile(t *testing.T) {
	s := buildStockServer(t)
	viewPath := filepath.Join(stockRoot, "application", "views", "pages", "about.php")
	src, err := os.ReadFile(viewPath)
	require.NoError(t, err)
	uri := PathToURI(viewPath)
	// Simulate the file being open in the editor.
	s.docs.Set(uri, src)

	result, err := s.DocumentSymbol(nil, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	require.NoError(t, err)
	syms := result.([]protocol.DocumentSymbol)
	require.NotEmpty(t, syms)

	assert.Equal(t, uint32(0), syms[0].Range.Start.Line)
	assert.Greater(t, syms[0].Range.End.Line, uint32(0),
		"range end must be past line 0 for a multi-line file")
}

func TestDocumentSymbol_NonViewFile_ReturnsNil(t *testing.T) {
	s := buildStockServer(t)
	controllerPath := filepath.Join(stockRoot, "application", "classes", "Controller", "Pages.php")
	uri := PathToURI(controllerPath)

	result, err := s.DocumentSymbol(nil, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestDocumentSymbol_NoIndex_ReturnsNil(t *testing.T) {
	s := &Server{docs: newDocumentStore()}
	result, err := s.DocumentSymbol(nil, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///any.php"},
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

// ─── fullFileRange ────────────────────────────────────────────────────────────

func TestFullFileRange_Empty(t *testing.T) {
	r := fullFileRange(nil)
	assert.Equal(t, protocol.Range{}, r)
}

func TestFullFileRange_SingleLine(t *testing.T) {
	r := fullFileRange([]byte("<?php echo 1;"))
	assert.Equal(t, uint32(0), r.Start.Line)
	assert.Equal(t, uint32(0), r.End.Line)
}

func TestFullFileRange_MultiLine(t *testing.T) {
	src := []byte("line1\nline2\nline3\n")
	r := fullFileRange(src)
	assert.Equal(t, uint32(0), r.Start.Line)
	assert.Equal(t, uint32(3), r.End.Line) // 3 newlines → last line index 3
}
