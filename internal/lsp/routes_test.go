package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// byteOffsetToPosition converts a byte offset in src to an LSP Position.
func byteOffsetToPosition(src []byte, offset int) protocol.Position {
	line := uint32(0)
	col := uint32(0)
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return protocol.Position{Line: line, Character: col}
}

// ─── titleCaseSegments ────────────────────────────────────────────────────────

func TestTitleCaseSegments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"pages", "Pages"},
		{"auth_user", "Auth_User"},
		{"admin", "Admin"},
		{"Pages", "Pages"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, titleCaseSegments(c.in), "input=%q", c.in)
	}
}

// ─── buildControllerClassName ─────────────────────────────────────────────────

func TestBuildControllerClassName(t *testing.T) {
	assert.Equal(t, "Controller_Pages", buildControllerClassName("pages", ""))
	assert.Equal(t, "Controller_Admin_Users", buildControllerClassName("users", "admin"))
	assert.Equal(t, "Controller_Api_V1_Posts", buildControllerClassName("posts", "api_v1"))
}

// ─── findActionMethodLine ─────────────────────────────────────────────────────

func TestFindActionMethodLine_Found(t *testing.T) {
	path := filepath.Join(stockRoot, "application", "classes", "Controller", "Pages.php")
	line, ok := findActionMethodLine(path, "about")
	assert.True(t, ok)
	assert.Greater(t, line, uint32(0), "action_about must not be on line 0")
}

func TestFindActionMethodLine_NotFound(t *testing.T) {
	path := filepath.Join(stockRoot, "application", "classes", "Controller", "Pages.php")
	_, ok := findActionMethodLine(path, "nonexistent")
	assert.False(t, ok)
}

func TestFindActionMethodLine_BadFile(t *testing.T) {
	_, ok := findActionMethodLine("/nonexistent/file.php", "index")
	assert.False(t, ok)
}

// ─── locateControllerAction ───────────────────────────────────────────────────

func stockCascade(t *testing.T) (string, config.Config, []project.Module) {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(stockRoot, cfg)
	require.NoError(t, err)
	return stockRoot, cfg, modules
}

func hmvcCascade(t *testing.T) (string, config.Config, []project.Module) {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(hmvcRoot, cfg)
	require.NoError(t, err)
	return hmvcRoot, cfg, modules
}

func TestLocateControllerAction_ControllerOnly(t *testing.T) {
	root, cfg, modules := stockCascade(t)
	locs := locateControllerAction(root, cfg, modules, "pages", "", "")
	require.NotEmpty(t, locs)
	assert.Contains(t, string(locs[0].URI), "Controller/Pages.php")
	assert.Equal(t, uint32(0), locs[0].Range.Start.Line, "no action: must land at file top")
}

func TestLocateControllerAction_WithAction(t *testing.T) {
	root, cfg, modules := stockCascade(t)
	locs := locateControllerAction(root, cfg, modules, "pages", "about", "")
	require.NotEmpty(t, locs)
	assert.Greater(t, locs[0].Range.Start.Line, uint32(0), "with action: must land on method line")
}

func TestLocateControllerAction_WithDirectory(t *testing.T) {
	root, cfg, modules := hmvcCascade(t)
	locs := locateControllerAction(root, cfg, modules, "users", "", "admin")
	require.NotEmpty(t, locs)
	assert.Contains(t, string(locs[0].URI), "Controller/Admin/Users.php")
}

func TestLocateControllerAction_WithDirectoryAndAction(t *testing.T) {
	root, cfg, modules := hmvcCascade(t)
	locs := locateControllerAction(root, cfg, modules, "users", "edit", "admin")
	require.NotEmpty(t, locs)
	assert.Contains(t, string(locs[0].URI), "Controller/Admin/Users.php")
	assert.Greater(t, locs[0].Range.Start.Line, uint32(0))
}

func TestLocateControllerAction_UnknownController(t *testing.T) {
	root, cfg, modules := stockCascade(t)
	locs := locateControllerAction(root, cfg, modules, "nonexistent", "", "")
	assert.Empty(t, locs)
}

// ─── routeDefaultsFinder ─────────────────────────────────────────────────────

func parseRouteOffset(src []byte, needle string) int {
	idx := bytes.Index(src, []byte(needle))
	if idx < 0 {
		return -1
	}
	return idx + len(needle)/2 // cursor inside the string
}

func TestRouteDefaultsFinder_CursorOnController(t *testing.T) {
	src := []byte(`<?php
Route::set('default', '(<controller>(/<action>))')
    ->defaults(['controller' => 'pages', 'action' => 'about']);
`)
	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	offset := idx + 3 // inside 'pages'

	m, ok := findRouteAtOffset(src, "test.php", offset, nil)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
	assert.Equal(t, "about", m.Action)
	assert.Equal(t, "controller", m.CursorOn)
}

func TestRouteDefaultsFinder_CursorOnAction(t *testing.T) {
	src := []byte(`<?php
Route::set('default', '(<controller>(/<action>))')
    ->defaults(['controller' => 'pages', 'action' => 'about']);
`)
	needle := "'about'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	offset := idx + 3

	m, ok := findRouteAtOffset(src, "test.php", offset, nil)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
	assert.Equal(t, "about", m.Action)
	assert.Equal(t, "action", m.CursorOn)
}

func TestRouteDefaultsFinder_WithDirectory(t *testing.T) {
	src := []byte(`<?php
Route::set('admin', 'admin(/<action>)')
    ->defaults(['directory' => 'admin', 'controller' => 'users', 'action' => 'index']);
`)
	needle := "'users'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	offset := idx + 3

	m, ok := findRouteAtOffset(src, "test.php", offset, nil)
	require.True(t, ok)
	assert.Equal(t, "users", m.Controller)
	assert.Equal(t, "admin", m.Directory)
	assert.Equal(t, "controller", m.CursorOn)
}

func TestRouteDefaultsFinder_WithFilterInChain(t *testing.T) {
	// ->filter(...) between set and defaults must still be recognised.
	src := []byte(`<?php
Route::set('x', 'x')->filter(function($r,$p){return $p;})
    ->defaults(['controller' => 'pages', 'action' => 'index']);
`)
	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
}

func TestRouteDefaultsFinder_NotInDefaults(t *testing.T) {
	// Cursor on the route name string, not in defaults array.
	src := []byte(`<?php Route::set('myroute', 'x')->defaults(['controller'=>'pages']);`)
	needle := "'myroute'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	_, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	assert.False(t, ok)
}

// ─── routeURLFinder ───────────────────────────────────────────────────────────

func TestRouteURLFinder_CursorOnController(t *testing.T) {
	src := []byte(`<?php $url = Route::url('default', ['controller' => 'pages', 'action' => 'about']);`)
	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
	assert.Equal(t, "about", m.Action)
	assert.Equal(t, "controller", m.CursorOn)
}

func TestRouteURLFinder_CursorOnAction(t *testing.T) {
	src := []byte(`<?php $url = Route::url('default', ['controller' => 'pages', 'action' => 'contact']);`)
	needle := "'contact'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	require.True(t, ok)
	assert.Equal(t, "contact", m.Action)
	assert.Equal(t, "action", m.CursorOn)
}

// ─── requestFactoryFinder ─────────────────────────────────────────────────────

func TestRequestFactoryFinder_CursorOnController(t *testing.T) {
	src := []byte(`<?php Request::factory()->controller('pages')->action('about')->execute();`)
	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
	assert.Equal(t, "controller", m.CursorOn)
	// Action is downstream — not available when cursor is on controller.
	assert.Equal(t, "", m.Action)
}

func TestRequestFactoryFinder_CursorOnAction(t *testing.T) {
	src := []byte(`<?php Request::factory()->controller('pages')->action('about')->execute();`)
	needle := "'about'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	require.True(t, ok)
	assert.Equal(t, "about", m.Action)
	assert.Equal(t, "pages", m.Controller, "must find sibling controller upstream")
	assert.Equal(t, "action", m.CursorOn)
}

func TestRequestFactoryFinder_NonRequestChain(t *testing.T) {
	// A ->action() on something that isn't Request should not match.
	src := []byte(`<?php $obj->action('do_something');`)
	needle := "'do_something'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	_, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	assert.False(t, ok)
}

func TestRequestFactoryFinder_Initial(t *testing.T) {
	// Request::initial() is also a valid root.
	src := []byte(`<?php Request::initial()->controller('pages')->action('about');`)
	needle := "'about'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
}

// ─── routeHelperFinder ────────────────────────────────────────────────────────

func routesIntPtr(n int) *int { return &n }

func TestRouteHelperFinder_CursorOnController(t *testing.T) {
	helpers := []config.RouteHelper{
		{Name: "Skp_Helper::getWidget", Controller: routesIntPtr(0), Action: routesIntPtr(1)},
	}
	src := []byte(`<?php Skp_Helper::getWidget('pages', 'about', [], 'post');`)
	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, helpers)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
	assert.Equal(t, "about", m.Action)
	assert.Equal(t, "controller", m.CursorOn)
}

func TestRouteHelperFinder_CursorOnAction(t *testing.T) {
	helpers := []config.RouteHelper{
		{Name: "Skp_Helper::getWidget", Controller: routesIntPtr(0), Action: routesIntPtr(1)},
	}
	src := []byte(`<?php Skp_Helper::getWidget('pages', 'contact', []);`)
	needle := "'contact'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, helpers)
	require.True(t, ok)
	assert.Equal(t, "contact", m.Action)
	assert.Equal(t, "pages", m.Controller)
	assert.Equal(t, "action", m.CursorOn)
}

func TestRouteHelperFinder_GlobalFunction(t *testing.T) {
	helpers := []config.RouteHelper{
		{Name: "render_widget", Controller: routesIntPtr(0), Action: routesIntPtr(1)},
	}
	src := []byte(`<?php render_widget('pages', 'about');`)
	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	m, ok := findRouteAtOffset(src, "test.php", idx+3, helpers)
	require.True(t, ok)
	assert.Equal(t, "pages", m.Controller)
}

func TestRouteHelperFinder_NoMatch(t *testing.T) {
	// No helpers configured — should not match.
	src := []byte(`<?php Skp_Helper::getWidget('pages', 'about');`)
	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0)
	_, ok := findRouteAtOffset(src, "test.php", idx+3, nil)
	assert.False(t, ok)
}

// ─── End-to-end through Definition handler ────────────────────────────────────

func TestDefinition_RouteDefaults_ControllerNavigation(t *testing.T) {
	s := buildStockServer(t)
	bootstrapPath := filepath.Join(stockRoot, "application", "bootstrap.php")
	src, err := os.ReadFile(bootstrapPath)
	require.NoError(t, err)
	uri := PathToURI(bootstrapPath)
	s.docs.Set(uri, src)

	needle := "'pages'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0, "bootstrap.php must contain 'pages'")

	// Find the position of the cursor inside 'pages'.
	pos := byteOffsetToPosition(src, idx+3)

	result, err := s.Definition(nil, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	locs, ok := result.([]protocol.Location)
	require.True(t, ok)
	require.NotEmpty(t, locs)
	assert.Contains(t, string(locs[0].URI), "Controller/Pages.php")
}

func TestDefinition_RouteDefaults_ActionNavigation(t *testing.T) {
	s := buildStockServer(t)
	bootstrapPath := filepath.Join(stockRoot, "application", "bootstrap.php")
	src, err := os.ReadFile(bootstrapPath)
	require.NoError(t, err)
	uri := PathToURI(bootstrapPath)
	s.docs.Set(uri, src)

	needle := "'about'"
	idx := bytes.Index(src, []byte(needle))
	require.Greater(t, idx, 0, "bootstrap.php must contain 'about'")
	pos := byteOffsetToPosition(src, idx+3)

	result, err := s.Definition(nil, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
	})
	require.NoError(t, err)
	locs, ok := result.([]protocol.Location)
	require.True(t, ok)
	require.NotEmpty(t, locs)
	assert.Contains(t, string(locs[0].URI), "Controller/Pages.php")
	assert.Greater(t, locs[0].Range.Start.Line, uint32(0), "action nav must land on method line")
}
