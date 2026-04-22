package lsp

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/phputil"
)

// ─── findViewNameLocAtOffset ─────────────────────────────────────────────────

func TestFindViewNameLocAtOffset_ReturnsLocation(t *testing.T) {
	src := []byte("<?php View::factory('pages/about'); ")
	needle := "'pages/about'"
	start := bytes.Index(src, []byte(needle))
	require.Greater(t, start, 0)

	name, loc, found := findViewNameLocAtOffset(src, "test.php", start+3)
	assert.True(t, found)
	assert.Equal(t, "pages/about", name)
	assert.False(t, loc.Zero(), "location must be populated")
	assert.Greater(t, loc.StartByte, 0)
	assert.Equal(t, loc.Path, "test.php")
}

func TestFindViewNameLocAtOffset_NotOnString(t *testing.T) {
	src := []byte("<?php View::factory('pages/about'); ")
	_, _, found := findViewNameLocAtOffset(src, "test.php", 0)
	assert.False(t, found)
}

func TestFindViewNameLocAtOffset_FindFile(t *testing.T) {
	src := []byte("<?php Kohana::find_file('views', 'pages/about'); ")
	needle := "'pages/about'"
	start := bytes.Index(src, []byte(needle))
	require.Greater(t, start, 0)

	name, _, found := findViewNameLocAtOffset(src, "test.php", start+3)
	assert.True(t, found)
	assert.Equal(t, "pages/about", name)
}

func TestFindViewNameLocAtOffset_NewView(t *testing.T) {
	src := []byte("<?php new View('pages/about', []); ")
	needle := "'pages/about'"
	start := bytes.Index(src, []byte(needle))
	require.Greater(t, start, 0)

	name, _, found := findViewNameLocAtOffset(src, "test.php", start+3)
	assert.True(t, found)
	assert.Equal(t, "pages/about", name)
}

// ─── collectViewRenameEdits ───────────────────────────────────────────────────

func TestCollectViewRenameEdits_SingleOccurrence(t *testing.T) {
	src := []byte("<?php View::factory('pages/about'); ")
	edits := collectViewRenameEdits(src, "test.php", "pages/about", "pages/home")
	require.Len(t, edits, 1)
	assert.Equal(t, "'pages/home'", edits[0].NewText)
}

func TestCollectViewRenameEdits_MultipleOccurrences(t *testing.T) {
	src := []byte(`<?php
$a = View::factory('pages/about');
$b = new View('pages/about', []);
$c = View::factory('pages/about');
`)
	edits := collectViewRenameEdits(src, "test.php", "pages/about", "pages/home")
	assert.Len(t, edits, 3, "all three occurrences should be replaced")
	for _, e := range edits {
		assert.Equal(t, "'pages/home'", e.NewText)
	}
}

func TestCollectViewRenameEdits_NoMatch(t *testing.T) {
	src := []byte("<?php View::factory('pages/other'); ")
	edits := collectViewRenameEdits(src, "test.php", "pages/about", "pages/home")
	assert.Empty(t, edits)
}

func TestCollectViewRenameEdits_FindFileVariant(t *testing.T) {
	src := []byte("<?php Kohana::find_file('views', 'pages/about'); ")
	edits := collectViewRenameEdits(src, "test.php", "pages/about", "pages/home")
	require.Len(t, edits, 1)
	assert.Equal(t, "'pages/home'", edits[0].NewText)
}

func TestCollectViewRenameEdits_PreservesDoubleQuotes(t *testing.T) {
	src := []byte(`<?php View::factory("pages/about"); `)
	edits := collectViewRenameEdits(src, "test.php", "pages/about", "pages/home")
	require.Len(t, edits, 1)
	assert.Equal(t, `"pages/home"`, edits[0].NewText)
}

// ─── Rename integration against fixture ──────────────────────────────────────

func TestCollectViewRenameEdits_FixtureController(t *testing.T) {
	src, path := readFixture(t, filepath.Join("application", "classes", "Controller", "Pages.php"))

	// The stock fixture uses 'pages/about' in View::factory and new View.
	edits := collectViewRenameEdits(src, path, "pages/about", "pages/home")
	assert.NotEmpty(t, edits, "stock controller must have at least one edit")

	for _, e := range edits {
		assert.Equal(t, "'pages/home'", e.NewText)
		// Verify line is plausible (> 0).
		assert.Greater(t, e.Range.Start.Line, uint32(0))
	}
}

// ─── viewFileNewPath ─────────────────────────────────────────────────────────

func TestViewFileNewPath_SimpleRename(t *testing.T) {
	defPath := "/project/application/views/pages/about.php"
	got := viewFileNewPath(defPath, "pages/about", "pages/home")
	assert.Equal(t, "/project/application/views/pages/home.php", got)
}

func TestViewFileNewPath_NestedRename(t *testing.T) {
	defPath := "/project/application/views/pages/about.php"
	got := viewFileNewPath(defPath, "pages/about", "section/pages/home")
	assert.Equal(t, "/project/application/views/section/pages/home.php", got)
}

func TestViewFileNewPath_NoMatch(t *testing.T) {
	defPath := "/project/application/views/pages/other.php"
	got := viewFileNewPath(defPath, "pages/about", "pages/home")
	assert.Equal(t, "", got, "non-matching path must return empty string")
}

// ─── textEditsToDocChanges ────────────────────────────────────────────────────

func TestTextEditsToDocChanges_PopulatesTextDocumentEdits(t *testing.T) {
	changes := map[protocol.DocumentUri][]protocol.TextEdit{
		"file:///a.php": {
			{Range: protocol.Range{}, NewText: "'new'"},
		},
		"file:///b.php": {
			{Range: protocol.Range{}, NewText: "'new'"},
			{Range: protocol.Range{}, NewText: "'new'"},
		},
	}
	docChanges := textEditsToDocChanges(changes)
	assert.Len(t, docChanges, 2)
	for _, dc := range docChanges {
		tde, ok := dc.(protocol.TextDocumentEdit)
		require.True(t, ok, "each entry must be a TextDocumentEdit")
		assert.NotEmpty(t, tde.TextDocument.URI)
		assert.NotEmpty(t, tde.Edits)
	}
}

// ─── toLSPRange precision check ───────────────────────────────────────────────

func TestRenameRangeCoversQuotes(t *testing.T) {
	src := []byte("<?php View::factory('pages/about'); ")
	needle := "'pages/about'"
	start := bytes.Index(src, []byte(needle))
	require.Greater(t, start, 0)

	_, loc, found := findViewNameLocAtOffset(src, "test.php", start+3)
	require.True(t, found)

	r := toLSPRange(loc, src)
	// The replaced range should span exactly the quoted string.
	// On the same line, character should be > 0 (not at column 0).
	assert.Greater(t, r.Start.Character, uint32(0), "start column must be past beginning of line")
	assert.Greater(t, r.End.Character, r.Start.Character, "end must be after start")

	// VKCOM StartPos is 0-indexed and matches bytes.Index exactly.
	assert.Equal(t, start, loc.StartByte,
		"StartByte should point to the opening quote")
	_ = phputil.Location{} // ensure phputil is used
}
