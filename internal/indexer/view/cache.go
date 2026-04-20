package view

import (
	"encoding/gob"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// cacheVersion must be incremented whenever cacheData or any type it embeds
// changes in a way that would make an old cache unreadable.
const cacheVersion = 1

// cacheData is the on-disk representation of a ViewIndex plus the metadata
// required to validate that it is still current.
type cacheData struct {
	Version     int
	ConfigHash  string
	Manifest    []statEntry
	Definitions []ViewDefinition
	Usages      []ViewUsage
	Globals     []ExposedVar
}

// statEntry records the path and modification time of a file or directory
// observed during Walk. A changed mtime means the cache is stale.
type statEntry struct {
	Path    string
	ModTime time.Time
}

// cacheDir returns the directory where the cache file lives.
func cacheDir(root string) string {
	return filepath.Join(root, ".cache", "koseven-ls")
}

// cachePath returns the absolute path of the index cache file.
func cachePath(root string) string {
	return filepath.Join(cacheDir(root), "index.gob")
}

// configHash returns a cheap fingerprint of the config and module list.
// Any change that would produce a different index must change this hash.
func configHash(cfg config.Config, modules []project.Module) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%+v", cfg)
	for _, m := range modules {
		fmt.Fprintf(h, "%s:%s", m.Name, m.Path)
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

// tryLoadCache loads the cached index when it exists and its manifest is still
// valid. Returns nil if the cache is absent, corrupt, version-mismatched, or
// stale (any manifest entry has a different mtime than on disk).
func tryLoadCache(root string, cfg config.Config, modules []project.Module) *ViewIndex {
	f, err := os.Open(cachePath(root))
	if err != nil {
		return nil
	}
	defer f.Close()

	var cd cacheData
	if err := gob.NewDecoder(f).Decode(&cd); err != nil {
		return nil
	}
	if cd.Version != cacheVersion {
		return nil
	}
	if cd.ConfigHash != configHash(cfg, modules) {
		return nil
	}
	for _, e := range cd.Manifest {
		info, err := os.Stat(e.Path)
		if err != nil || !info.ModTime().Equal(e.ModTime) {
			return nil
		}
	}

	idx := NewViewIndex()
	for _, def := range cd.Definitions {
		idx.addDefinition(def)
	}
	for _, u := range cd.Usages {
		idx.addUsage(u)
	}
	for _, g := range cd.Globals {
		idx.addGlobal(g)
	}
	return idx
}

// saveCache serialises idx and the walk manifest to disk. Written to a
// temporary file first, then renamed atomically to avoid corrupt reads.
// Must be called after Walk completes; safe to run in a goroutine.
func saveCache(root string, cfg config.Config, modules []project.Module, idx *ViewIndex, manifest []statEntry) {
	if err := os.MkdirAll(cacheDir(root), 0o755); err != nil {
		return
	}

	idx.mu.RLock()
	var defs []ViewDefinition
	for _, ds := range idx.byName {
		defs = append(defs, ds...)
	}
	var usages []ViewUsage
	for _, us := range idx.usages {
		usages = append(usages, us...)
	}
	globals := make([]ExposedVar, len(idx.globals))
	copy(globals, idx.globals)
	idx.mu.RUnlock()

	cd := cacheData{
		Version:     cacheVersion,
		ConfigHash:  configHash(cfg, modules),
		Manifest:    manifest,
		Definitions: defs,
		Usages:      usages,
		Globals:     globals,
	}

	tmp := cachePath(root) + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	if err := gob.NewEncoder(f).Encode(cd); err != nil {
		f.Close()
		os.Remove(tmp)
		return
	}
	f.Close()
	os.Rename(tmp, cachePath(root)) //nolint:errcheck — best-effort, non-fatal
}
