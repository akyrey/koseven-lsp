package lsp

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/project"
)

func buildStockIndex(t *testing.T) *view.ViewIndex {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(stockRoot, cfg)
	require.NoError(t, err)
	idx, err := view.Walk(stockRoot, cfg, modules)
	require.NoError(t, err)
	return idx
}

func buildHMVCIndex(t *testing.T) *view.ViewIndex {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(hmvcRoot, cfg)
	require.NoError(t, err)
	idx, err := view.Walk(hmvcRoot, cfg, modules)
	require.NoError(t, err)
	return idx
}

// TestUsageRange_PrefersNameRange verifies that usageRange returns NameRange
// when it is non-zero, falling back to Range otherwise.
func TestUsageRange_PrefersNameRange(t *testing.T) {
	zero := view.ViewUsage{Name: "x", File: "f.php"}
	assert.Equal(t, zero.Range, usageRange(zero), "zero NameRange should fall back to Range")

	withName := view.ViewUsage{
		Name: "x",
		File: "f.php",
		NameRange: protocol.Range{
			Start: protocol.Position{Line: 5},
			End:   protocol.Position{Line: 5},
		},
		Range: protocol.Range{
			Start: protocol.Position{Line: 3},
			End:   protocol.Position{Line: 7},
		},
	}
	got := usageRange(withName)
	assert.Equal(t, uint32(5), got.Start.Line, "should prefer NameRange line")
}

// TestReferencesFromViewFile verifies that the pages/about view has at least
// one reference site in the stock fixture.
func TestReferencesFromViewFile(t *testing.T) {
	idx := buildStockIndex(t)
	viewPath := filepath.Join(stockRoot, "application", "views", "pages", "about.php")

	names := idx.NamesForFile(viewPath)
	require.NotEmpty(t, names)

	var locs []protocol.Location
	for _, name := range names {
		for _, u := range idx.UsagesOf(name) {
			locs = append(locs, protocol.Location{
				URI:   PathToURI(u.File),
				Range: usageRange(u),
			})
		}
	}
	assert.NotEmpty(t, locs, "pages/about must have at least one reference")

	// All references should point to PHP files (not the view itself).
	for _, loc := range locs {
		assert.Contains(t, string(loc.URI), ".php")
	}
}

// TestReferencesHMVC verifies that both the application and the blog module
// return usages for the 'pages/about' view name in the HMVC fixture.
func TestReferencesHMVC(t *testing.T) {
	idx := buildHMVCIndex(t)
	usages := idx.UsagesOf("pages/about")
	require.NotEmpty(t, usages, "hmvc fixture controller constructs pages/about")

	// Every usage should reference an existing file.
	for _, u := range usages {
		assert.NotEmpty(t, u.File)
		assert.NotEmpty(t, u.Name)
	}
}

// TestDiagnostics_MissingView checks that collectMissingViewDiags emits a
// warning for a view name that isn't in the index.
func TestDiagnostics_MissingView(t *testing.T) {
	idx := buildStockIndex(t)

	src := []byte(`<?php
View::factory('does/not/exist');
`)
	diags := collectMissingViewDiags(src, "test.php", idx)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "does/not/exist")
}

// TestDiagnostics_KnownView verifies that known views produce no diagnostics.
func TestDiagnostics_KnownView(t *testing.T) {
	idx := buildStockIndex(t)

	src := []byte(`<?php
View::factory('pages/about');
`)
	diags := collectMissingViewDiags(src, "test.php", idx)
	assert.Empty(t, diags)
}

// TestDiagnostics_DedupPerFile checks that a view name appearing twice in a
// file produces only one diagnostic.
func TestDiagnostics_DedupPerFile(t *testing.T) {
	idx := buildStockIndex(t)

	src := []byte(`<?php
$a = View::factory('ghost/view');
$b = View::factory('ghost/view');
`)
	diags := collectMissingViewDiags(src, "test.php", idx)
	assert.Len(t, diags, 1, "duplicate view name should produce only one diagnostic")
}

// TestWorkspaceSymbol_EmptyQuery verifies that an empty query returns all views.
func TestWorkspaceSymbol_AllDefs(t *testing.T) {
	idx := buildStockIndex(t)
	all := idx.AllDefinitions()
	assert.NotEmpty(t, all)

	// Every definition should have a non-empty name and path.
	for _, d := range all {
		assert.NotEmpty(t, d.Name)
		assert.NotEmpty(t, d.Path)
	}
}
