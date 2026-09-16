// Package icons bundles the application icon into the switcher binary so
// it stays a single self-contained .exe. icon.ico is also used by CI as
// the source for both .exe files' own icon resource (see
// .github/workflows/release.yml).
package icons

import (
	_ "embed"
	"encoding/binary"
)

//go:embed icon.ico
var Icon []byte

// Image returns the image data (PNG or BMP, as stored) of the smallest
// size in Icon that is at least size pixels wide, or the largest one if
// none is - what Windows' CreateIconFromResourceEx takes. ok is false if
// ico isn't a well-formed icon file.
func Image(ico []byte, size int) (data []byte, ok bool) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint16(ico[4:]))
	best, bestWidth := -1, 0
	for i := 0; i < count; i++ {
		entry := 6 + 16*i
		if len(ico) < entry+16 {
			return nil, false
		}
		width := int(ico[entry])
		if width == 0 { // 0 stands for 256
			width = 256
		}
		better := best < 0 ||
			(width >= size && (bestWidth < size || width < bestWidth)) ||
			(width < size && bestWidth < size && width > bestWidth)
		if better {
			best, bestWidth = entry, width
		}
	}
	if best < 0 {
		return nil, false
	}
	length := int(binary.LittleEndian.Uint32(ico[best+8:]))
	offset := int(binary.LittleEndian.Uint32(ico[best+12:]))
	if offset < 0 || length <= 0 || offset+length > len(ico) {
		return nil, false
	}
	return ico[offset : offset+length], true
}
