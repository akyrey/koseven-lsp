package view

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/VKCOM/php-parser/pkg/visitor/traverser"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/phpparse"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// Walk builds a complete ViewIndex for the project in two phases:
//
//  1. View discovery: walk each root in the cascade and record every .php file
//     as a ViewDefinition keyed by its logical name.
//  2. Usage extraction: walk all PHP source files and extract ViewUsage entries
//     by finding View::factory, new View, and Kohana::find_file('views', ...)
//     call sites with their chained set/bind variable exposures.
//
// Both phases respect .gitignore files found at root and inside walked
// directories, so vendor trees and generated code are skipped automatically.
// The resulting index is cached to disk; subsequent calls return the cached
// index instantly when no file in the manifest has changed.
func Walk(root string, cfg config.Config, modules []project.Module) (*ViewIndex, error) {
	if idx := tryLoadCache(root, cfg, modules); idx != nil {
		return idx, nil
	}

	viewRoots := project.BuildViewRoots(root, cfg, modules)
	idx := NewViewIndex()

	// Seed gitignore matchers from the project root. Nested .gitignore files are
	// picked up dynamically as each walk descends into subdirectories.
	initMatchers := rootGitIgnores(root)

	// manifest accumulates stat entries for every file and directory visited so
	// the cache can detect staleness on next startup.
	var manifest []statEntry

	// Phase 1: discover view definitions.
	for _, vr := range viewRoots {
		if err := discoverViews(vr, idx, initMatchers, &manifest); err != nil {
			return nil, fmt.Errorf("view discovery %s: %w", vr.Path, err)
		}
	}

	// Phase 2: extract view usages from PHP source files.
	for _, dir := range project.PHPScanDirs(root, cfg, modules) {
		if err := extractDir(dir, idx, initMatchers, &manifest); err != nil {
			fmt.Fprintf(os.Stderr, "koseven-lsp: usage scan %s: %v\n", dir, err)
		}
	}

	go saveCache(root, cfg, modules, idx, manifest)

	return idx, nil
}

// rootGitIgnores loads the .gitignore file at root (if any). The Walk caller
// passes this slice into both sub-walks; additional .gitignore files found
// during descent are appended inside the walk closures.
func rootGitIgnores(root string) []ignoreEntry {
	if e := tryLoadIgnoreFile(filepath.Join(root, ".gitignore")); e != nil {
		return []ignoreEntry{*e}
	}
	return nil
}

// appendStat adds a statEntry for path using the DirEntry info (no extra stat).
func appendStat(manifest *[]statEntry, path string, d fs.DirEntry) {
	info, err := d.Info()
	if err != nil {
		return
	}
	*manifest = append(*manifest, statEntry{Path: path, ModTime: info.ModTime()})
}

// ReindexFile performs an incremental update for a single changed PHP file.
// It clones the usage map from old, removes entries contributed by path, re-parses
// the file, and re-inserts the new usages. The definition maps (byName/byPath) are
// shared unchanged — they are only rebuilt by a full Walk when view files
// themselves are added or removed.
//
// Note: changes to View::set_global / View::bind_global in path will NOT be
// reflected until a full Walk; globals are not tracked per-file.
//
// Returns old unchanged on parse error so the caller can keep the last good state.
func ReindexFile(path string, old *ViewIndex) (*ViewIndex, error) {
	if old == nil {
		return nil, fmt.Errorf("view: nil index passed to ReindexFile")
	}
	next := old.withoutFile(path)

	src, err := os.ReadFile(path)
	if err != nil {
		// File deleted — removal of old entries is sufficient.
		return next, nil
	}
	astRoot, err := phpparse.Bytes(src, path)
	if err != nil {
		return next, nil
	}

	ev := newExtractVisitor(path, src)
	traverser.NewTraverser(ev).Traverse(astRoot)

	for _, u := range ev.usages {
		next.addUsage(u)
	}
	// Globals are not updated here; see function doc.

	return next, nil
}

// discoverViews walks vr.Path and records every .php file as a ViewDefinition.
// Missing directories are silently skipped (modules without a views/ dir are valid).
// Directories and files matched by any gitignore in matchers are skipped.
// Every visited entry is appended to manifest for cache validation.
func discoverViews(vr project.ViewRoot, idx *ViewIndex, initMatchers []ignoreEntry, manifest *[]statEntry) error {
	if _, err := os.Stat(vr.Path); os.IsNotExist(err) {
		return nil
	}

	matchers := append([]ignoreEntry(nil), initMatchers...)

	return filepath.WalkDir(vr.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		appendStat(manifest, path, d)
		if d.IsDir() {
			if isIgnored(matchers, path) {
				return filepath.SkipDir
			}
			if e := tryLoadIgnoreFile(filepath.Join(path, ".gitignore")); e != nil {
				matchers = append(matchers, *e)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".php") || isIgnored(matchers, path) {
			return nil
		}
		rel, err := filepath.Rel(vr.Path, path)
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, ".php"))
		idx.addDefinition(ViewDefinition{
			Name:       name,
			Path:       path,
			RootKind:   vr.Kind,
			RootOrder:  vr.Order,
			ModuleName: vr.ModuleName,
		})
		return nil
	})
}

// extractDir walks dir and extracts view usages from every .php file found.
// Parse errors are logged to stderr and skipped; they don't abort the walk.
// Directories and files matched by any gitignore in matchers are skipped.
// Every visited entry is appended to manifest for cache validation.
func extractDir(dir string, idx *ViewIndex, initMatchers []ignoreEntry, manifest *[]statEntry) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}

	matchers := append([]ignoreEntry(nil), initMatchers...)

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		appendStat(manifest, path, d)
		if d.IsDir() {
			if isIgnored(matchers, path) {
				return filepath.SkipDir
			}
			if e := tryLoadIgnoreFile(filepath.Join(path, ".gitignore")); e != nil {
				matchers = append(matchers, *e)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".php") || isIgnored(matchers, path) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "koseven-lsp: skipping %s: %v\n", path, err)
			return nil
		}
		astRoot, err := phpparse.Bytes(src, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "koseven-lsp: skipping %s: %v\n", path, err)
			return nil
		}
		usages, globals := ExtractFileUsages(path, astRoot, src)
		for _, u := range usages {
			idx.addUsage(u)
		}
		for _, g := range globals {
			idx.addGlobal(g)
		}
		return nil
	})
}
