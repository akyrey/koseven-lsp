package project

import (
	"path/filepath"

	"github.com/akyrey/koseven-lsp/internal/config"
)

// BuildViewRoots computes the ordered list of view directories from the project
// root, enabled modules, and config. The order matches the Koseven cascade:
//
//  1. application/views (highest priority)
//  2. modules in declaration order (each module's views/ directory)
//  3. system/views (lowest priority)
func BuildViewRoots(root string, cfg config.Config, modules []Module) []ViewRoot {
	roots := make([]ViewRoot, 0, len(modules)+2)

	roots = append(roots, ViewRoot{
		Path:  filepath.Join(root, cfg.ApplicationPath, "views"),
		Kind:  RootApplication,
		Order: 0,
	})

	for i, mod := range modules {
		roots = append(roots, ViewRoot{
			Path:       filepath.Join(mod.Path, "views"),
			Kind:       RootModule,
			Order:      i + 1,
			ModuleName: mod.Name,
		})
	}

	roots = append(roots, ViewRoot{
		Path:  filepath.Join(root, cfg.SystemPath, "views"),
		Kind:  RootSystem,
		Order: len(modules) + 1,
	})

	return roots
}

// PHPScanDirs returns the directories to walk for PHP source files that may
// contain View::factory / new View / Kohana::find_file calls. This covers
// application/ and each module's directory (which includes both classes/ and
// views/, since views may nest other views via View::factory).
func PHPScanDirs(root string, cfg config.Config, modules []Module) []string {
	dirs := []string{
		filepath.Join(root, cfg.ApplicationPath),
	}
	for _, mod := range modules {
		dirs = append(dirs, mod.Path)
	}
	return dirs
}
