package view

import (
	"sync"

	"github.com/akyrey/koseven-lsp/internal/indexer/route"
)

// ViewIndex is the concrete, thread-safe implementation of Index.
// The byName and byPath maps (view file definitions) are rebuilt only by
// Walk or a full reindex. The usages map is updated per-file by ReindexFile.
type ViewIndex struct {
	mu sync.RWMutex

	byName  map[string][]ViewDefinition // viewName → cascade-ordered definitions
	byPath  map[string][]string         // absolute view file path → []viewName
	usages  map[string][]ViewUsage      // viewName → []ViewUsage
	globals []ExposedVar                // from View::set_global / View::bind_global

	controllerRefs map[string][]ControllerRef // route.ClassKey(class) → call sites
}

// NewViewIndex returns an empty, ready-to-use ViewIndex.
func NewViewIndex() *ViewIndex {
	return &ViewIndex{
		byName: make(map[string][]ViewDefinition),
		byPath: make(map[string][]string),
		usages: make(map[string][]ViewUsage),

		controllerRefs: make(map[string][]ControllerRef),
	}
}

// Resolve implements Index.
func (idx *ViewIndex) Resolve(viewName string) []ViewDefinition {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.byName[viewName]
}

// UsagesOf implements Index.
func (idx *ViewIndex) UsagesOf(viewName string) []ViewUsage {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.usages[viewName]
}

// VarsFor implements Index. Returns the union of exposed vars across all call
// sites that construct viewName, plus any globally exposed vars.
// When the same variable name appears at multiple sites, the first occurrence
// (by index insertion order) wins.
func (idx *ViewIndex) VarsFor(viewName string) []ExposedVar {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	seen := make(map[string]struct{})
	var vars []ExposedVar

	for _, u := range idx.usages[viewName] {
		for _, v := range u.ExposedVars {
			if _, ok := seen[v.Name]; ok {
				continue
			}
			seen[v.Name] = struct{}{}
			vars = append(vars, v)
		}
	}
	for _, g := range idx.globals {
		if _, ok := seen[g.Name]; ok {
			continue
		}
		seen[g.Name] = struct{}{}
		vars = append(vars, g)
	}
	return vars
}

// NamesForFile implements Index.
func (idx *ViewIndex) NamesForFile(path string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.byPath[path]
}

// AllDefinitions implements Index.
func (idx *ViewIndex) AllDefinitions() []ViewDefinition {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var all []ViewDefinition
	for _, defs := range idx.byName {
		all = append(all, defs...)
	}
	return all
}

// ControllerRefs implements Index.
func (idx *ViewIndex) ControllerRefs(className string) []ControllerRef {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.controllerRefs[route.ClassKey(className)]
}

// addDefinition adds a view definition. Not safe for concurrent use; call
// only during index construction before the index is shared.
func (idx *ViewIndex) addDefinition(def ViewDefinition) {
	idx.byName[def.Name] = append(idx.byName[def.Name], def)
	idx.byPath[def.Path] = append(idx.byPath[def.Path], def.Name)
}

// addUsage records a view usage. Not safe for concurrent use; call only
// during index construction or under external synchronisation.
func (idx *ViewIndex) addUsage(u ViewUsage) {
	idx.usages[u.Name] = append(idx.usages[u.Name], u)
}

// addControllerRef records a controller/action call site. Same concurrency
// rules as addUsage.
func (idx *ViewIndex) addControllerRef(r ControllerRef) {
	key := route.ClassKey(r.Class)
	idx.controllerRefs[key] = append(idx.controllerRefs[key], r)
}

// addGlobal records a globally exposed variable.
func (idx *ViewIndex) addGlobal(v ExposedVar) {
	idx.globals = append(idx.globals, v)
}

// withoutFile returns a shallow clone of the usage index with all entries
// contributed by path removed. The definition maps (byName/byPath) are shared
// because view file creation/deletion requires a full Walk to update them.
// Globals are not updated; callers that modify set_global calls should trigger
// a full reindex.
func (idx *ViewIndex) withoutFile(path string) *ViewIndex {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	next := &ViewIndex{
		byName:  idx.byName,
		byPath:  idx.byPath,
		globals: idx.globals,
		usages:  make(map[string][]ViewUsage, len(idx.usages)),

		controllerRefs: make(map[string][]ControllerRef, len(idx.controllerRefs)),
	}
	for name, us := range idx.usages {
		if filtered := filterByFile(us, path, func(u ViewUsage) string { return u.File }); len(filtered) > 0 {
			next.usages[name] = filtered
		}
	}
	for key, refs := range idx.controllerRefs {
		if filtered := filterByFile(refs, path, func(r ControllerRef) string { return r.File }); len(filtered) > 0 {
			next.controllerRefs[key] = filtered
		}
	}
	return next
}

// filterByFile returns a new slice holding the items whose file is not path.
func filterByFile[T any](items []T, path string, fileOf func(T) string) []T {
	filtered := items[:0:0]
	for _, it := range items {
		if fileOf(it) != path {
			filtered = append(filtered, it)
		}
	}
	return filtered
}
