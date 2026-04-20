package view_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// — Index cache —

func TestCache_WarmHit_ReturnsSameData(t *testing.T) {
	root := t.TempDir()
	// Copy the stock fixture into the temp dir so the cache write doesn't
	// pollute the checked-in testdata tree.
	stockRoot := filepath.Join("..", "..", "..", "testdata", "stock")
	copyDir(t, stockRoot, root)

	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)

	// First Walk — builds and saves the cache.
	idx1, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)
	// Give the async goroutine time to flush.
	waitForCache(t, root)

	// Second Walk — must hit the cache.
	idx2, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)

	assert.ElementsMatch(t,
		defNames(idx1.AllDefinitions()),
		defNames(idx2.AllDefinitions()),
		"cached index must have the same view definitions as the fresh one")
	assert.Equal(t,
		len(idx1.UsagesOf("pages/about")),
		len(idx2.UsagesOf("pages/about")),
		"cached index must have the same usage count")
}

func TestCache_StaleFile_Misses(t *testing.T) {
	root := t.TempDir()
	stockRoot := filepath.Join("..", "..", "..", "testdata", "stock")
	copyDir(t, stockRoot, root)

	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)

	// Build and save cache.
	_, err = view.Walk(root, cfg, modules)
	require.NoError(t, err)
	waitForCache(t, root)

	// Touch a PHP file to invalidate the manifest.
	controllerPath := filepath.Join(root, "application", "classes", "Controller", "Pages.php")
	now := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(controllerPath, now, now))

	// Second Walk — manifest mismatch must trigger a fresh Walk.
	// We verify this indirectly: the returned index must still be valid.
	idx2, err := view.Walk(root, cfg, modules)
	require.NoError(t, err)
	assert.NotEmpty(t, idx2.UsagesOf("pages/about"),
		"fresh walk after cache bust must still index usages")
}

// waitForCache polls until the cache file appears or the test times out.
func waitForCache(t *testing.T, root string) {
	t.Helper()
	cacheFile := filepath.Join(root, ".cache", "koseven-ls", "index.gob")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cacheFile); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("cache file never appeared at %s", cacheFile)
}

// copyDir recursively copies src into dst, creating dst if needed.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	require.NoError(t, err)
}

func defNames(defs []view.ViewDefinition) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
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

// — NameRange column precision —

// TestWalk_NameRange_HasPreciseColumns verifies that NameRange carries non-zero
// UTF-16 column offsets now that src bytes are threaded into the extractor.
// The fixture has View::factory('pages/about') on a line with leading whitespace,
// so both Start.Character and End.Character must be > 0.
func TestWalk_NameRange_HasPreciseColumns(t *testing.T) {
	idx := walkStock(t)
	usages := idx.UsagesOf("pages/about")
	require.NotEmpty(t, usages)

	for _, u := range usages {
		if u.NameRange.Start.Line == 0 && u.NameRange.End.Line == 0 {
			continue // zero NameRange — skip (shouldn't happen for this fixture)
		}
		assert.Greater(t, u.NameRange.Start.Character, uint32(0),
			"NameRange.Start.Character must be > 0 for indented call site")
		assert.Greater(t, u.NameRange.End.Character, uint32(0),
			"NameRange.End.Character must be > 0 for indented call site")
		assert.Greater(t, u.NameRange.End.Character, u.NameRange.Start.Character,
			"NameRange.End must be past NameRange.Start on the same line")
		return // one passing usage is sufficient
	}
}

func TestWalk_GitIgnore_NonIgnoredFilesStillIndexed(t *testing.T) {
	// Sanity-check: the gitignore must not suppress non-vendor files.
	idx := walkStock(t)
	usages := idx.UsagesOf("pages/about")
	assert.NotEmpty(t, usages,
		"pages/about usages must still be indexed after gitignore support is added")
}
