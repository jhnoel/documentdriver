package render

import (
	"image"
	"math"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/render/fonts"
	"github.com/jhnoel/documentdriver/internal/render/ttf"
)

// The ASCII advance widths, in thousandths of an em, the width tables
// that preceded the embedded fonts held (read from the same fonts'
// hmtx by a throwaway script, metrics_table.go until 2026-11-25): the
// font-backed widths must equal them, so layouts do not move.
var (
	oldCarlitoRegular = [95]uint16{226, 326, 401, 498, 507, 715, 682, 221, 303, 303, 498, 498, 250, 306, 252, 386, 507, 507, 507, 507, 507, 507, 507, 507, 507, 507, 268, 268, 498, 498, 498, 463, 894, 579, 544, 533, 615, 488, 459, 631, 623, 252, 319, 520, 420, 855, 646, 662, 517, 673, 543, 459, 487, 642, 567, 890, 519, 487, 468, 307, 386, 307, 498, 498, 291, 479, 525, 423, 525, 498, 305, 471, 525, 229, 239, 455, 229, 799, 525, 527, 525, 525, 349, 391, 335, 525, 452, 715, 433, 453, 395, 314, 460, 314, 498}
	oldCarlitoBold    = [95]uint16{226, 326, 438, 498, 507, 729, 705, 233, 312, 312, 498, 498, 258, 306, 267, 430, 507, 507, 507, 507, 507, 507, 507, 507, 507, 507, 276, 276, 498, 498, 498, 463, 898, 606, 561, 529, 630, 488, 459, 637, 631, 267, 331, 547, 423, 874, 659, 676, 532, 686, 563, 473, 495, 653, 591, 906, 551, 520, 478, 325, 430, 325, 498, 498, 300, 494, 537, 418, 537, 503, 316, 474, 537, 246, 255, 480, 246, 813, 537, 538, 537, 537, 355, 399, 347, 537, 473, 745, 459, 474, 397, 344, 475, 344, 498}
	oldSansRegular    = [95]uint16{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584}
	oldSansBold       = [95]uint16{278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584}
	oldSerifRegular   = [95]uint16{250, 333, 408, 500, 500, 833, 778, 180, 333, 333, 500, 564, 250, 333, 250, 278, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 278, 278, 564, 564, 564, 444, 921, 722, 667, 667, 722, 611, 556, 722, 722, 333, 389, 722, 611, 889, 722, 722, 556, 722, 667, 556, 611, 722, 722, 944, 722, 722, 611, 333, 278, 333, 469, 500, 333, 444, 500, 444, 500, 444, 333, 500, 500, 278, 278, 500, 278, 778, 500, 500, 500, 500, 333, 389, 278, 500, 500, 722, 500, 500, 444, 480, 200, 480, 541}
	oldSerifBold      = [95]uint16{250, 333, 555, 500, 500, 1000, 833, 278, 333, 333, 500, 570, 250, 333, 250, 278, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 333, 333, 570, 570, 570, 500, 930, 722, 667, 722, 722, 667, 611, 778, 778, 389, 500, 778, 667, 944, 722, 778, 611, 778, 722, 556, 667, 722, 722, 1000, 722, 722, 667, 333, 278, 333, 581, 500, 333, 500, 556, 444, 556, 444, 333, 500, 556, 278, 333, 556, 278, 833, 556, 500, 556, 556, 444, 389, 333, 556, 500, 722, 500, 500, 444, 394, 220, 394, 520}
)

// TestEmbeddedFonts checks that every embedded font parses and that
// RuneWidth gives, for printable ASCII, the widths the old tables held
// (rounded to thousandths), regular and bold of each family.
func TestEmbeddedFonts(t *testing.T) {
	for _, name := range append([]string{fallbackFile}, faceFiles[0][:]...) {
		b, err := fonts.Bytes(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := ttf.Parse(b); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	fs := loadFaces()
	for c := range faceFiles {
		for st := range faceFiles[c] {
			if fs.fonts[c][st] == nil {
				t.Errorf("%s did not load", faceFiles[c][st])
			}
		}
	}
	for _, c := range []struct {
		face  string
		bold  bool
		table *[95]uint16
	}{
		{"Calibri", false, &oldCarlitoRegular}, {"Calibri", true, &oldCarlitoBold},
		{"Arial", false, &oldSansRegular}, {"Arial", true, &oldSansBold},
		{"Times New Roman", false, &oldSerifRegular}, {"Times New Roman", true, &oldSerifBold},
	} {
		for i, want := range c.table {
			r := rune(' ' + i)
			got := math.Round(RuneWidth(c.face, c.bold, false, r) * 1000)
			if got != float64(want) {
				t.Errorf("%s bold %v %q: width %v, want %d", c.face, c.bold, r, got, want)
			}
		}
	}
}

// coverageText is the characters the Go renderer must draw from a real
// glyph in every face and style: printable ASCII, Latin-1 letters and
// signs, Greek, the arrows, sub- and superscript digits, and the
// punctuation and maths signs figures use.
const coverageText = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~" +
	"ÀÁÂÄÅÆÇÈÉÊËÌÍÎÏÑÒÓÔÖØÙÚÛÜßàáâäåæçèéêëìíîïñòóôöøùúûüÿ°±²³µ·×÷§¶©®" +
	"ΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩαβγδεζηθικλμνξοπρστυφχψω" +
	"←↑→↓↔↕⇒⇐⇔" +
	"₀₁₂₃₄₅₆₇₈₉⁰¹⁴⁵⁶⁷⁸⁹" +
	"•…–—‘’“”′″‰€≤≥≠≈∞√∑∫∂∆Ω"

// TestGlyphCoverage checks, for every face family and style, that each
// coverage character is drawn from a real glyph and not as a box: its
// picture alone differs from the picture of a character no font has
// (U+E000, private use), which the renderer leaves out with a note,
// and it inks the picture. With the 7x13 bitmap font this fails: it
// drew every character it lacked as the same box.
func TestGlyphCoverage(t *testing.T) {
	draw := func(face string, bold, italic bool, s string) (*image.RGBA, []string) {
		t.Helper()
		tl := &TextLine{Frame: geom.Identity, X: 4, Y: 20, Anchor: "start",
			Spans: []Span{{Text: s, Family: Family(face), Size: 20, Color: "#000000", Bold: bold, Italic: italic}}}
		sc := &Scene{W: 40, H: 28, Background: "#FFFFFF", Groups: []Group{{ID: "g", Items: []Item{{Kind: TextItem, Text: tl}}}}}
		img, notes, err := Rasterise(sc, 1)
		if err != nil {
			t.Fatal(err)
		}
		return img, notes
	}
	inked := func(img *image.RGBA) bool {
		for i := 0; i < len(img.Pix); i += 4 {
			if img.Pix[i] < 250 {
				return true
			}
		}
		return false
	}
	same := func(a, b *image.RGBA) bool {
		for i := range a.Pix {
			if a.Pix[i] != b.Pix[i] {
				return false
			}
		}
		return true
	}
	for _, face := range []string{"Calibri", "Arial", "Times New Roman"} {
		for st := range 4 {
			bold, italic := st&1 != 0, st&2 != 0
			missing, _ := draw(face, bold, italic, "")
			for _, r := range coverageText {
				if r == ' ' {
					continue
				}
				img, notes := draw(face, bold, italic, string(r))
				if len(notes) != 0 {
					t.Errorf("%s bold %v italic %v %q (U+%04X): notes %v", face, bold, italic, r, r, notes)
					continue
				}
				if !inked(img) || same(img, missing) {
					t.Errorf("%s bold %v italic %v %q (U+%04X): drawn as no glyph or as the missing-glyph box", face, bold, italic, r, r)
				}
			}
		}
	}
}

// TestMissingGlyphNote checks that a character no embedded font has is
// left out with a note naming it, not drawn as a box.
func TestMissingGlyphNote(t *testing.T) {
	tl := &TextLine{Frame: geom.Identity, X: 4, Y: 20, Anchor: "start", Spans: []Span{{Text: "ab", Size: 20, Color: "#000000"}}}
	sc := &Scene{W: 60, H: 28, Groups: []Group{{ID: "page0/s1", Items: []Item{{Kind: TextItem, Text: tl}}}}}
	_, notes, err := Rasterise(sc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0] != "page0/s1: text: no embedded font has U+E000 (left out)" {
		t.Errorf("notes = %q", notes)
	}
}
