package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeICO(t *testing.T) {
	pngs, err := frames()
	if err != nil {
		t.Fatal(err)
	}
	data := encodeICO(pngs)

	r := bytes.NewReader(data)
	var dir iconDir
	if err := binary.Read(r, binary.LittleEndian, &dir); err != nil {
		t.Fatal(err)
	}
	if dir.Reserved != 0 || dir.Type != 1 || int(dir.Count) != len(sizes) {
		t.Fatalf("header = %+v, want Reserved 0, Type 1, Count %d", dir, len(sizes))
	}
	for _, size := range sizes {
		var e iconDirEntry
		if err := binary.Read(r, binary.LittleEndian, &e); err != nil {
			t.Fatal(err)
		}
		wantDim := byte(size)
		if size >= 256 {
			wantDim = 0
		}
		if e.Width != wantDim || e.Height != wantDim || e.Planes != 1 || e.BitCount != 32 {
			t.Errorf("%dpx entry = %+v", size, e)
		}
		end := int(e.ImageOffset) + int(e.BytesInRes)
		if end > len(data) {
			t.Fatalf("%dpx frame runs past the end of the file (%d > %d)", size, end, len(data))
		}
		frame, err := png.Decode(bytes.NewReader(data[e.ImageOffset:end]))
		if err != nil {
			t.Fatalf("%dpx frame is not a valid PNG: %v", size, err)
		}
		if b := frame.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Errorf("%dpx frame decodes as %dx%d", size, b.Dx(), b.Dy())
		}
	}
}

func TestGlyphHasEveryPart(t *testing.T) {
	img := render(64)
	// At 64px one pixel is 8 design units: the left fader's knob is
	// centred at (11, 38), the middle rail runs down x = 32.
	if c := img.NRGBAAt(0, 0); c.A != 0 {
		t.Errorf("corner = %v, want transparent", c)
	}
	if c := img.NRGBAAt(11, 38); c != handleColor {
		t.Errorf("left knob = %v, want %v", c, handleColor)
	}
	if c := img.NRGBAAt(32, 52); c != railColor {
		t.Errorf("middle rail = %v, want %v", c, railColor)
	}
	// Just left of the left knob's edge (x = 11 - 7.75) is outline.
	if c := img.NRGBAAt(2, 38); c != outlineColor {
		t.Errorf("outline = %v, want %v", c, outlineColor)
	}
}

// TestCommittedIconsAreCurrent fails if the glyph changed but the
// committed icon files weren't regenerated (see this package's doc
// comment): assets/icons/icon.ico must be exactly what genicon writes,
// and since rsrc stores each PNG frame verbatim, every frame must also
// appear in each program's .syso.
func TestCommittedIconsAreCurrent(t *testing.T) {
	pngs, err := frames()
	if err != nil {
		t.Fatal(err)
	}
	ico, err := os.ReadFile("../../assets/icons/icon.ico")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ico, encodeICO(pngs)) {
		t.Error("assets/icons/icon.ico is stale; regenerate it with go run ./tools/genicon")
	}

	cmdDirs, err := filepath.Glob("../../cmd/*")
	if err != nil || len(cmdDirs) == 0 {
		t.Fatalf("no cmd directories found: %v", err)
	}
	for _, dir := range cmdDirs {
		syso := filepath.Join(dir, "rsrc_windows_amd64.syso")
		data, err := os.ReadFile(syso)
		if err != nil {
			t.Errorf("%s has no icon resource: %v", dir, err)
			continue
		}
		for i, f := range pngs {
			if !bytes.Contains(data, f) {
				t.Errorf("%s is stale: its %dpx frame doesn't match genicon's output; regenerate it", syso, sizes[i])
			}
		}
	}
}
