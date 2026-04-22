package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akyrey/koseven-lsp/internal/config"
)

func intPtr(n int) *int { return &n }

func TestLoad_RouteHelpers_Parsed(t *testing.T) {
	dir := t.TempDir()
	toml := `
[[route_helpers]]
name       = "Skp_Helper::getWidget"
controller = 0
action     = 1

[[route_helpers]]
name       = "my_function"
controller = 2
action     = 3
directory  = 4
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "koseven-ls.toml"), []byte(toml), 0o644))

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	require.Len(t, cfg.RouteHelpers, 2)

	h0 := cfg.RouteHelpers[0]
	assert.Equal(t, "Skp_Helper::getWidget", h0.Name)
	assert.Equal(t, intPtr(0), h0.Controller)
	assert.Equal(t, intPtr(1), h0.Action)
	assert.Nil(t, h0.Directory)

	h1 := cfg.RouteHelpers[1]
	assert.Equal(t, "my_function", h1.Name)
	assert.Equal(t, intPtr(2), h1.Controller)
	assert.Equal(t, intPtr(3), h1.Action)
	assert.Equal(t, intPtr(4), h1.Directory)
}

func TestLoad_RouteHelpers_EmptyByDefault(t *testing.T) {
	cfg := config.Defaults()
	assert.Empty(t, cfg.RouteHelpers)
}

func TestLoad_RouteHelpers_AbsentToml(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, cfg.RouteHelpers)
}
