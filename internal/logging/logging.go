// Package logging decides how verbose the server's stderr logging is.
//
// Editors record everything a language server writes to stderr; Neovim, for
// one, logs all of it at ERROR level. Routine INFO messages (one per
// incremental reindex) therefore grow the editor's log without bound, so the
// default only lets warnings and errors through.
package logging

import (
	"strconv"
	"strings"
)

// EnvVerbosity names the environment variable that overrides the verbosity.
const EnvVerbosity = "KOSEVEN_LSP_LOG_VERBOSITY"

// commonlog verbosity levels: -4 none, -3 critical, -2 error, -1 warning,
// 0 notice, 1 info, 2 debug.
const (
	MinVerbosity     = -4
	MaxVerbosity     = 2
	DefaultVerbosity = -1
)

// Verbosity returns the commonlog verbosity from EnvVerbosity, read through
// getenv (os.Getenv in production). Unset or non-numeric values fall back to
// DefaultVerbosity; out-of-range values are clamped.
func Verbosity(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv(EnvVerbosity))
	if raw == "" {
		return DefaultVerbosity
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return DefaultVerbosity
	}
	return min(max(v, MinVerbosity), MaxVerbosity)
}
