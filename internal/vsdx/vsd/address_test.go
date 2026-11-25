package vsd

import (
	"strings"
	"testing"
)

// TestPageID checks the page and master constructors (chunk V2 rows).
func TestPageID(t *testing.T) {
	for _, tc := range []struct {
		id   uint32
		dup  int
		want string
	}{
		{0, 1, "page0"},
		{12, 1, "page12"},
		{5, 2, "page5~2"},
	} {
		if got := PageID(tc.id, tc.dup); got != tc.want {
			t.Errorf("PageID(%d, %d) = %q, want %q", tc.id, tc.dup, got, tc.want)
		}
	}
	if got := MasterID(2); got != "master2" {
		t.Errorf("MasterID(2) = %q, want %q", got, "master2")
	}
}

// TestShapeID checks the shape constructor (chunk V2 rows).
func TestShapeID(t *testing.T) {
	for _, tc := range []struct {
		container string
		id        uint32
		dup       int
		want      string
	}{
		{"page0", 5, 1, "page0/s5"},
		{"page0", 5, 2, "page0/s5~2"},
		{"master2", 5, 1, "master2/s5"},
		{"page5~2", 3, 1, "page5~2/s3"},
	} {
		if got := ShapeID(tc.container, tc.id, tc.dup); got != tc.want {
			t.Errorf("ShapeID(%q, %d, %d) = %q, want %q", tc.container, tc.id, tc.dup, got, tc.want)
		}
	}
}

// TestPropLayerFieldID checks the prop, layer and field constructors
// (chunk V2 rows).
func TestPropLayerFieldID(t *testing.T) {
	if got := PropID("page0/s5", "Cost"); got != "page0/s5/prop:Cost" {
		t.Errorf("PropID = %q, want %q", got, "page0/s5/prop:Cost")
	}
	if got := PropID("page0/s5", "A/B"); got != "page0/s5/prop:A/B" {
		t.Errorf("PropID = %q, want %q", got, "page0/s5/prop:A/B")
	}
	if got := LayerID("page0", "Connector"); got != "page0/layer:Connector" {
		t.Errorf("LayerID = %q, want %q", got, "page0/layer:Connector")
	}
	if got := FieldID("page0/s5", 1); got != "page0/s5/field:1" {
		t.Errorf("FieldID = %q, want %q", got, "page0/s5/field:1")
	}
	if got := FieldID("page0/s5", 12); got != "page0/s5/field:12" {
		t.Errorf("FieldID = %q, want %q", got, "page0/s5/field:12")
	}
}

// TestGlue checks glue as read shows it (chunk V2 rows).
func TestGlue(t *testing.T) {
	for _, tc := range []struct {
		shape    uint32
		toCell   string
		resolved bool
		want     string
	}{
		{12, "PinX", true, "s12"},
		{12, "Connections.X3", true, "s12:3"},
		{12, "Geometry1.X2", true, "s12@Geometry1.X2"},
		{12, "PinX", false, "#12"},
		{12, "Connections.X3", false, "#12:3"},
		{12, "Geometry1.X2", false, "#12@Geometry1.X2"},
	} {
		if got := Glue(tc.shape, tc.toCell, tc.resolved); got != tc.want {
			t.Errorf("Glue(%d, %q, %t) = %q, want %q", tc.shape, tc.toCell, tc.resolved, got, tc.want)
		}
	}
}

// TestParse checks every form Parse accepts (chunk V2 rows, plus the
// dup-carrying container and master sub-addresses they imply).
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Address
	}{
		{"page0", Address{Kind: KindPage, Container: 0, PageDup: 1, Dup: 1}},
		{"page5~2", Address{Kind: KindPage, Container: 5, PageDup: 2, Dup: 1}},
		{"master2", Address{Kind: KindMaster, Container: 2, PageDup: 1, Dup: 1}},
		{"page0/s5~2", Address{Kind: KindShape, Container: 0, PageDup: 1, Shape: 5, Dup: 2}},
		{"page0/s5", Address{Kind: KindShape, Container: 0, PageDup: 1, Shape: 5, Dup: 1}},
		{"master2/s5", Address{Kind: KindShape, InMaster: true, Container: 2, PageDup: 1, Shape: 5, Dup: 1}},
		{"page0/s5/prop:A/B", Address{Kind: KindProp, Container: 0, PageDup: 1, Shape: 5, Dup: 1, Name: "A/B"}},
		{"page0/layer:Connector", Address{Kind: KindLayer, Container: 0, PageDup: 1, Dup: 1, Name: "Connector"}},
		{"page0/s5/field:2", Address{Kind: KindField, Container: 0, PageDup: 1, Shape: 5, Dup: 1, N: 2}},
		{"$box", Address{Kind: KindAlias, Alias: "box"}},
		{"page5~2/s3", Address{Kind: KindShape, Container: 5, PageDup: 2, Shape: 3, Dup: 1}},
		{"page5~2/layer:Flow", Address{Kind: KindLayer, Container: 5, PageDup: 2, Dup: 1, Name: "Flow"}},
		{"master2/s5~2/prop:Cost", Address{Kind: KindProp, InMaster: true, Container: 2, PageDup: 1, Shape: 5, Dup: 2, Name: "Cost"}},
		{"master2/s5/field:1", Address{Kind: KindField, InMaster: true, Container: 2, PageDup: 1, Shape: 5, Dup: 1, N: 1}},
		{"page0/s5~3/field:4", Address{Kind: KindField, Container: 0, PageDup: 1, Shape: 5, Dup: 3, N: 4}},
	} {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

// TestParseErrors checks that each bad address is refused with an
// error naming the expected form (chunk V2 rows).
func TestParseErrors(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"slide3", "page<id>"},
		{"page", "page<id>"},
		{"page-1", "page<id>"},
		{"page01", "page<id>"},
		{"page0/", "page<id>/s<id>"},
		{"page0/s", "page<id>/s<id>"},
		{"page0/s5~1", "~<dup>"},
		{"page0/s5~0", "~<dup>"},
		{"page0~1", "~<dup>"},
		{"page0/x5", "/s<id>"},
		{"page0/s5/prop:", "/prop:<name>"},
		{"page0/s5/field:0", "/field:<n>"},
		{"page99999999999", "page<id>"},
		{"$", "$alias"},
		{"$1x", "$alias"},
		{"master2~2", "master<id>"},
		{"", "page<id>"},
		{"master", "master<id>"},
		{"master2/layer:Foo", "master<id>/s<id>"},
		{"page0/s5/field:01", "/field:<n>"},
		{"page0/s05", "page<id>/s<id>"},
		{"page0/s5/unknown:X", "/prop:<name>"},
	} {
		_, err := Parse(tc.in)
		if err == nil {
			t.Errorf("Parse(%q) = no error, want one naming %q", tc.in, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) error = %q, want it to name %q", tc.in, err, tc.want)
		}
	}
}

// TestParseRoundTrip checks the chunk V2 property over generated
// values: Parse(ShapeID(PageID(p, pd), s, d)) returns p, pd, s, d.
func TestParseRoundTrip(t *testing.T) {
	for i := range 256 {
		p := uint32(i * 7919 % 100003)
		pd := 1 + i%5
		s := uint32(i * 104729 % 100003)
		d := 1 + (i/7)%5
		in := ShapeID(PageID(p, pd), s, d)
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", in, err)
		}
		if got.Kind != KindShape || got.Container != p || got.PageDup != pd || got.Shape != s || got.Dup != d {
			t.Errorf("Parse(%q) = %+v, want KindShape Container %d PageDup %d Shape %d Dup %d", in, got, p, pd, s, d)
		}
	}
}

// FuzzParseAddress feeds arbitrary strings to Parse: it must never
// panic, and every accepted address must carry the invariants its
// kind promises (chunk V2).
func FuzzParseAddress(f *testing.F) {
	for _, s := range []string{
		"page0", "page5~2", "master2", "page0/s5~2", "page0/s5/prop:A/B",
		"page0/layer:Connector", "page0/s5/field:2", "$box",
		"slide3", "page01", "page0/s5~1", "page99999999999", "$1x", "master2~2",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		a, err := Parse(s)
		if err != nil {
			return
		}
		switch a.Kind {
		case KindAlias:
			if a.Alias == "" {
				t.Errorf("Parse(%q) = alias %q", s, a.Alias)
			}
		case KindPage, KindMaster, KindShape, KindProp, KindLayer, KindField:
			if a.PageDup < 1 || a.Dup < 1 {
				t.Errorf("Parse(%q) = %+v, dup below 1", s, a)
			}
			if a.Kind == KindField && a.N < 1 {
				t.Errorf("Parse(%q) = %+v, field below 1", s, a)
			}
			if (a.Kind == KindProp || a.Kind == KindLayer) && a.Name == "" {
				t.Errorf("Parse(%q) = %+v, empty name", s, a)
			}
		default:
			t.Errorf("Parse(%q) = unknown kind %d", s, a.Kind)
		}
	})
}
