package project

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/akyrey/koseven-lsp/internal/config"
)

// CascadeBase is one root directory in the Koseven cascading filesystem.
type CascadeBase struct {
	Path       string
	Order      int
	ModuleName string // "" for application/system roots
}

// BuildCascadeBases returns the ordered list of base directories that form the
// Koseven cascade: application, then each enabled module (in declaration order),
// then system.
func BuildCascadeBases(root string, cfg config.Config, modules []Module) []CascadeBase {
	bases := make([]CascadeBase, 0, len(modules)+2)
	bases = append(bases, CascadeBase{
		Path:  filepath.Join(root, cfg.ApplicationPath),
		Order: 0,
	})
	for i, mod := range modules {
		bases = append(bases, CascadeBase{
			Path:       mod.Path,
			Order:      i + 1,
			ModuleName: mod.Name,
		})
	}
	bases = append(bases, CascadeBase{
		Path:  filepath.Join(root, cfg.SystemPath),
		Order: len(modules) + 1,
	})
	return bases
}

// FindCascadeFiles returns the absolute paths of files that exist at
// <cascadeBase>/<subdir>/<relPath> for each base in cascade order (highest
// priority first). Only existing paths are returned.
//
// Example: FindCascadeFiles(root, cfg, mods, "classes", "Model/User.php")
// searches application/classes/Model/User.php, then each module's, then system's.
func FindCascadeFiles(root string, cfg config.Config, modules []Module, subdir, relPath string) []string {
	bases := BuildCascadeBases(root, cfg, modules)
	found := make([]string, 0, 2)
	for _, base := range bases {
		candidate := filepath.Join(base.Path, subdir, relPath)
		if _, err := os.Stat(candidate); err == nil {
			found = append(found, candidate)
		}
	}
	return found
}

// ClassNameToRelPath converts a Kohana PSR-0 class name to a relative file path
// under the classes/ directory. Underscores become path separators.
//
//	Model_Member         → Model/Member.php
//	Model_Member_Profile → Model/Member/Profile.php
//	Controller_Pages     → Controller/Pages.php
//	Kohana_View          → Kohana/View.php
func ClassNameToRelPath(className string) string {
	return strings.ReplaceAll(className, "_", "/") + ".php"
}
