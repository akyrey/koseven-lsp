package lsp

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// ─── isViewDefinitionPath ─────────────────────────────────────────────────────

func stockViewRoots(t *testing.T) []project.ViewRoot {
	t.Helper()
	cfg := config.Defaults()
	modules, _ := project.ParseModules(stockRoot, cfg)
	return project.BuildViewRoots(stockRoot, cfg, modules)
}

func TestIsViewDefinitionPath_ViewFile(t *testing.T) {
	viewRoots := stockViewRoots(t)
	path := filepath.Join(stockRoot, "application", "views", "pages", "about.php")
	assert.True(t, isViewDefinitionPath(path, viewRoots),
		"a .php file inside a views/ directory must be a view definition path")
}

func TestIsViewDefinitionPath_ControllerFile(t *testing.T) {
	viewRoots := stockViewRoots(t)
	path := filepath.Join(stockRoot, "application", "classes", "Controller", "Pages.php")
	assert.False(t, isViewDefinitionPath(path, viewRoots),
		"a controller file must not be treated as a view definition path")
}

func TestIsViewDefinitionPath_NonPHP(t *testing.T) {
	viewRoots := stockViewRoots(t)
	path := filepath.Join(stockRoot, "application", "views", "pages", "about.html")
	assert.False(t, isViewDefinitionPath(path, viewRoots),
		"non-.php files must not be treated as view definition paths")
}

func TestIsViewDefinitionPath_OutsideRoot(t *testing.T) {
	viewRoots := stockViewRoots(t)
	assert.False(t, isViewDefinitionPath("/tmp/random.php", viewRoots))
}
