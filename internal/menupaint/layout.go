// Package menupaint draws the tray menu's device entries itself, so that
// a device left out of cycling can be shown faded - in the colour
// Windows uses for unavailable text - while staying clickable. Windows
// offers no other way round: MF_GRAYED greys a menu item out but
// swallows its click along with it.
//
// This file holds the arithmetic, so it can be tested anywhere; the
// Win32 side lives in menupaint_windows.go.
package menupaint

// rect mirrors the Win32 RECT: right and bottom are exclusive.
type rect struct {
	left, top, right, bottom int32
}

func (r rect) width() int32  { return r.right - r.left }
func (r rect) height() int32 { return r.bottom - r.top }

// metrics are the system measurements an item's layout is built from:
// the check-mark column Windows reserves on the left of every popup
// item, and the size of the item's own text in the menu font.
type metrics struct {
	checkWidth  int32 // SM_CXMENUCHECK
	checkHeight int32 // SM_CYMENUCHECK
	textWidth   int32
	textHeight  int32
}

// Padding around an owner-drawn item, in pixels. These make our entries
// line up with the ones Windows still draws itself (Configure, Exit), so
// they are the numbers to touch if the text sits off by a pixel or two.
const (
	gutterPad = 4 // between the check-mark column and the text
	rightPad  = 20
	itemPadY  = 2

	// Windows centers a label a pixel higher in the item than DrawText's
	// DT_VCENTER does, so the box the label is centered in is lifted by
	// one to land in the same place as the entries Windows draws itself.
	textLift = 1
)

// itemSize is the size an entry reports in WM_MEASUREITEM: wide enough
// for the check column and the text, tall enough for the taller of the
// text and a check mark.
func itemSize(m metrics) (width, height int32) {
	width = m.checkWidth + gutterPad + m.textWidth + rightPad
	height = max32(m.textHeight, m.checkHeight) + 2*itemPadY
	return width, height
}

// layout splits the rectangle handed to WM_DRAWITEM into the check-mark
// box and the area the label is drawn in.
func layout(item rect, m metrics) (check, text rect) {
	check = rect{
		left:   item.left,
		top:    item.top + (item.height()-m.checkHeight)/2,
		right:  item.left + m.checkWidth,
		bottom: item.top + (item.height()-m.checkHeight)/2 + m.checkHeight,
	}
	text = rect{
		left:   item.left + m.checkWidth + gutterPad,
		top:    item.top - textLift,
		right:  item.right - rightPad,
		bottom: item.bottom - textLift,
	}
	if text.right < text.left {
		text.right = text.left
	}
	return check, text
}

// fade mixes a colour percent of the way toward another, channel by
// channel. It's how a skipped entry stays visibly faded while the pointer
// is on it: plain grey on the selection bar is barely readable, so the
// label is washed out toward the bar's own colour instead. Colours are
// COLORREFs (0x00BBGGRR), but the arithmetic is per byte, so the channel
// order doesn't matter.
func fade(c, toward uint32, percent uint32) uint32 {
	var out uint32
	for shift := uint(0); shift <= 16; shift += 8 {
		a := (c >> shift) & 0xFF
		b := (toward >> shift) & 0xFF
		out |= (a*(100-percent) + b*percent) / 100 << shift
	}
	return out
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
