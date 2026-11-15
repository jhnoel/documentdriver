package model

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

func readFirst(t *testing.T, path string) (*Deck, *Slide) {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	d, err := OpenDeck(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.ReadSlide(d.Slides[0])
	if err != nil {
		t.Fatal(err)
	}
	return d, s
}

func byID(s *Slide) map[string]*Object {
	m := map[string]*Object{}
	for _, o := range s.Objects {
		m[o.ID] = o
	}
	return m
}

func pts(v float64) float64 { return math.Round(v/EMUPerPoint*100) / 100 }

func prop(o *Object, key string) (Prop, bool) {
	for _, p := range o.Props {
		if p.Key == key {
			return p, true
		}
	}
	return Prop{}, false
}

func omitted(o *Object, prop string) string {
	for _, x := range o.Omitted {
		if x.Prop == prop {
			return x.Reason
		}
	}
	return ""
}

func TestReadDiagram(t *testing.T) {
	_, s := readFirst(t, testdeck.Diagram(t))
	o := byID(s)
	if s.Name != "slide1" || pts(s.W) != 960 || pts(s.H) != 540 {
		t.Errorf("slide %s %gx%g", s.Name, pts(s.W), pts(s.H))
	}

	// Placeholders inherit their boxes, with origins.
	title := o["s2"]
	if title.Box == nil || pts(title.Box.X) != 50 || pts(title.Box.W) != 860 || title.Origin["box"] != FromLayout {
		t.Errorf("title box %+v origin %s", title.Box, title.Origin["box"])
	}
	body := o["s3"]
	if body.Box == nil || pts(body.Box.Y) != 120 || body.Origin["box"] != FromMaster {
		t.Errorf("body box %+v origin %s", body.Box, body.Origin["box"])
	}
	// Text defaults: alignment from the layout, size from the master's
	// title style, anchor from the master placeholder, insets from the
	// schema defaults.
	for _, c := range []struct {
		key, val string
		org      Origin
	}{
		{"text.lvl1.align", "ctr", FromLayout},
		{"text.lvl1.size", "44", FromMaster},
		{"text.lvl1.color", "tx1 #000000", FromMaster},
		{"text.lvl1.font", "+mj-lt", FromMaster},
	} {
		if p, ok := prop(title, c.key); !ok || p.Value != c.val || p.Origin != c.org {
			t.Errorf("title %s = %+v, want %s from %s", c.key, p, c.val, c.org)
		}
	}
	if title.Text.Anchor != "b" || title.Origin["text.anchor"] != FromMaster || title.Origin["text.wrap"] != FromSpecDflt {
		t.Errorf("title body props %+v %v", title.Text, title.Origin)
	}
	if p, _ := prop(body, "text.lvl2.size"); p.Value != "24" || p.Origin != FromMaster {
		t.Errorf("body lvl2 size %+v", p)
	}
	if p, _ := prop(body, "text.lvl2.bullet"); p.Value != "char:-" {
		t.Errorf("body lvl2 bullet %+v", p)
	}
	if *body.Text.Insets[0] != 0 || body.Origin["text.insets.lIns"] != FromMaster {
		t.Errorf("body lIns %v from %s", *body.Text.Insets[0], body.Origin["text.insets.lIns"])
	}

	// Text as HTML: local formatting only.
	api := o["s10"]
	wantHTML := `<p style="text-align:center" lang="en-US"><b>API</b><span style="font-size:12pt;color:#C55A11" data-color="accent2 lumMod=75000 #C55A11"> Gateway &amp; co</span><br/><i><u><span style="font-family:var(--mj-lt)">v2</span></u></i></p><ul><li data-char="•" lang="en-US">bullet</li></ul>`
	if api.Text.HTML != wantHTML {
		t.Errorf("html\n got %s\nwant %s", api.Text.HTML, wantHTML)
	}
	// A non-placeholder's defaults come from otherStyle.
	if p, _ := prop(api, "text.lvl1.size"); p.Value != "18" || p.Origin != FromMaster {
		t.Errorf("API lvl1 size %+v", p)
	}
	// Style fontRef gives the colour of shapes with a style.
	if p, _ := prop(o["s71"], "text.lvl1.color"); p.Value != "lt1 #FFFFFF" || p.Origin != FromStyle {
		t.Errorf("style font colour %+v", p)
	}

	// Fill and line through styles.
	if f := o["s71"].Fill; f == nil || f.Color.Val != "accent3" || o["s71"].Origin["fill"] != FromStyle {
		t.Errorf("style fill %+v", f)
	}
	if l := o["s71"].Line; l == nil || !l.None {
		t.Errorf("lnRef 0 line %+v", l)
	}
	if l := o["s70"].Line; l == nil || l.Color.Val != "accent2" || pts(*l.W) != 1 || o["s70"].Origin["line.w"] != FromStyle {
		t.Errorf("style line %+v", l)
	}
	if f := o["s70"].Fill; f.Color.Hex != "#2F5597" || o["s70"].Origin["fill"] != FromSlide {
		t.Errorf("local fill beats style: %+v", f.Color)
	}
	if f := o["s24"].Fill; f == nil || f.Color.Val != "accent6" || o["s24"].Origin["fill"] != FromGroup {
		t.Errorf("grpFill %+v", f)
	}

	// Connectors and glue.
	c := o["s12"].Conn
	if c.Start.ID != "s10" || c.Start.Site != 3 || c.End.ID != "s11" || c.End.Site != 2 {
		t.Errorf("glue %+v %+v", c.Start, c.End)
	}
	if pts(c.From.X) != 200 || pts(c.To.X) != 300 || pts(c.From.Y) != 125 {
		t.Errorf("straight endpoints %v %v", c.From, c.To)
	}
	if l := o["s12"].Line; l.Tail == nil || l.Tail.Type != "triangle" || l.Tail.W != "sm" || l.Tail.Len != "lg" {
		t.Errorf("tail %+v", l.Tail)
	}

	// Groups: flips and rotation compose.
	if b := o["s21"].Box; pts(b.X) != 650 || !b.FlipH {
		t.Errorf("flipped group child %+v", b)
	}
	if b := o["s26"].Box; pts(b.X) != 135 || pts(b.Y) != 285 || b.Rot != 90 {
		t.Errorf("rotated group child %+v", b)
	}

	// mc:AlternateContent and duplicate ids.
	if o["s30"].Name != "Choice" || o["s31"].Name != "Fallback31" {
		t.Errorf("branches: %s %s", o["s30"].Name, o["s31"].Name)
	}
	if o["s30"].source.Local() != "AlternateContent" {
		t.Errorf("source of s30 is %s", o["s30"].source.Name())
	}
	if o["s40"].Name != "Dup A" || o["s40~2"] == nil || o["s40~2"].Name != "Dup B" {
		t.Error("duplicate ids")
	}

	// Custom geometry.
	if g := o["s50"].Geom; !g.Custom || g.Path == "" || len(o["s50"].Sites) != 2 {
		t.Errorf("custGeom %+v sites %d", g, len(o["s50"].Sites))
	}
	if r := omitted(o["s51"], "geom.path"); !strings.Contains(r, "arcTo") {
		t.Errorf("arcTo omission %q", r)
	}
	if r := omitted(title, "geom"); r == "" {
		t.Error("title geometry should be recorded as omitted")
	}

	// Hazards.
	kinds := map[string]string{}
	for _, h := range s.Hazards {
		kinds[h.ID] = h.Kind
	}
	want := map[string]string{"s24": "group-frame", "s60": "stale-connector", "s61": "dangling-glue"}
	for id, k := range want {
		if kinds[id] != k {
			t.Errorf("hazard on %s = %q, want %s", id, kinds[id], k)
		}
	}
	if len(s.Hazards) != len(want) {
		t.Errorf("hazards %+v", s.Hazards)
	}

	// Z-order and parents.
	if o["s21"].Parent != "s20" || o["s20"].Z >= o["s21"].Z {
		t.Error("z-order/parent")
	}
}

func TestReadNeverWrites(t *testing.T) {
	path := testdeck.Diagram(t)
	before, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := OpenDeck(p)
	if _, err := d.List(); err != nil {
		t.Fatal(err)
	}
	if mod, _ := p.Modified(); mod {
		t.Error("reading modified the package")
	}
	p.Close()
	after, _ := os.ReadFile(path)
	info2, _ := os.Stat(path)
	if string(before) != string(after) || !info.ModTime().Equal(info2.ModTime()) {
		t.Error("reading changed the file")
	}
}

func TestResolveSlide(t *testing.T) {
	d, _ := readFirst(t, testdeck.Diagram(t))
	for _, arg := range []string{"slide1", "slide1.xml", "ppt/slides/slide1.xml", "/ppt/slides/slide1.xml"} {
		if p, err := d.ResolveSlide(arg); err != nil || p != "/ppt/slides/slide1.xml" {
			t.Errorf("%s: %s %v", arg, p, err)
		}
	}
	if _, err := d.ResolveSlide("slide2"); err == nil {
		t.Error("slide2 resolved")
	}
}

// corpusDeck returns a corpus deck path or skips.
func corpusDeck(t *testing.T, rel string) string {
	dir := os.Getenv("PPTX_CORPUS")
	if dir == "" {
		t.Skip("PPTX_CORPUS not set")
	}
	p := filepath.Join(dir, "repos", rel)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("%s not in corpus", rel)
	}
	return p
}

// TestCorpusTargets checks derivations on decks chosen for them.
func TestCorpusTargets(t *testing.T) {
	t.Run("connectors", func(t *testing.T) {
		_, s := readFirst(t, corpusDeck(t, "libreoffice-core/sd/qa/unit/data/pptx/connectors.pptx"))
		var glued int
		for _, o := range s.Objects {
			if o.Kind == Connector && o.Conn != nil {
				if o.Conn.First == "" || o.Conn.First == "?" || len(o.Conn.Elbows) != 1 {
					t.Errorf("%s: first %q elbows %v", o.ID, o.Conn.First, o.Conn.Elbows)
				}
				for _, g := range []*Glue{o.Conn.Start, o.Conn.End} {
					if g != nil {
						glued++
					}
				}
			}
		}
		if glued != 12 || len(s.Hazards) != 0 {
			t.Errorf("glued ends %d, hazards %+v", glued, s.Hazards)
		}
	})
	t.Run("group-rot", func(t *testing.T) {
		_, s := readFirst(t, corpusDeck(t, "libreoffice-core/sd/qa/unit/data/pptx/group-rot.pptx"))
		for _, o := range s.Objects {
			if o.Box == nil {
				t.Errorf("%s: no box: %v", o.ID, o.Omitted)
			}
		}
	})
	t.Run("nesting depth 4", func(t *testing.T) {
		_, s := readFirst(t, corpusDeck(t, "shapecrawler/ShapeCrawler.Tests/Assets/077 grouped shape.pptx"))
		o := byID(s)
		depth := 0
		for _, ob := range s.Objects {
			d := 0
			for p := ob; p.Parent != ""; p = o[p.Parent] {
				d++
			}
			depth = max(depth, d)
			if ob.Box == nil {
				t.Errorf("%s: no box", ob.ID)
			}
		}
		if depth < 4 {
			t.Errorf("max depth %d", depth)
		}
	})
	t.Run("skewed group child", func(t *testing.T) {
		_, s := readFirst(t, corpusDeck(t, "libreoffice-core/oox/qa/unit/data/tdf141463_GroupTransform.pptx"))
		o := byID(s)
		if o["s2"].Box != nil || !strings.Contains(omitted(o["s2"], "box"), "skew") {
			t.Errorf("s2 box %+v omitted %v", o["s2"].Box, o["s2"].Omitted)
		}
		if b := o["s3"].Box; b == nil || b.Rot != 20 {
			t.Errorf("s3 box %+v", b)
		}
	})
	t.Run("alternate content", func(t *testing.T) {
		p := corpusDeck(t, "apache-poi/test-data/integration/stress013.pptx")
		pk, err := opc.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer pk.Close()
		d, _ := OpenDeck(pk)
		branches := map[string]int{}
		for _, part := range d.Slides {
			s, err := d.ReadSlide(part)
			if err != nil {
				t.Fatal(err)
			}
			for _, o := range s.Objects {
				if p, ok := prop(o, "mc.branch"); ok {
					branches[p.Value]++
				}
			}
		}
		if len(branches) == 0 {
			t.Error("no objects in mc:AlternateContent")
		}
		t.Logf("projected branches: %v", branches)
	})
}
