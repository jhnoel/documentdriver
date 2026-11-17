package model

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
)

// TestObjectOmittedV10Fill checks MF1 row 1: BuildPage on V10
// page0/s9 keeps paint's omission, so Omitted contains the fill
// entry with the themed-value reason.
func TestObjectOmittedV10Fill(t *testing.T) {
	d, r := openFixture(t, "v10-defects")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s9")
	want := Omission{Prop: "fill", Reason: "themed value not derived"}
	found := false
	for _, got := range o.Omitted {
		if got == want {
			found = true
		}
	}
	if !found {
		t.Errorf("page0/s9 Omitted = %#v, want it to contain %#v", o.Omitted, want)
	}
}

// TestObjectOmittedV10Box checks MF1 row 2: BuildPage on V10
// page0/s8 (Master 99, only PinX and PinY stored) keeps geometry's
// omission, so Omitted holds a box entry whose reason names Width.
func TestObjectOmittedV10Box(t *testing.T) {
	d, r := openFixture(t, "v10-defects")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s8")
	reason := omittedReason(o, "box")
	if !strings.Contains(reason, "Width") {
		t.Errorf("page0/s8 box omission = %q, want a reason naming Width (omitted %v)", reason, o.Omitted)
	}
	if reason != "Width not stored" {
		t.Errorf("page0/s8 box omission = %q, want %q", reason, "Width not stored")
	}
}

// TestObjectOmittedV1 checks MF1 row 4: BuildPage on V1 page0/s1,
// whose box, fill, text and props are all derived, keeps an empty
// Omitted.
func TestObjectOmittedV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	p := buildPageOf(t, d, r, 0)
	if got := byID(t, p, "page0/s1").Omitted; len(got) != 0 {
		t.Errorf("page0/s1 Omitted = %#v, want empty", got)
	}
}

// TestObjectOmittedV6BadValue checks MF1 row 3: on V6 page0/s1 the
// BuildPage Omitted is exactly the omissions geometry, paint,
// textOf and readProps record separately, none dropped; and with a
// test-built Value that does not parse as its type, fillObject keeps
// that prop omission alongside the others, in geometry, paint, text,
// props order and at their summed count.
func TestObjectOmittedV6BadValue(t *testing.T) {
	d, r := openFixture(t, "v6-data")
	p := buildPageOf(t, d, r, 0)
	o := byID(t, p, "page0/s1")
	s := shapeFor(o, 0, false)
	geo := &Object{ID: o.ID, Kind: o.Kind}
	geometry(geo, r, s, place.Page, p.H, Options{})
	pa := &Object{ID: o.ID, Kind: o.Kind}
	paint(pa, r, s)
	tx := &Object{ID: o.ID, Kind: o.Kind}
	textOf(tx, r, s)
	_, propsOmitted := readProps(o.ID, r, s)
	var want []Omission
	want = append(want, geo.Omitted...)
	want = append(want, pa.Omitted...)
	want = append(want, tx.Omitted...)
	want = append(want, propsOmitted...)
	if len(o.Omitted) != len(geo.Omitted)+len(pa.Omitted)+len(tx.Omitted)+len(propsOmitted) {
		t.Errorf("page0/s1 Omitted count = %d, want geometry %d + paint %d + text %d + props %d",
			len(o.Omitted), len(geo.Omitted), len(pa.Omitted), len(tx.Omitted), len(propsOmitted))
	}
	if !reflect.DeepEqual(o.Omitted, want) {
		t.Errorf("page0/s1 Omitted = %#v, want separately computed %#v", o.Omitted, want)
	}

	parsed, err := tree.ParseString(`<Shape ID='1'><Cell N='PinX' V='2'/><Cell N='PinY' V='9'/>` +
		`<Cell N='FillForegnd' V='Themed'/><Section N='Property'><Row N='Num'>` +
		`<Cell N='Value' V='abc'/><Cell N='Type' V='2'/></Row></Section></Shape>`)
	if err != nil {
		t.Fatalf("ParseString: %v", err)
	}
	head := readShapeHead(parsed.Root(), 0, false)
	if !head.ok {
		t.Fatal("test-built shape has no usable ID")
	}
	geo = &Object{ID: "page0/s1", Kind: Shape}
	geometry(geo, r, head.s, place.Page, 11, Options{})
	pa = &Object{ID: "page0/s1", Kind: Shape}
	paint(pa, r, head.s)
	tx = &Object{ID: "page0/s1", Kind: Shape}
	textOf(tx, r, head.s)
	_, propsOmitted = readProps("page0/s1", r, head.s)
	want = nil
	want = append(want, geo.Omitted...)
	want = append(want, pa.Omitted...)
	want = append(want, tx.Omitted...)
	want = append(want, propsOmitted...)
	if len(want) == 0 {
		t.Fatal("separately computed omissions are empty, want geometry, paint and props omissions")
	}
	fresh := &Object{ID: "page0/s1", Kind: Shape}
	fillObject(fresh, r, head.s, place.Page, 11, Options{})
	propWant := Omission{Prop: "page0/s1/prop:Num", Reason: `value "abc" is not a number`}
	found := false
	for _, got := range fresh.Omitted {
		if got == propWant {
			found = true
		}
	}
	if !found {
		t.Errorf("fillObject Omitted = %#v, want it to contain %#v", fresh.Omitted, propWant)
	}
	if len(fresh.Omitted) != len(geo.Omitted)+len(pa.Omitted)+len(tx.Omitted)+len(propsOmitted) {
		t.Errorf("fillObject Omitted count = %d, want geometry %d + paint %d + text %d + props %d",
			len(fresh.Omitted), len(geo.Omitted), len(pa.Omitted), len(tx.Omitted), len(propsOmitted))
	}
	if !reflect.DeepEqual(fresh.Omitted, want) {
		t.Errorf("fillObject Omitted = %#v, want separately computed %#v in geometry, paint, text, props order", fresh.Omitted, want)
	}
}
