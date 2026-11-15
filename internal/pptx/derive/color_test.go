package derive

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

const nsDecl = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`

func parse(t *testing.T, s string) *tree.Node {
	t.Helper()
	d, err := tree.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d.Root()
}

const scheme = `<a:clrScheme ` + nsDecl + ` name="Office">
<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1>
<a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>
<a:dk2><a:srgbClr val="44546A"/></a:dk2>
<a:lt2><a:srgbClr val="E7E6E6"/></a:lt2>
<a:accent1><a:srgbClr val="4472C4"/></a:accent1>
<a:accent2><a:srgbClr val="ED7D31"/></a:accent2>
</a:clrScheme>`

func resolve(t *testing.T, s Scheme, m ClrMap, el string, ph *Color) (Resolved, error) {
	t.Helper()
	c, ok := ParseColor(parse(t, el))
	if !ok {
		t.Fatalf("not a colour: %s", el)
	}
	return s.Resolve(c, m, ph)
}

func TestResolveColors(t *testing.T) {
	s := ParseScheme(parse(t, scheme))
	m := ParseClrMap(parse(t, `<p:clrMap `+nsDecl+` bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2"/>`))
	cases := []struct{ el, hex string }{
		{`<a:srgbClr ` + nsDecl + ` val="ff0000"/>`, "FF0000"},
		{`<a:schemeClr ` + nsDecl + ` val="accent1"/>`, "4472C4"},
		{`<a:schemeClr ` + nsDecl + ` val="tx1"/>`, "000000"},
		{`<a:schemeClr ` + nsDecl + ` val="bg2"/>`, "E7E6E6"},
		{`<a:sysClr ` + nsDecl + ` val="window" lastClr="FFFFFF"/>`, "FFFFFF"},
		{`<a:prstClr ` + nsDecl + ` val="red"/>`, "FF0000"},
		{`<a:hslClr ` + nsDecl + ` hue="0" sat="100000" lum="50000"/>`, "FF0000"},
		{`<a:scrgbClr ` + nsDecl + ` r="100000" g="0" b="0"/>`, "FF0000"},
		// Luminance modifiers in HSL (LibreOffice's method).
		{`<a:schemeClr ` + nsDecl + ` val="accent1"><a:lumMod val="75000"/></a:schemeClr>`, "2F5597"},
		{`<a:schemeClr ` + nsDecl + ` val="accent1"><a:lumMod val="60000"/><a:lumOff val="40000"/></a:schemeClr>`, "8FAADC"},
		{`<a:schemeClr ` + nsDecl + ` val="bg1"><a:lumMod val="85000"/></a:schemeClr>`, "D9D9D9"},
		// Shade and tint in linear RGB with gamma 2.3.
		{`<a:srgbClr ` + nsDecl + ` val="FFFFFF"><a:shade val="0"/></a:srgbClr>`, "000000"},
		{`<a:srgbClr ` + nsDecl + ` val="000000"><a:tint val="0"/></a:srgbClr>`, "FFFFFF"},
		// The linear-RGB round trip truncates, as in LibreOffice.
		{`<a:srgbClr ` + nsDecl + ` val="808080"><a:tint val="100000"/></a:srgbClr>`, "7F7F7F"},
		{`<a:srgbClr ` + nsDecl + ` val="FF0000"><a:comp/></a:srgbClr>`, "00FFFF"},
		{`<a:srgbClr ` + nsDecl + ` val="FF0000"><a:inv/></a:srgbClr>`, "00FFFF"},
	}
	for _, c := range cases {
		r, err := resolve(t, s, m, c.el, nil)
		if err != nil {
			t.Errorf("%s: %v", c.el, err)
			continue
		}
		if r.Hex != c.hex {
			t.Errorf("%s = %s, want %s", c.el, r.Hex, c.hex)
		}
	}
	r, _ := resolve(t, s, m, `<a:srgbClr `+nsDecl+` val="FF0000"><a:alpha val="40000"/></a:srgbClr>`, nil)
	if r.Alpha != 0.4 || r.Hex != "FF0000" {
		t.Errorf("alpha: %+v", r)
	}
	// A colour map that swaps light and dark.
	inv := ClrMap{"bg1": "dk1", "tx1": "lt1"}
	if r, _ := resolve(t, s, inv, `<a:schemeClr `+nsDecl+` val="tx1"/>`, nil); r.Hex != "FFFFFF" {
		t.Errorf("overridden tx1 = %s", r.Hex)
	}
}

func TestResolvePhClr(t *testing.T) {
	s := ParseScheme(parse(t, scheme))
	ph, _ := ParseColor(parse(t, `<a:schemeClr `+nsDecl+` val="accent1"><a:lumMod val="75000"/></a:schemeClr>`))
	// The style entry's own modifiers follow the reference colour's.
	r, err := resolve(t, s, ClrMap{}, `<a:schemeClr `+nsDecl+` val="phClr"/>`, &ph)
	if err != nil || r.Hex != "2F5597" {
		t.Errorf("phClr = %+v %v", r, err)
	}
	if _, err := resolve(t, s, ClrMap{}, `<a:schemeClr `+nsDecl+` val="phClr"/>`, nil); !errors.Is(err, ErrPhClr) {
		t.Errorf("phClr without reference: %v", err)
	}
}

func TestResolveFailures(t *testing.T) {
	s := ParseScheme(parse(t, scheme))
	for el, want := range map[string]error{
		`<a:schemeClr ` + nsDecl + ` val="accent6"/>`: ErrUnresolvedScheme,
		`<a:sysClr ` + nsDecl + ` val="window"/>`:     ErrSysNoLastClr,
		`<a:prstClr ` + nsDecl + ` val="noSuch"/>`:    ErrUnknownPreset,
	} {
		if _, err := resolve(t, s, ClrMap{}, el, nil); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", el, err, want)
		}
	}
}

func TestSelectBranch(t *testing.T) {
	ac := parse(t, `<mc:AlternateContent `+nsDecl+`>
<mc:Choice xmlns:am3d="http://schemas.microsoft.com/office/drawing/2017/model3d" Requires="am3d"><p:sp/></mc:Choice>
<mc:Choice xmlns:p14="http://schemas.microsoft.com/office/powerpoint/2010/main" xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main" Requires="p14 a14"><p:pic/></mc:Choice>
<mc:Fallback><p:graphicFrame/></mc:Fallback></mc:AlternateContent>`)
	b := SelectBranch(ac)
	if b.Fallback || b.Requires != "p14 a14" || b.Node.Elements()[0].Local() != "pic" {
		t.Errorf("chose %+v", b)
	}
	if len(b.Unsupported) != 1 {
		t.Errorf("unsupported %v", b.Unsupported)
	}
	ac = parse(t, `<mc:AlternateContent `+nsDecl+`>
<mc:Choice xmlns:x="urn:unknown" Requires="x"><p:sp/></mc:Choice>
<mc:Choice Requires="undeclared"><p:sp/></mc:Choice>
<mc:Fallback><p:graphicFrame/></mc:Fallback></mc:AlternateContent>`)
	if b := SelectBranch(ac); !b.Fallback {
		t.Errorf("unsupported choices: chose %+v", b)
	}
	ac = parse(t, `<mc:AlternateContent `+nsDecl+`><mc:Choice xmlns:x="urn:unknown" Requires="x"><p:sp/></mc:Choice></mc:AlternateContent>`)
	if b := SelectBranch(ac); b.Node != nil {
		t.Errorf("no branch applies: chose %+v", b)
	}
}

func TestFindPlaceholder(t *testing.T) {
	sp := func(id, ph string) string {
		return `<p:sp><p:nvSpPr><p:cNvPr id="` + id + `" name=""/><p:cNvSpPr/><p:nvPr>` + ph + `</p:nvPr></p:nvSpPr><p:spPr/></p:sp>`
	}
	layout := parse(t, `<p:spTree `+nsDecl+`>`+
		sp("2", `<p:ph type="title"/>`)+
		sp("3", `<p:ph idx="1"/>`)+
		sp("4", `<p:ph sz="half" idx="2"/>`)+
		sp("5", `<p:ph type="dt" sz="half" idx="10"/>`)+
		`<p:grpSp><p:nvGrpSpPr/><p:grpSpPr/>`+sp("6", `<p:ph type="pic" idx="13"/>`)+`</p:grpSp>`+
		`</p:spTree>`)
	master := parse(t, `<p:spTree `+nsDecl+`>`+
		sp("2", `<p:ph type="title"/>`)+
		sp("3", `<p:ph type="body" idx="1"/>`)+
		sp("4", `<p:ph type="dt" sz="half" idx="2"/>`)+
		`</p:spTree>`)
	id := func(n *tree.Node) string {
		if n == nil {
			return "none"
		}
		v, _ := n.Elements()[0].Elements()[0].Attr("id")
		return v
	}
	cases := []struct {
		tree *tree.Node
		ref  PhRef
		want string
	}{
		{layout, PhRef{Type: "title", HasType: true}, "2"},
		{layout, PhRef{Type: "ctrTitle", HasType: true}, "2"}, // falls back to title
		{layout, PhRef{Idx: "1"}, "3"},
		{layout, PhRef{Idx: "2"}, "4"},
		{layout, PhRef{Type: "dt", HasType: true, Idx: "10"}, "5"},
		{layout, PhRef{Type: "pic", HasType: true, Idx: "13"}, "6"}, // inside a group
		{master, PhRef{Idx: "1"}, "3"},                              // obj -> body
		{master, PhRef{Type: "subTitle", HasType: true, Idx: "1"}, "3"},
		{master, PhRef{Type: "dt", HasType: true, Idx: "10"}, "4"}, // type wins over idx
		{master, PhRef{Type: "sldNum", HasType: true, Idx: "12"}, "none"},
	}
	for _, c := range cases {
		if got := id(FindPlaceholder(c.tree, c.ref)); got != c.want {
			t.Errorf("%+v: got %s, want %s", c.ref, got, c.want)
		}
	}
	if MasterTextStyle("ctrTitle") != "titleStyle" || MasterTextStyle("obj") != "bodyStyle" || MasterTextStyle("ftr") != "otherStyle" {
		t.Error("MasterTextStyle")
	}
}
