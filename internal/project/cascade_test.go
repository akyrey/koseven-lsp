package project_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/project"
)

func TestFindCascadeFiles_Stock(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "stock")
	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)

	// Model/User.php exists in application/classes/
	paths := project.FindCascadeFiles(root, cfg, modules, "classes", "Model/User.php")
	require.Len(t, paths, 1)
	assert.Contains(t, paths[0], filepath.Join("application", "classes", "Model", "User.php"))
}

func TestFindCascadeFiles_NotFound(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "stock")
	cfg := config.Defaults()
	modules, _ := project.ParseModules(root, cfg)

	paths := project.FindCascadeFiles(root, cfg, modules, "classes", "Model/Ghost.php")
	assert.Empty(t, paths)
}

func TestFindCascadeFiles_HMVC_ModuleFirst(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "hmvc")
	cfg := config.Defaults()
	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)

	// Model/Post.php only exists in the blog module
	paths := project.FindCascadeFiles(root, cfg, modules, "classes", "Model/Post.php")
	require.Len(t, paths, 1)
	assert.Contains(t, paths[0], filepath.Join("blog", "classes", "Model", "Post.php"))
}

func TestFindCascadeFiles_I18n(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "stock")
	cfg := config.Defaults()
	modules, _ := project.ParseModules(root, cfg)

	paths := project.FindCascadeFiles(root, cfg, modules, "i18n", "en.php")
	require.Len(t, paths, 1)
	assert.Contains(t, paths[0], "en.php")
}

func TestClassNameToRelPath(t *testing.T) {
	tests := []struct {
		className string
		want      string
	}{
		{"Model_User", "Model/User.php"},
		{"Model_Member_Profile", "Model/Member/Profile.php"},
		{"Controller_Pages", "Controller/Pages.php"},
		{"Kohana_View", "Kohana/View.php"},
		{"ORM", "ORM.php"},
	}
	for _, tc := range tests {
		t.Run(tc.className, func(t *testing.T) {
			assert.Equal(t, tc.want, project.ClassNameToRelPath(tc.className))
		})
	}
}
