package view

import (
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// ignoreEntry pairs a compiled .gitignore matcher with the directory it was
// loaded from. Patterns are evaluated relative to base so that inner
// .gitignore files only apply to their own subtree.
type ignoreEntry struct {
	base    string
	matcher *ignore.GitIgnore
}

// tryLoadIgnoreFile compiles the .gitignore at path. Returns nil on any error
// (missing file, unreadable, bad syntax) so callers can treat it as a no-op.
func tryLoadIgnoreFile(path string) *ignoreEntry {
	ig, err := ignore.CompileIgnoreFile(path)
	if err != nil {
		return nil
	}
	return &ignoreEntry{base: filepath.Dir(path), matcher: ig}
}

// isIgnored reports whether absPath should be excluded according to any of
// the accumulated gitignore matchers. A matcher only applies when its base
// directory is an ancestor of absPath (checked via filepath.Rel prefix).
func isIgnored(entries []ignoreEntry, absPath string) bool {
	for _, e := range entries {
		rel, err := filepath.Rel(e.base, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if e.matcher.MatchesPath(rel) {
			return true
		}
	}
	return false
}
