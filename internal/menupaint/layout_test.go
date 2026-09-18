package menupaint

import "testing"

// A typical Windows 11 popup at 100% scaling: a 15x15 check column and a
// 15-pixel-tall label.
var testMetrics = metrics{checkWidth: 15, checkHeight: 15, textWidth: 120, textHeight: 15}

func TestItemSize(t *testing.T) {
	w, h := itemSize(testMetrics)
	if want := int32(15 + gutterPad + 120 + rightPad); w != want {
		t.Errorf("width = %d, want %d (check column + gap + text + right padding)", w, want)
	}
	if want := int32(15 + 2*itemPadY); h != want {
		t.Errorf("height = %d, want %d", h, want)
	}
}

func TestItemSizeIsAtLeastACheckMarkTall(t *testing.T) {
	m := testMetrics
	m.textHeight = 4 // an empty or tiny label must not squash the item
	_, h := itemSize(m)
	if want := int32(m.checkHeight + 2*itemPadY); h != want {
		t.Errorf("height = %d, want %d: the check mark sets the floor", h, want)
	}
}

func TestLayoutCentersTheCheckAndIndentsTheText(t *testing.T) {
	item := rect{left: 0, top: 100, right: 200, bottom: 122}
	check, text := layout(item, testMetrics)

	if check.left != item.left || check.width() != testMetrics.checkWidth {
		t.Errorf("check box = %+v, want it at the left edge and %d wide", check, testMetrics.checkWidth)
	}
	if top, bottom := item.top+3, item.bottom-4; check.top != top || check.bottom != bottom {
		t.Errorf("check box top/bottom = %d/%d, want %d/%d (vertically centered)",
			check.top, check.bottom, top, bottom)
	}
	if want := item.left + testMetrics.checkWidth + gutterPad; text.left != want {
		t.Errorf("text left = %d, want %d (clear of the check column)", text.left, want)
	}
	if text.top != item.top-textLift || text.bottom != item.bottom-textLift {
		t.Errorf("text box = %+v, want the item's height lifted by %d so DT_VCENTER lands where Windows puts its own labels",
			text, textLift)
	}
}

func TestLayoutSurvivesAnItemNarrowerThanItsPadding(t *testing.T) {
	item := rect{left: 0, top: 0, right: 10, bottom: 20}
	_, text := layout(item, testMetrics)
	if text.right < text.left {
		t.Errorf("text box = %+v, want a non-negative width", text)
	}
}

func TestFade(t *testing.T) {
	const (
		white = 0x00FFFFFF
		blue  = 0x00D77800 // COLORREF for RGB(0, 120, 215)
	)
	if got := fade(white, blue, 0); got != white {
		t.Errorf("fade(_, _, 0%%) = %#06x, want the colour itself", got)
	}
	if got := fade(white, blue, 100); got != blue {
		t.Errorf("fade(_, _, 100%%) = %#06x, want the colour faded into", got)
	}
	// Halfway: every channel lands between the two.
	got := fade(white, blue, 50)
	for shift := uint(0); shift <= 16; shift += 8 {
		v := (got >> shift) & 0xFF
		lo, hi := uint32(blue>>shift)&0xFF, uint32(white>>shift)&0xFF
		if v < lo || v > hi {
			t.Errorf("fade(white, blue, 50%%) channel at bit %d = %d, want between %d and %d", shift, v, lo, hi)
		}
	}
	if got == white || got == blue {
		t.Errorf("fade(white, blue, 50%%) = %#06x, want a mix of the two", got)
	}
}
