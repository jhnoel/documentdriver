package model

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// broken writes a copy of the minimal deck with one attribute set to a
// value its schema type does not allow.
func broken(t *testing.T, part, sel, attr, value string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "broken.pptx")
	if err := ops.ApplyTo(testdeck.Minimal(t), dst, &ops.DebugSetAttr{Part: part, Selector: sel, Attr: attr, Value: value}); err != nil {
		t.Fatal(err)
	}
	return dst
}

// TestUnreadableValuesAreOmitted: values that cannot be read as their
// schema type are omitted with a reason, never read as zero.
func TestUnreadableValuesAreOmitted(t *testing.T) {
	const slide = "/ppt/slides/slide1.xml"
	cases := []struct {
		sel, attr, value string
		id, prop, reason string
	}{
		{"/p:sld/p:cSld/p:spTree/p:sp[1]/p:spPr/a:xfrm/a:off", "x", "notanumber", "s2", "box", "not an integer"},
		{"/p:sld/p:cSld/p:spTree/p:sp[1]/p:spPr/a:xfrm", "rot", "1.5", "s2", "box", "not an integer"},
		{"/p:sld/p:cSld/p:spTree/p:sp[1]/p:spPr/a:xfrm", "flipH", "yes", "s2", "box", "not an xsd:boolean"},
		{"//p:cNvPr[@id='3']", "hidden", "maybe", "s3", "hidden", "not an xsd:boolean"},
		{"/p:sld/p:cSld/p:spTree/p:cxnSp[1]/p:spPr/a:ln", "w", "wide", "s4", "line.w", "not an integer"},
	}
	for _, c := range cases {
		t.Run(c.attr+"="+c.value, func(t *testing.T) {
			_, s := readFirst(t, broken(t, slide, c.sel, c.attr, c.value))
			o := byID(s)[c.id]
			if r := omitted(o, c.prop); !strings.Contains(r, c.reason) {
				t.Fatalf("%s %s omission = %q, want %q", c.id, c.prop, r, c.reason)
			}
			if c.prop == "box" && (o.Box != nil || o.Stored != nil) {
				t.Fatalf("box read as %+v", o.Stored)
			}
		})
	}
}

// TestAmbiguousGlue: glue to an id that several objects share resolves
// to none of them, rather than to the first.
func TestAmbiguousGlue(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dup.pptx")
	op := &ops.DebugSetAttr{Part: "/ppt/slides/slide1.xml", Selector: "//a:stCxn[1]", Attr: "id", Value: "40"}
	if err := ops.ApplyTo(testdeck.Diagram(t), dst, op); err != nil {
		t.Fatal(err)
	}
	_, s := readFirst(t, dst)
	for _, o := range s.Objects {
		if o.Conn == nil || o.Conn.Start == nil || o.Conn.Start.RawID != "40" {
			continue
		}
		g := o.Conn.Start
		if g.ID != "" || strings.Join(g.Candidates, ",") != "s40,s40~2" {
			t.Fatalf("glue = %+v", g)
		}
		if !strings.Contains(omitted(o, "start"), "ambiguous") {
			t.Fatalf("omitted = %v", o.Omitted)
		}
		for _, h := range s.Hazards {
			if h.ID == o.ID && h.Kind == "dangling-glue" {
				t.Fatal("ambiguous glue reported as dangling")
			}
		}
		return
	}
	t.Fatal("no connector glued to id 40")
}

func TestUnreadableSlideSizeIsOmitted(t *testing.T) {
	d, s := readFirst(t, broken(t, "/ppt/presentation.xml", "//p:sldSz", "cx", "huge"))
	if d.W != 0 || len(d.Omitted) != 1 || !strings.Contains(s.Omitted[0].Reason, "not an integer") {
		t.Fatalf("W=%v omitted=%v slide=%v", d.W, d.Omitted, s.Omitted)
	}
}
