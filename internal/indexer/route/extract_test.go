package route_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/route"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
)

func intPtr(n int) *int { return &n }

func extract(t *testing.T, src string, helpers []config.RouteHelper) []route.Ref {
	t.Helper()
	root, err := phpparse.Bytes([]byte(src), "test.php")
	require.NoError(t, err)
	return route.Extract("test.php", root, helpers)
}

func TestExtract(t *testing.T) {
	helpers := []config.RouteHelper{
		{Name: "Skp_Helper::getWidget", Controller: intPtr(0), Action: intPtr(1)},
		{Name: "render_widget", Controller: intPtr(0), Action: intPtr(1), Directory: intPtr(2)},
	}
	cases := []struct {
		name      string
		src       string
		wantKind  route.Kind
		wantCtrl  string
		wantAct   string
		wantDir   string
		wantClass string
	}{
		{
			name:      "route defaults",
			src:       `<?php Route::set('d', '(<controller>)')->defaults(['controller' => 'pages', 'action' => 'about']);`,
			wantKind:  route.KindRouteDefaults,
			wantCtrl:  "pages",
			wantAct:   "about",
			wantClass: "Controller_Pages",
		},
		{
			name:      "route defaults through filter with directory",
			src:       `<?php Route::set('a', 'a')->filter(function($r){return $r;})->defaults(['directory' => 'admin', 'controller' => 'users', 'action' => 'edit']);`,
			wantKind:  route.KindRouteDefaults,
			wantCtrl:  "users",
			wantAct:   "edit",
			wantDir:   "admin",
			wantClass: "Controller_Admin_Users",
		},
		{
			name:      "route url",
			src:       `<?php echo Route::url('default', ['controller' => 'pages', 'action' => 'contact']);`,
			wantKind:  route.KindRouteURL,
			wantCtrl:  "pages",
			wantAct:   "contact",
			wantClass: "Controller_Pages",
		},
		{
			name:      "route get uri",
			src:       `<?php echo Route::get('default')->uri(['controller' => 'pages', 'action' => 'about']);`,
			wantKind:  route.KindRouteURI,
			wantCtrl:  "pages",
			wantAct:   "about",
			wantClass: "Controller_Pages",
		},
		{
			name:      "request chain",
			src:       `<?php Request::factory()->directory('admin')->controller('users')->action('edit')->execute();`,
			wantKind:  route.KindRequest,
			wantCtrl:  "users",
			wantAct:   "edit",
			wantDir:   "admin",
			wantClass: "Controller_Admin_Users",
		},
		{
			name:      "static helper",
			src:       `<?php Skp_Helper::getWidget('pages', 'about', []);`,
			wantKind:  route.KindHelper,
			wantCtrl:  "pages",
			wantAct:   "about",
			wantClass: "Controller_Pages",
		},
		{
			name:      "function helper",
			src:       `<?php render_widget('users', 'edit', 'admin');`,
			wantKind:  route.KindHelper,
			wantCtrl:  "users",
			wantAct:   "edit",
			wantDir:   "admin",
			wantClass: "Controller_Admin_Users",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refs := extract(t, c.src, helpers)
			require.Len(t, refs, 1)
			r := refs[0]
			assert.Equal(t, c.wantKind, r.Kind)
			assert.Equal(t, c.wantCtrl, r.Controller.Value)
			assert.Equal(t, c.wantAct, r.Action.Value)
			assert.Equal(t, c.wantDir, r.Directory.Value)
			assert.Equal(t, c.wantClass, r.ControllerClass())
		})
	}
}

func TestExtract_NoMatch(t *testing.T) {
	cases := map[string]string{
		"non-request action":    `<?php $obj->action('x');`,
		"request getter":        `<?php $a = Request::current()->action();`,
		"unconfigured helper":   `<?php Skp_Helper::getWidget('pages', 'about');`,
		"dynamic values":        `<?php Route::url('d', ['controller' => $c, 'action' => $a]);`,
		"route url without arr": `<?php Route::url('d');`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, extract(t, src, nil))
		})
	}
}

func TestExtract_FieldLocationsMatchLiteral(t *testing.T) {
	src := `<?php
Route::url('default', ['controller' => 'pages', 'action' => 'about']);
`
	refs := extract(t, src, nil)
	require.Len(t, refs, 1)

	start := bytes.Index([]byte(src), []byte("'about'"))
	act := refs[0].Action
	assert.Equal(t, start, act.Loc.StartByte)
	assert.Equal(t, start+len("'about'"), act.Loc.EndByte)
	assert.Equal(t, 2, act.Loc.StartLine)
	assert.True(t, act.Contains(start+2))
	assert.False(t, act.Contains(start-1))
}

func TestExtract_RequestChainRecordedOnce(t *testing.T) {
	refs := extract(t, `<?php Request::factory()->controller('pages')->action('about')->execute();`, nil)
	require.Len(t, refs, 1, "inner chain calls must not produce duplicate refs")
	assert.Equal(t, "about", refs[0].Action.Value)
}

func TestActionFromMethod(t *testing.T) {
	cases := map[string]string{
		"action_index": "index",
		"Action_About": "About",
		"action_":      "",
		"before":       "",
		"actionfoo":    "",
	}
	for in, want := range cases {
		assert.Equal(t, want, route.ActionFromMethod(in), "input=%q", in)
	}
}

func TestControllerClassName(t *testing.T) {
	cases := []struct{ ctrl, dir, want string }{
		{"pages", "", "Controller_Pages"},
		{"auth_user", "", "Controller_Auth_User"},
		{"Pages", "", "Controller_Pages"},
		{"users", "admin", "Controller_Admin_Users"},
		{"posts", "api_v1", "Controller_Api_V1_Posts"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, route.ControllerClassName(c.ctrl, c.dir))
	}
	assert.Equal(t, "", route.TitleCaseSegments(""))
}

func TestClassKey(t *testing.T) {
	assert.Equal(t, "controller_pages", route.ClassKey(`\Controller_Pages`))
	assert.Equal(t, route.ClassKey("Controller_Pages"), route.ClassKey(route.ControllerClassName("pages", "")))
}
