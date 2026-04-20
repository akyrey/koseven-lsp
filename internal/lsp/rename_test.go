package lsp

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
