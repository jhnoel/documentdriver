package testdeck

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExport writes the committed fixture decks and the image assets the
// conformance plans use to the directory named by TESTDECK_EXPORT, for
// `make conformance`. It skips when the variable is unset.
func TestExport(t *testing.T) {
	dir := os.Getenv("TESTDECK_EXPORT")
	if dir == "" {
		t.Skip("TESTDECK_EXPORT not set")
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil { //nolint:gosec // G301: developer scratch directory
		t.Fatal(err)
	}
	for name, src := range All(t) {
		b, err := os.ReadFile(src) //nolint:gosec // G304: a temp copy made by All
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".pptx"), b, 0o644); err != nil { //nolint:gosec // G306: developer scratch file
			t.Fatal(err)
		}
	}
	files := map[string][]byte{
		"a.png": pngBytes(t),
		"b.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10" viewBox="0 0 20 10"><rect width="20" height="10" fill="#c83232"/></svg>`),
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, "assets", name), b, 0o644); err != nil { //nolint:gosec // G306: developer scratch file
			t.Fatal(err)
		}
	}
}
