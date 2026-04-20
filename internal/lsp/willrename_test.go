package lsp

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// ─── viewNameFromPath ─────────────────────────────────────────────────────────

func TestViewNameFromPath_ApplicationView(t *testing.T) {
	cfg, modules := stockCascadeState(t)
	viewRoots := project.BuildViewRoots(stockRoot, cfg, modules)

	path := filepath.Join(stockRoot, "application", "views", "pages", "about.php")
	got := viewNameFromPath(path, viewRoots)
	assert.Equal(t, "pages/about", got)
}

func TestViewNameFromPath_OutsideViewRoots(t *testing.T) {
	cfg, modules := stockCascadeState(t)
	viewRoots := project.BuildViewRoots(stockRoot, cfg, modules)

	path := filepath.Join(stockRoot, "application", "classes", "Controller", "Pages.php")
	got := viewNameFromPath(path, viewRoots)
	assert.Equal(t, "", got, "class files should not resolve to a view name")
}

func TestViewNameFromPath_ModuleView(t *testing.T) {
	cfg, modules := hmvcCascadeState(t)
	viewRoots := project.BuildViewRoots(hmvcRoot, cfg, modules)

	path := filepath.Join(hmvcRoot, "modules", "blog", "views", "pages", "about.php")
	got := viewNameFromPath(path, viewRoots)
	assert.Equal(t, "pages/about", got)
}

func hmvcCascadeState(t *testing.T) (config.Config, []project.Module) {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(hmvcRoot, cfg)
	require.NoError(t, err)
	return cfg, modules
}

// ─── WillRenameFiles integration ─────────────────────────────────────────────

// TestWillRenameFiles_BuildsEditsForKnownView verifies that collectViewRenameEdits
// is invoked correctly for each usage of a view when it would be renamed.
// We test the pure rename-edit logic rather than the full handler (which needs
// a live server with an index).
func TestWillRenameFiles_RenameEditsForUsages(t *testing.T) {
	// Simulate: pages/about → pages/home by calling collectViewRenameEdits
	// directly on the stock controller fixture.
	src, path := readFixture(t, filepath.Join("application", "classes", "Controller", "Pages.php"))

	edits := collectViewRenameEdits(src, path, "pages/about", "pages/home")
	require.NotEmpty(t, edits, "controller fixture should have usages of pages/about")

	for _, e := range edits {
		assert.Equal(t, "'pages/home'", e.NewText)
	}
}

// TestViewNameFromPath_RoundTrip checks that going path→name→… reproduces the
// correct name for fixtures we know about.
func TestViewNameFromPath_RoundTrip(t *testing.T) {
	cfg, modules := stockCascadeState(t)
	viewRoots := project.BuildViewRoots(stockRoot, cfg, modules)

	// Walk through the view index to verify every known view file maps back.
	idx := buildStockIndex(t)
	for _, def := range idx.AllDefinitions() {
		got := viewNameFromPath(def.Path, viewRoots)
		// For application/system views we should always get the correct name.
		if def.RootKind == project.RootApplication || def.RootKind == project.RootSystem {
			assert.Equal(t, def.Name, got,
				"viewNameFromPath round-trip failed for %s", def.Path)
		}
	}
}

// ─── Kohana::message go-to-def ───────────────────────────────────────────────

func TestFindKohanaMessageAtOffset_FirstArg(t *testing.T) {
	src := []byte(`<?php Kohana::message('auth', 'username.notEmpty'); `)
	needle := "'auth'"
	start := offsetOf(src, needle)
	require.Greater(t, start, 0)

	got := findKohanaMessageAtOffset(src, "test.php", start+2)
	assert.Equal(t, "auth", got)
}

func TestFindKohanaMessageAtOffset_CursorOnKey(t *testing.T) {
	// Cursor on the second arg (key) — not the file name, so should return "".
	src := []byte(`<?php Kohana::message('auth', 'username.notEmpty'); `)
	needle := "'username.notEmpty'"
	start := offsetOf(src, needle)
	require.Greater(t, start, 0)

	got := findKohanaMessageAtOffset(src, "test.php", start+5)
	assert.Equal(t, "", got, "cursor on key arg should return empty, not file name")
}

func TestFindKohanaMessageAtOffset_NotKohana(t *testing.T) {
	src := []byte(`<?php SomeClass::message('auth'); `)
	start := offsetOf(src, "'auth'")
	require.Greater(t, start, 0)

	got := findKohanaMessageAtOffset(src, "test.php", start+2)
	assert.Equal(t, "", got)
}

// TestCascadeLocs_Message verifies the cascade lookup for messages/.
func TestCascadeLocs_Message(t *testing.T) {
	cfg, modules := stockCascadeState(t)
	locs := cascadeLocs(stockRoot, cfg, modules, "messages", "auth.php")
	require.NotEmpty(t, locs)
	assert.Contains(t, string(locs[0].URI), "auth.php")
}

// ─── DocumentSymbol with children ────────────────────────────────────────────

func TestDocumentSymbol_HasVarChildren(t *testing.T) {
	idx := buildStockIndex(t)
	viewPath := filepath.Join(stockRoot, "application", "views", "pages", "about.php")

	names := idx.NamesForFile(viewPath)
	require.NotEmpty(t, names)

	vars := idx.VarsFor(names[0])
	require.NotEmpty(t, vars, "pages/about must have at least one exposed var")

	// Verify the var names are what we expect.
	byName := make(map[string]bool)
	for _, v := range vars {
		byName[v.Name] = true
	}
	assert.True(t, byName["user"], "user var should be exposed")
	assert.True(t, byName["show_contact"], "show_contact var should be exposed")
	assert.True(t, byName["email"], "email var should be exposed")
}
