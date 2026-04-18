package view

import "sync"

// ViewIndex is the concrete, thread-safe implementation of Index.
// The byName and byPath maps (view file definitions) are rebuilt only by
// Walk or a full reindex. The usages map is updated per-file by ReindexFile.
type ViewIndex struct {
	mu sync.RWMutex

	byName  map[string][]ViewDefinition // viewName → cascade-ordered definitions
	byPath  map[string][]string         // absolute view file path → []viewName
	usages  map[string][]ViewUsage      // viewName → []ViewUsage
	globals []ExposedVar                // from View::set_global / View::bind_global
}

// NewViewIndex returns an empty, ready-to-use ViewIndex.
func NewViewIndex() *ViewIndex {
	return &ViewIndex{
		byName: make(map[string][]ViewDefinition),
		byPath: make(map[string][]string),
		usages: make(map[string][]ViewUsage),
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
	}
	for name, us := range idx.usages {
		filtered := us[:0:0]
		for _, u := range us {
			if u.File != path {
				filtered = append(filtered, u)
			}
		}
		if len(filtered) > 0 {
			next.usages[name] = filtered
		}
	}
	return next
}
