// Command genicon renders the app's icon - three mixing-console faders
// with a dark outline - as a multi-resolution .ico: the tray icon
// (assets/icons/icon.ico, embedded into the switcher) and the source of
// both exes' file icon resources. It has no OS dependency and runs on any
// platform.
//
// Usage, from the repository root:
//
//	go run ./tools/genicon assets/icons/icon.ico
//	go run github.com/akavel/rsrc@latest -arch amd64 -ico assets/icons/icon.ico -o cmd/switcher/rsrc_windows_amd64.syso
//	go run github.com/akavel/rsrc@latest -arch amd64 -ico assets/icons/icon.ico -manifest assets/setup.manifest -o cmd/setup/rsrc_windows_amd64.syso
//
// `go build` picks up a *_windows_amd64.syso file automatically. The
// tests fail if the committed files no longer match what this renders.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// sizes are the frames baked into the .ico, from the 16px tray icon up to
// Explorer's largest view.
var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

// The glyph is laid out on a design grid of this many units a side and
// scaled to each size.
const grid = 512.0

// Flat colours only. There's no background plate - the glyph sits on a
// transparent canvas so the tray's own background shows through - so the
// whole silhouette gets a dark outline to stay legible on light and dark
// taskbars alike.
var (
	railColor    = color.NRGBA{255, 255, 255, 255}
	handleColor  = color.NRGBA{46, 230, 168, 255} // mint accent for each fader's knob
	outlineColor = color.NRGBA{18, 18, 26, 255}
)

// Three fat, widely spaced faders read as a distinct icon even at 16px,
// where thinner or more numerous shapes blur into a smudge. The knobs sit
// at different heights purely for visual variety - a mixing console reads
// as "audio" partly because its faders are never all level.
var faderLevels = []float64{0.62, 0.28, 0.48}

const (
	railWidth   = 30.0
	railHalfLen = 200.0
	knobRadius  = 62.0
	spacing     = 168.0
	outline     = 17.0 // how far the outline reaches beyond the glyph
)

// samples is the supersampling factor per axis, for smooth edges.
const samples = 8

// colorAt returns the glyph's colour at design-grid point (x, y), with
// ok false where it's transparent.
func colorAt(x, y float64) (color.NRGBA, bool) {
	const c = grid / 2
	top := c - railHalfLen
	nearest := math.Inf(1) // distance to the closest shape
	for i, level := range faderLevels {
		fx := c + (float64(i)-float64(len(faderLevels)-1)/2)*spacing
		knobY := top + 2*railHalfLen*level
		knob := math.Hypot(x-fx, y-knobY) - knobRadius
		if knob <= 0 {
			return handleColor, true
		}
		// A rail with fully rounded ends is a capsule around its centre
		// line.
		r := railWidth / 2
		cy := math.Max(top+r, math.Min(c+railHalfLen-r, y))
		rail := math.Hypot(x-fx, y-cy) - r
		if rail <= 0 {
			return railColor, true
		}
		nearest = math.Min(nearest, math.Min(knob, rail))
	}
	if nearest <= outline {
		return outlineColor, true
	}
	return color.NRGBA{}, false
}

// render draws the glyph at size x size pixels, averaging samples x
// samples points per pixel.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := grid / float64(size) / samples
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					col, ok := colorAt((float64(px*samples+sx)+0.5)*unit, (float64(py*samples+sy)+0.5)*unit)
					if !ok {
						continue
					}
					r += float64(col.R)
					g += float64(col.G)
					b += float64(col.B)
					a++
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(r / a)),
				G: uint8(math.Round(g / a)),
				B: uint8(math.Round(b / a)),
				A: uint8(math.Round(255 * a / samples / samples)),
			})
		}
	}
	return img
}

// iconDir and iconDirEntry are the ICO container's header and per-frame
// directory records; encoding/binary writes their fields packed, in
// order, exactly as the format lays them out.
type iconDir struct {
	Reserved uint16
	Type     uint16 // 1 = icon
	Count    uint16
}

type iconDirEntry struct {
	Width, Height byte // 0 means 256
	ColorCount    byte
	Reserved      byte
	Planes        uint16
	BitCount      uint16
	BytesInRes    uint32
	ImageOffset   uint32
}

// frames renders every size as PNG.
func frames() ([][]byte, error) {
	var out [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, render(s)); err != nil {
			return nil, err
		}
		out = append(out, buf.Bytes())
	}
	return out, nil
}

// encodeICO packs PNG frames of the given sizes as a Vista+-style ICO.
func encodeICO(pngs [][]byte) []byte {
	// Writes to a bytes.Buffer never fail, so binary.Write's error is
	// safe to ignore below.
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, iconDir{Type: 1, Count: uint16(len(pngs))})

	offset := uint32(binary.Size(iconDir{}) + binary.Size(iconDirEntry{})*len(pngs))
	for i, f := range pngs {
		dim := byte(sizes[i])
		if sizes[i] >= 256 {
			dim = 0
		}
		binary.Write(&out, binary.LittleEndian, iconDirEntry{
			Width:       dim,
			Height:      dim,
			Planes:      1,
			BitCount:    32,
			BytesInRes:  uint32(len(f)),
			ImageOffset: offset,
		})
		offset += uint32(len(f))
	}
	for _, f := range pngs {
		out.Write(f)
	}
	return out.Bytes()
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genicon <output.ico>")
		os.Exit(1)
	}
	pngs, err := frames()
	if err == nil {
		err = os.WriteFile(os.Args[1], encodeICO(pngs), 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
