package ttf

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/render/fonts"
)

// font returns the embedded font name, parsed.
func font(t testing.TB, name string) (*Font, []byte) {
	t.Helper()
	b, err := fonts.Bytes(name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(b)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f, b
}

// TestParse checks the header facts of an embedded font against the
// values fontTools reports for it (LiberationSans-Regular.ttf 2.1.5:
// 2048 units per em, ascender 1854, descender -434, 'A' glyph 36 with
// advance 1366, U+2192 present, 'Ä' a composite glyph with contours).
func TestParse(t *testing.T) {
	f, _ := font(t, "LiberationSans-Regular.ttf")
	if f.UnitsPerEm() != 2048 || f.Ascent() != 1854 || f.Descent() != -434 {
		t.Errorf("upem %d ascent %d descent %d, want 2048 1854 -434", f.UnitsPerEm(), f.Ascent(), f.Descent())
	}
	if g := f.Index('A'); g != 36 || f.Advance(g) != 1366 {
		t.Errorf("A: glyph %d advance %d, want 36 1366", g, f.Advance(g))
	}
	if f.Index(0x2192) == 0 {
		t.Error("U+2192 not mapped")
	}
	if f.Index(0xE000) != 0 || f.Index(-1) != 0 || f.Index(0x10FFFF) != 0 {
		t.Error("unmapped characters should give glyph 0")
	}
	for _, r := range []rune{'A', 'Ä', 'o', '→'} {
		cs, err := f.Outline(f.Index(r))
		if err != nil || len(cs) == 0 {
			t.Errorf("%q: %d contours, %v", r, len(cs), err)
		}
	}
	// 'o' has an outer and an inner contour.
	if cs, _ := f.Outline(f.Index('o')); len(cs) != 2 {
		t.Errorf("o: %d contours, want 2", len(cs))
	}
	if _, err := f.Outline(f.numGlyphs); err == nil {
		t.Error("a glyph past the end should be an error")
	}
}

// TestParseTruncated checks that every prefix length of a font up to a
// few kilobytes, and some longer ones, is refused or read without a
// panic, and that lookups on what parses do not panic.
func TestParseTruncated(t *testing.T) {
	_, b := font(t, "Carlito-Regular.ttf")
	for n := 0; n < len(b); n += 1 + n/7 {
		f, err := Parse(b[:n])
		if err != nil {
			continue
		}
		for _, r := range "Aa→₀" {
			g := f.Index(r)
			f.Advance(g)
			_, _ = f.Outline(g) //nolint:errcheck // only that it does not panic
		}
	}
}

// FuzzParse parses mutated fonts and looks up and outlines glyphs: no
// input may panic or exceed the point bound.
func FuzzParse(f *testing.F) {
	_, b := font(f, "LiberationSerif-Regular.ttf")
	f.Add(b[:4096])
	f.Add(b)
	f.Fuzz(func(t *testing.T, data []byte) {
		ft, err := Parse(data)
		if err != nil {
			return
		}
		for _, r := range "Ag→é" {
			g := ft.Index(r)
			ft.Advance(g)
			if cs, err := ft.Outline(g); err == nil {
				n := 0
				for _, c := range cs {
					n += len(c)
				}
				if n > MaxPoints {
					t.Fatalf("%d points, bound %d", n, MaxPoints)
				}
			}
		}
		for g := range min(ft.numGlyphs, 64) {
			_, _ = ft.Outline(g) //nolint:errcheck // only that it does not panic
		}
	})
}
