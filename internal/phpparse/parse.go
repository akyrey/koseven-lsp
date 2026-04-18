package phpparse

import (
	"fmt"
	"os"

	"github.com/VKCOM/php-parser/pkg/ast"
	"github.com/VKCOM/php-parser/pkg/conf"
	phperrors "github.com/VKCOM/php-parser/pkg/errors"
	"github.com/VKCOM/php-parser/pkg/parser"
	"github.com/VKCOM/php-parser/pkg/version"
)

// php81 is the highest version the parser officially supports. Koseven runs on
// PHP 7.4, which is fully forward-compatible at the AST level.
var php81 = &version.Version{Major: 8, Minor: 1}

// Bytes parses src as PHP 8.1, logging parse errors to stderr without aborting.
func Bytes(src []byte, path string) (ast.Vertex, error) {
	cfg := conf.Config{
		Version: php81,
		ErrorHandlerFunc: func(e *phperrors.Error) {
			fmt.Fprintf(os.Stderr, "koseven-lsp: parse error in %s: %s\n", path, e.String())
		},
	}
	root, err := parser.Parse(src, cfg)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return root, nil
}

// File reads path from disk and parses it as PHP 8.1.
func File(path string) (ast.Vertex, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Bytes(src, path)
}
