package phputil

import "github.com/VKCOM/php-parser/pkg/position"

// Location is a source position used as a jump target or reference.
// Line numbers are 1-indexed. StartByte and EndByte are file-level byte offsets
// matching VKCOM parser conventions (EndByte is exclusive, like LSP range ends).
type Location struct {
	Path      string
	StartLine int
	StartByte int
	EndLine   int
	EndByte   int
}

// Zero reports whether the location is unset.
func (l Location) Zero() bool { return l.Path == "" }

// FromPosition builds a Location from a VKCOM parser position.
func FromPosition(path string, pos *position.Position) Location {
	if pos == nil {
		return Location{Path: path}
	}
	return Location{
		Path:      path,
		StartLine: pos.StartLine,
		StartByte: pos.StartPos,
		EndLine:   pos.EndLine,
		EndByte:   pos.EndPos,
	}
}
