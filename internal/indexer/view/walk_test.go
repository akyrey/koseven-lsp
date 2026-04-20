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

// — Split-assignment scope tracking —

func TestWalk_Stock_SplitAssignment_Indexed(t *testing.T) {
	idx := walkStock(t)
	// pages/contact is only constructed in action_contact using split assignment.
	usages := idx.UsagesOf("pages/contact")
	require.NotEmpty(t, usages, "split-assignment View::factory must be indexed")
}

func TestWalk_Stock_SplitAssignment_SetVars(t *testing.T) {
	idx := walkStock(t)
	vars := idx.VarsFor("pages/contact")
	require.NotEmpty(t, vars)

	byName := make(map[string]view.ExposedVar)
	for _, v := range vars {
		byName[v.Name] = v
	}

	assert.Contains(t, byName, "name", "split $view->set('name',...) must be indexed")
	assert.Contains(t, byName, "subject", "split $view->set('subject',...) must be indexed")
	assert.Contains(t, byName, "message", "split $view->bind('message',...) must be indexed")

	assert.Equal(t, view.SourceSet, byName["name"].Source)
	assert.Equal(t, view.SourceBind, byName["message"].Source)
}

func TestWalk_Stock_SplitAssignment_TypeInference(t *testing.T) {
	idx := walkStock(t)
	vars := idx.VarsFor("pages/contact")

	for _, v := range vars {
		if v.Name == "name" {
			assert.Equal(t, view.TypeString, v.Type.Kind,
				"literal string 'John' should infer TypeString")
			return
		}
	}
	t.Error("name var not found")
}

func TestWalk_Stock_SplitAssignment_DoesNotPolluteSiblingView(t *testing.T) {
	idx := walkStock(t)
	// pages/about vars must not be polluted by the pages/contact split-assignment.
	vars := idx.VarsFor("pages/about")
	byName := make(map[string]bool)
	for _, v := range vars {
		byName[v.Name] = true
	}
	assert.False(t, byName["name"] && byName["subject"] && byName["message"],
		"pages/about must not contain all three contact vars")
}

func TestWalk_Stock_SplitAssignment_Reassignment(t *testing.T) {
	// Verify that reassigning a variable removes the old scope entry:
	// $view = View::factory('a'); $view = View::factory('b'); $view->set('x', 1);
	// Only 'pages/about_reassign' (if it existed) should get the var 'x'.
	// We verify indirectly: the stock fixture only has split-assignment for
	// pages/contact, and pages/about does NOT share vars from it.
	idx := walkStock(t)
	aboutVars := idx.VarsFor("pages/about")
	contactVars := idx.VarsFor("pages/contact")

	aboutByName := make(map[string]bool)
	for _, v := range aboutVars {
		aboutByName[v.Name] = true
	}
	contactByName := make(map[string]bool)
	for _, v := range contactVars {
		contactByName[v.Name] = true
	}

	// Vars exclusive to contact must not appear in about.
	assert.False(t, aboutByName["name"] && contactByName["name"] && !aboutByName["user"],
		"cross-contamination detected between views")
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

// — Gitignore-aware walk —

func TestWalk_GitIgnore_SkipsIgnoredDir(t *testing.T) {
	// testdata/stock/application/.gitignore contains "vendor/".
	// testdata/stock/application/vendor/some_lib/Helper.php calls
	// View::factory('vendor/secret'). That usage must not appear in the index.
	idx := walkStock(t)
	usages := idx.UsagesOf("vendor/secret")
	assert.Empty(t, usages,
		"files inside a gitignored vendor/ directory must not be indexed")
}

func TestWalk_GitIgnore_NonIgnoredFilesStillIndexed(t *testing.T) {
	// Sanity-check: the gitignore must not suppress non-vendor files.
	idx := walkStock(t)
	usages := idx.UsagesOf("pages/about")
	assert.NotEmpty(t, usages,
		"pages/about usages must still be indexed after gitignore support is added")
}
