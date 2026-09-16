//go:build windows

package install

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, content []byte) string {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSameContents(t *testing.T) {
	dir := t.TempDir()
	reference := writeFile(t, filepath.Join(dir, "reference"), []byte("hello world"))

	tests := []struct {
		name  string
		other string
		want  bool
	}{
		{"identical", writeFile(t, filepath.Join(dir, "identical"), []byte("hello world")), true},
		{"different size", writeFile(t, filepath.Join(dir, "shorter"), []byte("hello")), false},
		{"same size, different bytes", writeFile(t, filepath.Join(dir, "altered"), []byte("hello worlD")), false},
		{"not installed yet", filepath.Join(dir, "missing"), false},
	}
	for _, tt := range tests {
		got, err := sameContents(reference, tt.other)
		if err != nil {
			t.Errorf("%s: sameContents error: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: sameContents = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSameContentsSpansReadBuffers(t *testing.T) {
	dir := t.TempDir()
	// Longer than the 64 KiB read buffer, so a difference in the last
	// chunk is only caught if every chunk is compared.
	big := bytes.Repeat([]byte("audio"), 40_000)
	reference := writeFile(t, filepath.Join(dir, "big"), big)

	same := writeFile(t, filepath.Join(dir, "big-same"), big)
	if got, err := sameContents(reference, same); err != nil || !got {
		t.Errorf("sameContents(identical large files) = %v, %v; want true, nil", got, err)
	}

	altered := append(bytes.Clone(big[:len(big)-1]), 'X')
	differs := writeFile(t, filepath.Join(dir, "big-differs"), altered)
	if got, err := sameContents(reference, differs); err != nil || got {
		t.Errorf("sameContents(differing last byte) = %v, %v; want false, nil", got, err)
	}
}

func TestSameContentsMissingDownload(t *testing.T) {
	dir := t.TempDir()
	installed := writeFile(t, filepath.Join(dir, "installed"), []byte("x"))

	if _, err := sameContents(filepath.Join(dir, "missing"), installed); err == nil {
		t.Error("sameContents with a missing first file succeeded, want an error")
	}
}
