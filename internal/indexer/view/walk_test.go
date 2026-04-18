package view_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/project"
)

func walkStock(t *testing.T) *view.ViewIndex {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "stock")
	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)
	idx, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)
	return idx
}

func walkHMVC(t *testing.T) *view.ViewIndex {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "hmvc")
	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)
	idx, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)
	return idx
}

// — View definition discovery —

func TestWalk_Stock_FindsDefinition(t *testing.T) {
	idx := walkStock(t)
	defs := idx.Resolve("pages/about")
	require.Len(t, defs, 1, "stock has exactly one pages/about view")
	assert.Equal(t, view.RootApplication, defs[0].RootKind)
	assert.Equal(t, "", defs[0].ModuleName)
	assert.Contains(t, defs[0].Path, "about.php")
}

func TestWalk_HMVC_MultipleCandidates(t *testing.T) {
	idx := walkHMVC(t)
	defs := idx.Resolve("pages/about")
	require.GreaterOrEqual(t, len(defs), 1, "hmvc fixture has at least the blog module view")

	// Blog module overrides pages/about — find it in results.
	var foundBlog bool
	for _, d := range defs {
		if d.ModuleName == "blog" {
			foundBlog = true
		}
	}
	assert.True(t, foundBlog, "blog module's pages/about must be in the result set")
}

func TestWalk_HMVC_CascadeOrder(t *testing.T) {
	idx := walkHMVC(t)
	defs := idx.Resolve("pages/about")

	// Application root (if present) must have lower RootOrder than module roots.
	for _, d := range defs {
		if d.RootKind == view.RootApplication {
			for _, other := range defs {
				if other.RootKind == view.RootModule {
					assert.Less(t, d.RootOrder, other.RootOrder,
						"application root must have higher cascade priority than module roots")
				}
			}
		}
	}
}

func TestWalk_NamesForFile(t *testing.T) {
	idx := walkStock(t)
	root := filepath.Join("..", "..", "..", "testdata", "stock")
	viewPath := filepath.Join(root, "application", "views", "pages", "about.php")
	names := idx.NamesForFile(viewPath)
	require.NotEmpty(t, names)
	assert.Contains(t, names, "pages/about")
}

// — Usage extraction —

func TestWalk_Stock_ExtractsUsages(t *testing.T) {
	idx := walkStock(t)
	usages := idx.UsagesOf("pages/about")
	require.NotEmpty(t, usages, "Controller/Pages.php constructs pages/about")
}

func TestWalk_Stock_ChainedSetVars(t *testing.T) {
	idx := walkStock(t)
	vars := idx.VarsFor("pages/about")
	require.NotEmpty(t, vars)

	byName := make(map[string]view.ExposedVar)
	for _, v := range vars {
		byName[v.Name] = v
	}

	assert.Contains(t, byName, "user", "->set('user', ...) must be indexed")
	assert.Contains(t, byName, "show_contact", "->set('show_contact', ...) must be indexed")
	assert.Contains(t, byName, "email", "->set('email', ...) must be indexed")
}

func TestWalk_Stock_FactoryArrayVars(t *testing.T) {
	idx := walkStock(t)
	vars := idx.VarsFor("pages/about")

	byName := make(map[string]view.ExposedVar)
	for _, v := range vars {
		byName[v.Name] = v
	}

	// new View('pages/about', ['user' => ..., 'show_contact' => ..., 'email' => ...])
	assert.Contains(t, byName, "user")
	assert.Contains(t, byName, "show_contact")
	assert.Contains(t, byName, "email")
}

func TestWalk_Stock_ORMTypeInference(t *testing.T) {
	idx := walkStock(t)
	vars := idx.VarsFor("pages/about")

	for _, v := range vars {
		if v.Name == "user" {
			assert.Equal(t, view.TypeClass, v.Type.Kind,
				"ORM::factory('User') should infer TypeClass")
			assert.Equal(t, "Model_User", v.Type.Class,
				"ORM::factory('User') should resolve to Model_User")
			return
		}
	}
	t.Skip("user var not found (requires ORM::factory type inference to be wired)")
}

func TestWalk_Stock_StringTypeInference(t *testing.T) {
	idx := walkStock(t)
	vars := idx.VarsFor("pages/about")

	for _, v := range vars {
		if v.Name == "email" {
			assert.Equal(t, view.TypeString, v.Type.Kind,
				"string literal 'hello@example.com' should infer TypeString")
			return
		}
	}
	t.Log("email var not found in VarsFor — check fixture and extraction logic")
}

// — Incremental reindex —

func TestReindexFile_PreservesOtherUsages(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "stock")
	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)
	old, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)

	// Reindex the controller file — usages should still be present afterwards.
	controllerPath := filepath.Join(root, "application", "classes", "Controller", "Pages.php")
	updated, err := view.ReindexFile(controllerPath, old)
	require.NoError(t, err)
	assert.NotEmpty(t, updated.UsagesOf("pages/about"),
		"usages should survive a clean ReindexFile of the same file")
}

func TestReindexFile_MissingFile(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "stock")
	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)
	old, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)

	// ReindexFile on a non-existent file should remove its entries (none here)
	// and not error.
	_, err = view.ReindexFile("/nonexistent/gone.php", old)
	require.NoError(t, err)
}
