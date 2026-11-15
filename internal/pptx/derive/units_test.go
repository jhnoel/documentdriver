package derive

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

func xfrmNode(t *testing.T, s string) *tree.Node {
	t.Helper()
	doc, err := tree.Parse([]byte(`<a:xfrm xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` + s))
	if err != nil {
		t.Fatal(err)
	}
	return doc.Root()
}

func TestReadXfrmStrict(t *testing.T) {
	cases := []struct {
		xml  string
		want StoredXfrm
		err  string
	}{
		{`><a:off x="1" y="2"/><a:ext cx="3" cy="4"/></a:xfrm>`, StoredXfrm{X: 1, Y: 2, W: 3, H: 4}, ""},
		{` rot="5400000" flipH="1" flipV="true"><a:off x="-1" y="2"/><a:ext cx="3" cy="4"/></a:xfrm>`,
			StoredXfrm{X: -1, Y: 2, W: 3, H: 4, Rot: 5400000, FlipH: true, FlipV: true}, ""},
		{`><a:off x="notanumber" y="2"/><a:ext cx="3" cy="4"/></a:xfrm>`, StoredXfrm{}, `a:off@x="notanumber": not an integer`},
		{`><a:off x="1.5" y="2"/><a:ext cx="3" cy="4"/></a:xfrm>`, StoredXfrm{}, "not an integer"},
		{`><a:off y="2"/><a:ext cx="3" cy="4"/></a:xfrm>`, StoredXfrm{}, "a:off@x is not written"},
		{`><a:ext cx="3" cy="4"/></a:xfrm>`, StoredXfrm{}, "has no a:off"},
		{` rot="x"><a:off x="1" y="2"/><a:ext cx="3" cy="4"/></a:xfrm>`, StoredXfrm{}, `rot="x"`},
		{` flipH="yes"><a:off x="1" y="2"/><a:ext cx="3" cy="4"/></a:xfrm>`, StoredXfrm{}, "not an xsd:boolean"},
		{`><a:off x="1" y="2"/><a:ext cx="-3" cy="4"/></a:xfrm>`, StoredXfrm{}, "negative extent"},
	}
	for _, c := range cases {
		got, err := ReadXfrm(xfrmNode(t, c.xml))
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err = %v, want %q", c.xml, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v, %v", c.xml, got, err)
		}
	}
}

func TestReadTransformInheritsOnlyWhenAbsent(t *testing.T) {
	ns := `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`
	parse := func(s string) *tree.Node {
		doc, err := tree.Parse([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return doc.Root()
	}
	layout := parse(`<p:sp ` + ns + `><p:spPr><a:xfrm><a:off x="10" y="20"/><a:ext cx="30" cy="40"/></a:xfrm></p:spPr></p:sp>`)
	bare := parse(`<p:sp ` + ns + `><p:spPr/></p:sp>`)
	broken := parse(`<p:sp ` + ns + `><p:spPr><a:xfrm><a:off x="bad" y="20"/><a:ext cx="30" cy="40"/></a:xfrm></p:spPr></p:sp>`)

	r := ReadTransform([]Link{{bare, FromSlide}, {layout, FromLayout}}, Root, nil)
	if b, ok := r.Box().Get(); !ok || b.X != 10 || r.Box().Origin() != FromLayout {
		t.Fatalf("inherited box = %+v %v", b, r.Box().Reason())
	}
	r = ReadTransform([]Link{{broken, FromSlide}, {layout, FromLayout}}, Root, nil)
	if r.Box().OK() || !strings.Contains(r.Box().Reason(), "not an integer") {
		t.Fatalf("broken transform: %+v %q", r, r.Box().Reason())
	}
}

func TestValZeroIsAbsent(t *testing.T) {
	var v Val[int]
	if v.OK() || v.Reason() == "" {
		t.Fatal("zero Val is present")
	}
	if g := Got(0, FromSpecDflt); !g.OK() || g.Origin() != FromSpecDflt || g.Reason() != "" {
		t.Fatal("Got")
	}
}

func TestColorInvalidComponentDoesNotResolve(t *testing.T) {
	doc, err := tree.Parse([]byte(`<a:hslClr xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" hue="x" sat="0" lum="0"/>`))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := ParseColor(doc.Root())
	if !ok || c.Invalid == "" {
		t.Fatalf("ParseColor = %+v, %v", c, ok)
	}
	if _, err := (Scheme{}).Resolve(c, ClrMap{}, nil); err == nil {
		t.Fatal("invalid colour resolved")
	}
}
