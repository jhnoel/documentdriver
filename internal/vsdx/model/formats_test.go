package model

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// formatsOpenFixture opens the vsdx fixture name and returns its
// resolver. It is formatsOpenFixture rather than the package's
// openFixture so it cannot collide with other chunks' test helpers.
func formatsOpenFixture(t *testing.T, name string) *inherit.Resolver {
	t.Helper()
	p, err := opc.Open(vsdxtest.Fixture(t, name))
	if err != nil {
		t.Fatalf("opc.Open(%s): %v", name, err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close %s: %v", name, err)
		}
	})
	d, err := doc.Open(p)
	if err != nil {
		t.Fatalf("Open(%s): %v", name, err)
	}
	return inherit.New(d)
}

// formatsParseShape parses src as one Shape element and returns its
// shape, with TextStyle textStyle when textStyle is not negative.
func formatsParseShape(t *testing.T, src string, textStyle int) *inherit.Shape {
	t.Helper()
	parsed, err := tree.ParseString(src)
	if err != nil {
		t.Fatalf("ParseString(%q): %v", src, err)
	}
	sh := &inherit.Shape{}
	sh.Sheet, _ = sheet.Index(parsed.Root())
	if textStyle >= 0 {
		sh.Styles[2], sh.HasStyle[2] = uint32(textStyle), true
	}
	return sh
}

// formatsStyleResolver returns a resolver whose only style sheet is
// 9: a Character row 0 with Style 1, Size 0.1666666666666667, Color
// 0, Font Calibri and Strikethru 0, and a Paragraph row 0 with
// HorzAlign 1, Bullet 0, BulletStr "", and zero indents and spacings
// (no SpLine), for the in-test rows.
func formatsStyleResolver(t *testing.T) *inherit.Resolver {
	t.Helper()
	src := `<StyleSheet ID='9'><Section N='Character'><Row IX='0'><Cell N='Style' V='1'/><Cell N='Size' V='0.1666666666666667'/><Cell N='Color' V='0'/><Cell N='Font' V='Calibri'/><Cell N='Strikethru' V='0'/></Row></Section><Section N='Paragraph'><Row IX='0'><Cell N='HorzAlign' V='1'/><Cell N='Bullet' V='0'/><Cell N='BulletStr' V=''/><Cell N='IndLeft' V='0'/><Cell N='IndRight' V='0'/><Cell N='IndFirst' V='0'/><Cell N='SpBefore' V='0'/><Cell N='SpAfter' V='0'/></Row></Section></StyleSheet>`
	parsed, err := tree.ParseString(src)
	if err != nil {
		t.Fatalf("ParseString(style): %v", err)
	}
	d := &doc.Document{Part: "/visio/document.xml", Styles: map[uint32]*doc.StyleSheet{9: {ID: 9, Node: parsed.Root()}}}
	return inherit.New(d)
}

// TestFormatsV1 checks the V1 row: s1 (TextStyle 3, local Character
// row 0 Style 1) shows Char {0: {Bold "on"}} and no paragraph format.
func TestFormatsV1(t *testing.T) {
	r := formatsOpenFixture(t, "v1-minimal")
	owner := formatsParseShape(t, `<Shape ID='1'><Section N='Character'><Row IX='0'><Cell N='Style' V='1'/></Row></Section></Shape>`, 3)
	got := formats(r, owner)
	want := text.Formats{Char: map[int]text.CharFormat{0: {Bold: "on"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("formats(V1 s1) = %#v, want %#v", got, want)
	}
}

// TestFormatsV2 checks the V2 rows: s1 shows its master shape's text,
// so its owner is master shape 5 (Char {0: {Italic "on"}}); s2 has
// its own text and no Character or Paragraph rows, so it shows {}.
func TestFormatsV2(t *testing.T) {
	r := formatsOpenFixture(t, "v2-master")
	s1 := formatsParseShape(t, `<Shape ID='1'><Cell N='PinX' V='2'/><Cell N='PinY' V='9'/></Shape>`, -1)
	s1.Master, s1.HasMaster = 2, true
	master, ok := r.MasterShape(s1)
	if !ok {
		t.Fatal("MasterShape(s1) absent, want master2 shape 5")
	}
	got := formats(r, master)
	want := text.Formats{Char: map[int]text.CharFormat{0: {Italic: "on"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("formats(V2 s1) = %#v, want %#v", got, want)
	}
	s2 := formatsParseShape(t, `<Shape ID='2'><Cell N='PinX' V='4'/><Cell N='PinY' V='9'/><Cell N='Width' V='1' F='Inh'/></Shape>`, -1)
	s2.Master, s2.HasMaster = 2, true
	if got := formats(r, s2); len(got.Char) != 0 || len(got.Para) != 0 {
		t.Errorf("formats(V2 s2) = %#v, want {}", got)
	}
}

// TestFormatsV7 checks the V7 rows (testdata/vsdx/v7-text
// PROVENANCE.md): row 0's Size equals the inherited 12 pt so it
// shows an empty format, row 1 is bold red, row 2 is Arial 10 pt,
// and the paragraph rows show the alignment, bullet and indents.
func TestFormatsV7(t *testing.T) {
	r := formatsOpenFixture(t, "v7-text")
	src := `<Shape ID='1'><Section N='Character'><Row IX='0'><Cell N='Size' V='0.1666666666666667'/></Row><Row IX='1'><Cell N='Style' V='1'/><Cell N='Color' V='#FF0000'/></Row><Row IX='2'><Cell N='Font' V='Arial'/><Cell N='Size' V='0.1388888888888889'/></Row></Section><Section N='Paragraph'><Row IX='0'><Cell N='HorzAlign' V='0'/></Row><Row IX='1'><Cell N='IndFirst' V='-0.25'/><Cell N='IndLeft' V='0.25'/><Cell N='Bullet' V='1'/><Cell N='BulletStr' V='•'/></Row></Section></Shape>`
	owner := formatsParseShape(t, src, 3)
	got := formats(r, owner)
	want := text.Formats{
		Char: map[int]text.CharFormat{0: {}, 1: {Bold: "on", Color: "#FF0000"}, 2: {Size: "10", Font: "Arial"}},
		Para: map[int]text.ParaFormat{0: {Align: "left"}, 1: {Bullet: true, BulletChar: "•", MarginLeft: "18pt", Indent: "-18pt"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("formats(V7 s1) = %#v, want %#v", got, want)
	}
}

// TestFormatsInTest checks the in-test rows: owner cells over the
// style sheet of formatsStyleResolver (Style 1, Size
// 0.1666666666666667, Color 0, Font Calibri, HorzAlign 1).
func TestFormatsInTest(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want text.Formats
	}{
		{"bold off", `<Shape><Section N='Character'><Row IX='0'><Cell N='Style' V='0'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {Bold: "off"}}}},
		{"style bits", `<Shape><Section N='Character'><Row IX='0'><Cell N='Style' V='6'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {Bold: "off", Italic: "on", Underline: "on"}}}},
		{"strikethru on", `<Shape><Section N='Character'><Row IX='0'><Cell N='Strikethru' V='1'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {Strike: "on"}}}},
		{"strikethru off", `<Shape><Section N='Character'><Row IX='0'><Cell N='Strikethru' V='0'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {}}}},
		{"size equal inherited", `<Shape><Section N='Character'><Row IX='0'><Cell N='Size' V='0.1666666666666667'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {}}}},
		{"size 10", `<Shape><Section N='Character'><Row IX='0'><Cell N='Size' V='0.1388888888888889'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {Size: "10"}}}},
		{"color 2", `<Shape><Section N='Character'><Row IX='0'><Cell N='Color' V='2'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {Color: "#FF0000"}}}},
		{"font themed", `<Shape><Section N='Character'><Row IX='0'><Cell N='Font' V='Themed'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {}}}},
		{"font arial", `<Shape><Section N='Character'><Row IX='0'><Cell N='Font' V='Arial'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {Font: "Arial"}}}},
		{"font semicolon", `<Shape><Section N='Character'><Row IX='0'><Cell N='Font' V='A;B'/></Row></Section></Shape>`, text.Formats{Char: map[int]text.CharFormat{0: {}}}},
		{"align left", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='HorzAlign' V='0'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {Align: "left"}}}},
		{"align force", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='HorzAlign' V='4'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {}}}},
		{"bullet", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='Bullet' V='1'/><Cell N='BulletStr' V='•'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {Bullet: true, BulletChar: "•"}}}},
		{"ind left", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='IndLeft' V='0.25'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {MarginLeft: "18pt"}}}},
		{"ind right", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='IndRight' V='0.125'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {MarginRight: "9pt"}}}},
		{"ind first", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='IndFirst' V='-0.25'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {Indent: "-18pt"}}}},
		{"space before", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='SpBefore' V='0.0833333333333333'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {SpaceBefore: "6pt"}}}},
		{"space after", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='SpAfter' V='0.0833333333333333'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {SpaceAfter: "6pt"}}}},
		{"line height percent", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='SpLine' V='-1.2'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {LineHeight: "120%"}}}},
		{"line height points", `<Shape><Section N='Paragraph'><Row IX='0'><Cell N='SpLine' V='0.2'/></Row></Section></Shape>`, text.Formats{Para: map[int]text.ParaFormat{0: {LineHeight: "14.4pt"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := formatsStyleResolver(t)
			owner := formatsParseShape(t, tc.src, 9)
			if got := formats(r, owner); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("formats(%s) = %#v, want %#v", tc.name, got, tc.want)
			}
		})
	}
}

// TestFormatsNil checks that a nil resolver or owner shows nothing.
func TestFormatsNil(t *testing.T) {
	if got := formats(nil, nil); len(got.Char) != 0 || len(got.Para) != 0 {
		t.Errorf("formats(nil, nil) = %#v, want {}", got)
	}
	r := formatsStyleResolver(t)
	if got := formats(r, nil); len(got.Char) != 0 || len(got.Para) != 0 {
		t.Errorf("formats(r, nil) = %#v, want {}", got)
	}
}

// TestScalingFormats checks the scaling row: formats of N and 4N
// shapes (N = 1000) allocates at most 5.5 times as much for 4 times
// the shapes, linear in the number of shapes.
func TestScalingFormats(t *testing.T) {
	setup := func(n int) (func() error, error) {
		r := formatsOpenFixture(t, "v1-minimal")
		owner := formatsParseShape(t, `<Shape ID='1'><Section N='Character'><Row IX='0'><Cell N='Style' V='1'/></Row></Section></Shape>`, 3)
		many := make([]*inherit.Shape, n)
		for i := range many {
			many[i] = owner
		}
		return func() error {
			for _, s := range many {
				if got := formats(r, s); got.Char[0].Bold != "on" {
					return fmt.Errorf("formats = %#v, want Char[0].Bold %q", got, "on")
				}
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx model formats", setup)
}
