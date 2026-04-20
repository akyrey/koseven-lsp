package phputil

import (
	"bytes"
	"unicode/utf8"
)

// UTF16ColFromOffset computes the zero-based UTF-16 column for a file-level
// byte offset. lineNum is 1-based. Returns 0 on any out-of-range input.
func UTF16ColFromOffset(src []byte, lineNum int, fileOffset int) uint32 {
	if len(src) == 0 || fileOffset <= 0 {
		return 0
	}
	lineStart := 0
	for l := 1; l < lineNum; l++ {
		idx := bytes.IndexByte(src[lineStart:], '\n')
		if idx < 0 {
			return 0
		}
		lineStart += idx + 1
	}
	if fileOffset < lineStart || fileOffset > len(src) {
		return 0
	}
	return CountUTF16Units(src[lineStart:fileOffset])
}

// CountUTF16Units returns the number of UTF-16 code units for the UTF-8
// encoded bytes in b.
func CountUTF16Units(b []byte) uint32 {
	var n uint32
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
