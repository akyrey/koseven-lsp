package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// fixtureRoot returns the absolute path to testdata/stock relative to this file.
var (
	stockRoot = filepath.Join("..", "..", "testdata", "stock")
	hmvcRoot  = filepath.Join("..", "..", "testdata", "hmvc")
)

func readFixture(t *testing.T, rel string) ([]byte, string) {
	t.Helper()
	path := filepath.Join(stockRoot, rel)
	src, err := os.ReadFile(path)
	require.NoError(t, err, "reading fixture %s", rel)
	return src, path
}

func offsetOf(src []byte, needle string) int {
	idx := bytes.Index(src, []byte(needle))
	if idx < 0 {
		return -1
	}
	return idx
}

// TestFindViewNameAtOffset_ViewFactory checks that a cursor within the string
// literal of View::factory('pages/about') returns the correct view name.
func TestFindViewNameAtOffset_ViewFactory(t *testing.T) {
	src, path := readFixture(t, filepath.Join("application", "classes", "Controller", "Pages.php"))

	needle := "'pages/about'"
	start := offsetOf(src, needle)
	require.Greater(t, start, 0, "fixture must contain %s", needle)

	tests := []struct {
		name   string
		offset int
	}{
		{"at opening quote", start},
		{"inside string", start + 3},
		{"at closing quote", start + len(needle) - 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findViewNameAtOffset(src, path, tc.offset)
			assert.Equal(t, "pages/about", got)
		})
	}
}

// TestFindViewNameAtOffset_NewView checks that new View('pages/about', [...])
// is also recognised.
func TestFindViewNameAtOffset_NewView(t *testing.T) {
	src, path := readFixture(t, filepath.Join("application", "classes", "Controller", "Pages.php"))

	// The fixture has: new View('pages/about', [...])
	needle := "'pages/about'"
	// Find the second occurrence (inside new View call)
	first := bytes.Index(src, []byte(needle))
	require.Greater(t, first, 0)
	second := bytes.Index(src[first+1:], []byte(needle))
	if second < 0 {
		t.Skip("fixture has only one occurrence of 'pages/about' — skipping new View test")
	}
	offset := first + 1 + second + 3 // cursor inside second occurrence
	got := findViewNameAtOffset(src, path, offset)
	assert.Equal(t, "pages/about", got)
}

// TestFindViewNameAtOffset_NotOnString checks that offsets outside view name
// strings return "".
func TestFindViewNameAtOffset_NotOnString(t *testing.T) {
	src, path := readFixture(t, filepath.Join("application", "classes", "Controller", "Pages.php"))

	// Offset 0 is at the start of the file (<?php) — not a view name.
	got := findViewNameAtOffset(src, path, 0)
	assert.Equal(t, "", got)
}

// TestFindViewNameAtOffset_FindFile checks Kohana::find_file('views', 'pages/about').
func TestFindViewNameAtOffset_FindFile(t *testing.T) {
	src := []byte(`<?php
Kohana::find_file('views', 'pages/about');
`)
	needle := "'pages/about'"
	start := offsetOf(src, needle)
	require.Greater(t, start, 0)

	got := findViewNameAtOffset(src, "test.php", start+3)
	assert.Equal(t, "pages/about", got)
}

// ─── ORM::factory tests ──────────────────────────────────────────────────────

// TestFindClassFactoryAtOffset_ORM checks ORM::factory('User') detection.
func TestFindClassFactoryAtOffset_ORM(t *testing.T) {
	src := []byte(`<?php ORM::factory('User'); `)
	needle := "'User'"
	start := offsetOf(src, needle)
	require.Greater(t, start, 0)

	className, relPath := findClassFactoryAtOffset(src, "test.php", start+2)
	assert.Equal(t, "Model_User", className)
	assert.Equal(t, "Model/User.php", relPath)
}

// TestFindClassFactoryAtOffset_ModelFactory checks Model::factory variation.
func TestFindClassFactoryAtOffset_ModelFactory(t *testing.T) {
	src := []byte(`<?php Model::factory('Post'); `)
	start := offsetOf(src, "'Post'")
	require.Greater(t, start, 0)

	className, relPath := findClassFactoryAtOffset(src, "test.php", start+2)
	assert.Equal(t, "Model_Post", className)
	assert.Equal(t, "Model/Post.php", relPath)
}

// TestFindClassFactoryAtOffset_SubModel checks compound model names.
func TestFindClassFactoryAtOffset_SubModel(t *testing.T) {
	src := []byte(`<?php ORM::factory('Member_Profile'); `)
	start := offsetOf(src, "'Member_Profile'")
	require.Greater(t, start, 0)

	_, relPath := findClassFactoryAtOffset(src, "test.php", start+3)
	assert.Equal(t, "Model/Member/Profile.php", relPath)
}

// TestFindClassFactoryAtOffset_NotOnString returns empty when cursor is not
// on the string arg.
func TestFindClassFactoryAtOffset_NotOnString(t *testing.T) {
	src := []byte(`<?php ORM::factory('User'); `)
	// Cursor on "ORM" keyword, not on the string arg.
	className, relPath := findClassFactoryAtOffset(src, "test.php", 6)
	assert.Empty(t, className)
	assert.Empty(t, relPath)
}

// ─── Kohana::find_file tests ─────────────────────────────────────────────────

// TestFindFindFileAtOffset_Classes checks find_file('classes', ...) detection.
func TestFindFindFileAtOffset_Classes(t *testing.T) {
	src := []byte(`<?php Kohana::find_file('classes', 'Model_User'); `)
	start := offsetOf(src, "'Model_User'")
	require.Greater(t, start, 0)

	fileType, relPath := findFindFileAtOffset(src, "test.php", start+3)
	assert.Equal(t, "classes", fileType)
	assert.Equal(t, "Model/User.php", relPath)
}

// TestFindFindFileAtOffset_I18n checks find_file('i18n', ...) detection.
func TestFindFindFileAtOffset_I18n(t *testing.T) {
	src := []byte(`<?php Kohana::find_file('i18n', 'en'); `)
	start := offsetOf(src, "'en'")
	require.Greater(t, start, 0)

	fileType, relPath := findFindFileAtOffset(src, "test.php", start+2)
	assert.Equal(t, "i18n", fileType)
	assert.Equal(t, "en.php", relPath)
}

// TestFindFindFileAtOffset_Views returns empty (handled by viewNameFinder).
func TestFindFindFileAtOffset_Views(t *testing.T) {
	src := []byte(`<?php Kohana::find_file('views', 'pages/about'); `)
	start := offsetOf(src, "'pages/about'")
	require.Greater(t, start, 0)

	fileType, _ := findFindFileAtOffset(src, "test.php", start+3)
	assert.Equal(t, "views", fileType, "views type must be returned so caller can skip it")
}

// ─── Cascade integration ─────────────────────────────────────────────────────

// TestCascadeLocs_ORM verifies that ORM::factory('User') resolves to the
// Model/User.php fixture file via the cascade.
func TestCascadeLocs_ORM(t *testing.T) {
	cfg, modules := stockCascadeState(t)
	locs := cascadeLocs(stockRoot, cfg, modules, "classes", "Model/User.php")
	require.NotEmpty(t, locs)
	assert.Contains(t, string(locs[0].URI), "Model/User.php")
}

// stockCascadeState loads the stock fixture's cascade state for tests.
func stockCascadeState(t *testing.T) (config.Config, []project.Module) {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(stockRoot, cfg)
	require.NoError(t, err)
	return cfg, modules
}

// TestFindVarAtOffset verifies that bare variable names inside a view file are
// located correctly.
func TestFindVarAtOffset_Found(t *testing.T) {
	src := []byte(`<?php defined('SYSPATH') or die();
echo $user->name;
`)
	needle := []byte("$user")
	start := bytes.Index(src, needle)
	require.Greater(t, start, 0)

	got := findVarAtOffset(src, "test.php", start+2)
	assert.Equal(t, "user", got)
}

func TestFindVarAtOffset_NotFound(t *testing.T) {
	src := []byte(`<?php echo "hello"; `)
	got := findVarAtOffset(src, "test.php", 5)
	assert.Equal(t, "", got)
}
