package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/project"
)

func buildServerFor(t *testing.T, root string) *Server {
	t.Helper()
	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)
	idx, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)
	return &Server{cfg: cfg, modules: modules, root: root, viewIdx: idx, docs: newDocumentStore()}
}

// referencesAt opens the file at root/rel and requests references with the
// cursor placed inside the first occurrence of needle.
func referencesAt(t *testing.T, s *Server, rel, needle string) []protocol.Location {
	t.Helper()
	path := filepath.Join(s.root, rel)
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	off := bytes.Index(src, []byte(needle))
	require.GreaterOrEqual(t, off, 0, "needle %q not found in %s", needle, rel)

	uri := PathToURI(path)
	s.docs.Set(uri, src)
	locs, err := s.References(nil, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     byteOffsetToPosition(src, off+len(needle)/2),
		},
	})
	require.NoError(t, err)
	return locs
}

// locText returns the source text covered by a single-line location.
func locText(t *testing.T, loc protocol.Location) string {
	t.Helper()
	src, err := os.ReadFile(URIToPath(loc.URI))
	require.NoError(t, err)
	lines := strings.Split(string(src), "\n")
	require.Less(t, int(loc.Range.Start.Line), len(lines))
	line := lines[loc.Range.Start.Line]
	return line[loc.Range.Start.Character:loc.Range.End.Character]
}

func locFiles(locs []protocol.Location) []string {
	files := make([]string, len(locs))
	for i, l := range locs {
		files[i] = filepath.Base(URIToPath(l.URI))
	}
	return files
}

func TestReferences_Controllers(t *testing.T) {
	stockCtrl := filepath.Join("application", "classes", "Controller", "Pages.php")
	adminCtrl := filepath.Join("application", "classes", "Controller", "Admin", "Users.php")

	cases := []struct {
		name      string
		root      string
		file      string
		needle    string
		wantFiles []string // base names, order-insensitive
		wantText  string   // text under every returned range
	}{
		{
			name:      "action method name lists route defaults and Route::url",
			root:      stockRoot,
			file:      stockCtrl,
			needle:    "action_about()",
			wantFiles: []string{"bootstrap.php", "contact.php"},
			wantText:  "'about'",
		},
		{
			name:     "unreferenced action returns nothing",
			root:     stockRoot,
			file:     stockCtrl,
			needle:   "action_contact",
			wantText: "",
		},
		{
			name:      "controller class name lists every reference",
			root:      stockRoot,
			file:      stockCtrl,
			needle:    "Controller_Pages",
			wantFiles: []string{"bootstrap.php", "contact.php"},
			wantText:  "'pages'",
		},
		{
			name:      "HMVC directory action via route defaults",
			root:      hmvcRoot,
			file:      adminCtrl,
			needle:    "action_index",
			wantFiles: []string{"bootstrap.php"},
			wantText:  "'index'",
		},
		{
			name:      "HMVC directory action via Request chain",
			root:      hmvcRoot,
			file:      adminCtrl,
			needle:    "action_edit",
			wantFiles: []string{"Pages.php"},
			wantText:  "'edit'",
		},
		{
			name:      "cursor on route action string lists sibling references",
			root:      stockRoot,
			file:      filepath.Join("application", "views", "pages", "contact.php"),
			needle:    "'about'",
			wantFiles: []string{"bootstrap.php", "contact.php"},
			wantText:  "'about'",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := buildServerFor(t, c.root)
			locs := referencesAt(t, s, c.file, c.needle)
			assert.ElementsMatch(t, c.wantFiles, locFiles(locs))
			for _, l := range locs {
				assert.Equal(t, c.wantText, locText(t, l))
			}
		})
	}
}

func TestReferences_NonActionMethodIgnored(t *testing.T) {
	src := []byte(`<?php
class Controller_Pages extends Controller {
    public function before() {}
}
class Model_User extends ORM {
    public function action_about() {}
}`)
	for _, needle := range []string{"before", "action_about", "Model_User"} {
		off := bytes.Index(src, []byte(needle))
		_, ok := findControllerDeclAtOffset(src, "test.php", off+1)
		assert.False(t, ok, "needle %q must not be a controller target", needle)
	}
}

func TestReferences_ReindexPicksUpNewCallSite(t *testing.T) {
	dir := t.TempDir()
	ctrl := filepath.Join(dir, "Pages.php")
	require.NoError(t, os.WriteFile(ctrl, []byte(`<?php echo Route::url('d', ['controller' => 'pages', 'action' => 'about']);`), 0o644))

	idx := buildStockIndex(t)
	before := len(idx.ControllerRefs("Controller_Pages"))

	next, err := view.ReindexFile(ctrl, idx, nil)
	require.NoError(t, err)
	assert.Len(t, next.ControllerRefs("controller_pages"), before+1, "lookup must be case-insensitive and include the new site")

	require.NoError(t, os.Remove(ctrl))
	next, err = view.ReindexFile(ctrl, next, nil)
	require.NoError(t, err)
	assert.Len(t, next.ControllerRefs("Controller_Pages"), before, "deleted file's refs must be dropped")
}

func TestReferences_ViewFileOutsideRouteStringStillListsViewUsages(t *testing.T) {
	s := buildServerFor(t, stockRoot)
	locs := referencesAt(t, s, filepath.Join("application", "views", "pages", "contact.php"), "<h1>Contact")
	require.NotEmpty(t, locs)
	for _, l := range locs {
		assert.Equal(t, "'pages/contact'", locText(t, l))
	}
}
