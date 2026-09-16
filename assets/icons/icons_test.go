package icons

import (
	"bytes"
	"image/png"
	"testing"
)

func TestImagePicksTheSmallestSizeThatFits(t *testing.T) {
	tests := []struct{ size, want int }{
		{16, 16},
		{17, 20},
		{32, 32},
		{33, 40},
		{96, 128},
		{200, 256},
		{1000, 256}, // nothing big enough: the largest there is
	}
	for _, tt := range tests {
		data, ok := Image(Icon, tt.size)
		if !ok {
			t.Fatalf("Image(%d) failed", tt.size)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Image(%d) is not a PNG: %v", tt.size, err)
		}
		if cfg.Width != tt.want {
			t.Errorf("Image(%d) is %dpx wide, want %d", tt.size, cfg.Width, tt.want)
		}
	}
}

func TestImageRejectsMalformedData(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not an icon"), Icon[:30]} {
		if _, ok := Image(data, 32); ok {
			t.Errorf("Image(%q...) succeeded, want failure", data[:min(len(data), 8)])
		}
	}
}
