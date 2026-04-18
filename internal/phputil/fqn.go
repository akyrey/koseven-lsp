package phputil

// FQN is a fully-qualified PHP class name, e.g. "Model_Member".
// Koseven uses underscore-namespacing (PSR-0 style), not backslash namespaces.
// We keep this type compatible with both styles.
type FQN string

// UseMap maps import aliases to their FQN within a single file.
type UseMap map[string]FQN

// FileContext holds per-file resolution data needed to turn unqualified names
// and import aliases into fully-qualified names.
type FileContext struct {
	Path      string
	Namespace FQN
	Uses      UseMap
}

// Resolve turns a name as it appears in source into its FQN, using the file's
// namespace and use-statements.
func (fc *FileContext) Resolve(name string) FQN {
	if len(name) == 0 {
		return ""
	}
	if name[0] == '\\' {
		return FQN(name[1:])
	}
	firstSegment := name
	rest := ""
	for i, c := range name {
		if c == '\\' {
			firstSegment = name[:i]
			rest = name[i:]
			break
		}
	}
	if resolved, ok := fc.Uses[firstSegment]; ok {
		return FQN(string(resolved) + rest)
	}
	if fc.Namespace == "" {
		return FQN(name)
	}
	return FQN(string(fc.Namespace) + "\\" + name)
}
