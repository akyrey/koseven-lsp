package project_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/project"
)

func TestParseModules_Stock(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "stock")
	cfg := config.Defaults()

	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)
	assert.Empty(t, modules, "stock fixture has no enabled modules")
}

func TestParseModules_HMVC(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "hmvc")
	cfg := config.Defaults()

	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)
	require.Len(t, modules, 2, "hmvc fixture declares blog and auth modules")

	assert.Equal(t, "blog", modules[0].Name)
	assert.Equal(t, filepath.Join(root, "modules", "blog"), modules[0].Path)

	assert.Equal(t, "auth", modules[1].Name)
	assert.Equal(t, filepath.Join(root, "modules", "auth"), modules[1].Path)
}

func TestParseModules_OrderPreserved(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "hmvc")
	cfg := config.Defaults()

	modules, err := project.ParseModules(root, cfg)
	require.NoError(t, err)

	// Declaration order must match bootstrap.php order (blog before auth).
	if len(modules) >= 2 {
		assert.Equal(t, "blog", modules[0].Name, "blog must come before auth in cascade")
	}
}
