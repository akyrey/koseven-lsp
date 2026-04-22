package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all server configuration loaded from koseven-ls.toml.
// All fields have sensible defaults for a stock Koseven layout.
type Config struct {
	// ApplicationPath is the path to the application directory, relative to
	// the project root. Default: "application".
	ApplicationPath string `toml:"application_path"`

	// SystemPath is the path to the system (core) directory, relative to the
	// project root. Default: "system".
	SystemPath string `toml:"system_path"`

	// ModulesPath is the root that contains module directories. Default: "modules".
	ModulesPath string `toml:"modules_path"`

	// ExtraViewRoots are additional absolute or project-relative paths to
	// include as view roots in the cascade.
	ExtraViewRoots []string `toml:"extra_view_roots"`

	Diagnostics  DiagnosticsConfig `toml:"diagnostics"`
	RouteHelpers []RouteHelper     `toml:"route_helpers"`
}

// RouteHelper registers a custom function or static method that takes
// controller and/or action as positional string arguments, enabling
// go-to-definition navigation from those call sites.
// Use nil pointer fields to indicate "this arg is not a controller/action/directory".
//
// Example koseven-ls.toml entry:
//
//	[[route_helpers]]
//	name       = "Skp_Helper::getWidget"
//	controller = 0
//	action     = 1
type RouteHelper struct {
	// Name is "ClassName::method" for static calls, or a bare function name.
	Name string `toml:"name"`
	// Controller is the 0-based argument index that holds the controller name.
	// Nil means this helper does not carry a controller argument.
	Controller *int `toml:"controller"`
	// Action is the 0-based argument index that holds the action name.
	// Nil means this helper does not carry an action argument.
	Action *int `toml:"action"`
	// Directory is the 0-based argument index that holds the HMVC directory.
	// Nil means this helper does not carry a directory argument.
	Directory *int `toml:"directory"`
}

// DiagnosticsConfig holds diagnostic feature toggles.
type DiagnosticsConfig struct {
	// MissingViews emits a diagnostic when View::factory('x') resolves to no
	// file. Disabled by default to avoid noise during file moves.
	MissingViews bool `toml:"missing_views"`
}

// Defaults returns sensible configuration for a stock Koseven layout.
func Defaults() Config {
	return Config{
		ApplicationPath: "application",
		SystemPath:      "system",
		ModulesPath:     "modules",
	}
}

// Load reads koseven-ls.toml from projectRoot, merging over Defaults().
// Returns Defaults() when the file is absent (zero-config case).
func Load(projectRoot string) (Config, error) {
	cfg := Defaults()
	tomlPath := filepath.Join(projectRoot, "koseven-ls.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(tomlPath, &cfg); err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}
