package derive

import (
	"errors"
	"strconv"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
)

func TestGuideOperators(t *testing.T) {
	g, err := newGuides(200, 100, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]float64{
		"*/ w 3 4":      150,
		"+- w h 50":     250,
		"+/ w h 3":      100,
		"?: 1 w h":      200,
		"?: 0 w h":      100,
		"?: -1 w h":     100,
		"abs -7":        7,
		"at2 1 1":       2700000,
		"cat2 10 3 4":   6,
		"sat2 10 3 4":   8,
		"cos 10 cd4":    0,
		"sin 10 cd4":    10,
		"tan 10 cd8":    10,
		"max w h":       200,
		"min w h":       100,
		"mod 3 4 12":    13,
		"pin 0 150 100": 100,
		"pin 0 -5 100":  0,
		"pin 0 50 100":  50,
		"sqrt 144":      12,
		"val 7":         7,
		"val ss":        100,
		"val ls":        200,
		"val ssd8":      12.5,
		"val wd32":      6.25,
		"val hd10":      10,
		"val 3cd4":      16200000,
		"val cd3":       7200000,
		"*/ hc vc 1":    5000,
	}
	for f, want := range cases {
		got, err := g.Eval(f)
		if err != nil {
			t.Errorf("%q: %v", f, err)
			continue
		}
		if !near(got, want) {
			t.Errorf("%q = %g, want %g", f, got, want)
		}
	}
	for _, bad := range []string{"", "foo 1", "*/ 1 2", "*/ 1 2 0", "val nosuch", "sqrt -1"} {
		if _, err := g.Eval(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func sitesOf(t *testing.T, prst string, adj []Adjust, x Xfrm) []Site {
	t.Helper()
	p, err := Place(x, Root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := PresetSites(prst, adj, x.W, x.H, p.M)
	if err != nil {
		t.Fatalf("%s: %v", prst, err)
	}
	return s
}

func TestPresetSitesRect(t *testing.T) {
	// rect: 0 top, 1 left, 2 bottom, 3 right (presetShapeDefinitions.xml).
	s := sitesOf(t, "rect", nil, xf(100, 100, 100, 50))
	want := [][2]float64{{150, 100}, {100, 125}, {150, 150}, {200, 125}}
	if len(s) != 4 {
		t.Fatalf("%d sites", len(s))
	}
	for i, w := range want {
		if !ptNear(s[i].Pt, w[0], w[1]) {
			t.Errorf("site %d = %v, want %v", i, Pt{X: s[i].X / pt, Y: s[i].Y / pt}, w)
		}
	}
	if s[0].Ang != 270 || s[3].Ang != 0 {
		t.Errorf("angles %g %g", s[0].Ang, s[3].Ang)
	}
}

func TestPresetSitesRotated(t *testing.T) {
	x := xf(100, 100, 100, 50)
	x.Rot = 90
	s := sitesOf(t, "rect", nil, x)
	// Centre (150,125); the top site (0,-25) rotates to (25,0).
	if !ptNear(s[0].Pt, 175, 125) || s[0].Ang != 0 {
		t.Errorf("rotated top site = %v ang %g", Pt{X: s[0].X / pt, Y: s[0].Y / pt}, s[0].Ang)
	}
	x.Rot, x.FlipH = 0, true
	s = sitesOf(t, "rect", nil, x)
	if !ptNear(s[1].Pt, 200, 125) || !ptNear(s[3].Pt, 100, 125) {
		t.Errorf("flipped left/right sites = %v %v", s[1].Pt, s[3].Pt)
	}
}

func TestPresetSitesEllipse(t *testing.T) {
	// Eight sites starting at the top, then the top-left diagonal.
	s := sitesOf(t, "ellipse", nil, xf(0, 0, 200, 100))
	if len(s) != 8 {
		t.Fatalf("%d sites", len(s))
	}
	c, sn := geom.SinCosDeg(45)
	if !ptNear(s[0].Pt, 100, 0) || !ptNear(s[2].Pt, 0, 50) || !ptNear(s[1].Pt, 100-100*sn, 50-50*c) {
		t.Errorf("ellipse sites %v", s[:3])
	}
}

func TestPresetSitesAdjust(t *testing.T) {
	// can: site 0 is the top of the front face at y2 = 2 * ss * a / 200000.
	x := xf(0, 0, 100, 200)
	def := sitesOf(t, "can", nil, x)
	if !ptNear(def[0].Pt, 50, 25) { // adj 25000 on ss 100
		t.Errorf("can default site 0 = %v", def[0].Pt)
	}
	set := sitesOf(t, "can", []Adjust{{"adj", "val 50000"}}, x)
	if !ptNear(set[0].Pt, 50, 50) {
		t.Errorf("can adj 50000 site 0 = %v", set[0].Pt)
	}
	// Pinned to maxAdj = 50000 * h / ss = 100000.
	big := sitesOf(t, "can", []Adjust{{"adj", "val 900000"}}, x)
	if !ptNear(big[0].Pt, 50, 100) {
		t.Errorf("can pinned site 0 = %v", big[0].Pt)
	}
}

func TestPresetSitesTable(t *testing.T) {
	// Every tabulated preset evaluates at an ordinary size, and the
	// table has the presets the design calls for.
	for _, p := range SitePresets() {
		if _, err := PresetSites(p, nil, 100*pt, 60*pt, Identity); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{"rect", "roundRect", "ellipse", "diamond", "triangle", "parallelogram", "hexagon", "can", "flowChartProcess", "flowChartDecision", "flowChartTerminator", "flowChartDocument", "flowChartMagneticDisk"} {
		if !HasSiteTable(p) {
			t.Errorf("%s not in the site table", p)
		}
	}
	if _, err := PresetSites("star5", nil, 1, 1, Identity); !errors.Is(err, ErrNoSiteTable) {
		t.Errorf("star5: %v", err)
	}
}

func TestCustomSites(t *testing.T) {
	c := CustomGeom{
		Gd:  []Adjust{{"x1", "*/ w 1 4"}},
		Cxn: [][3]string{{"0", "x1", "0"}, {"cd4", "r", "b"}},
	}
	s, err := CustomSites(c, 400, 100, geom.Translate(10, 20))
	if err != nil {
		t.Fatal(err)
	}
	if s[0].X != 110 || s[0].Y != 20 || s[1].X != 410 || s[1].Y != 120 || s[1].Ang != 90 {
		t.Errorf("custom sites %+v", s)
	}
}

func TestCustomPathSVG(t *testing.T) {
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	c := CustomGeom{Paths: []CustomPath{{W: "100", H: "100", Cmds: []CustomCmd{
		{Op: "moveTo", Pts: [][2]string{{"0", "0"}}},
		{Op: "lnTo", Pts: [][2]string{{"100", "0"}}},
		{Op: "cubicBezTo", Pts: [][2]string{{"100", "50"}, {"50", "100"}, {"0", "100"}}},
		{Op: "quadBezTo", Pts: [][2]string{{"0", "50"}, {"0", "0"}}},
		{Op: "close"},
	}}}}
	got, err := CustomPathSVG(c, 200, 50, 1, 2, num)
	if err != nil {
		t.Fatal(err)
	}
	want := "M 0 0 L 200 0 C 200 50 100 100 0 100 Q 0 50 0 0 Z"
	if got != want {
		t.Errorf("path %q, want %q", got, want)
	}
	c.Paths[0].Cmds = append(c.Paths[0].Cmds, CustomCmd{Op: "arcTo"})
	if _, err := CustomPathSVG(c, 200, 50, 1, 1, num); !errors.Is(err, ErrArc) {
		t.Errorf("arcTo: %v", err)
	}
}

func connOf(t *testing.T, prst string, adj []Adjust, x Xfrm) Connector {
	t.Helper()
	p, err := Place(x, Root)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ConnectorGeom(prst, adj, x.W, x.H, p.M)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConnectorStraight(t *testing.T) {
	x := xf(200, 100, 100, 50)
	c := connOf(t, "straightConnector1", nil, x)
	if !ptNear(c.From, 200, 100) || !ptNear(c.To, 300, 150) || c.First != "" {
		t.Errorf("straight %+v", c)
	}
	x.FlipV = true
	c = connOf(t, "straightConnector1", nil, x)
	if !ptNear(c.From, 200, 150) || !ptNear(c.To, 300, 100) {
		t.Errorf("flipV straight %v -> %v", c.From, c.To)
	}
	x.FlipV, x.FlipH = false, true
	c = connOf(t, "line", nil, x)
	if !ptNear(c.From, 300, 100) || !ptNear(c.To, 200, 150) {
		t.Errorf("flipH line %v -> %v", c.From, c.To)
	}
}

func TestConnectorElbow(t *testing.T) {
	c := connOf(t, "bentConnector3", []Adjust{{"adj1", "val 25000"}}, xf(150, 150, 150, 100))
	if c.First != "h" || len(c.Elbows) != 2 || !ptNear(c.Elbows[0], 187.5, 150) || !ptNear(c.Elbows[1], 187.5, 250) || !ptNear(c.To, 300, 250) {
		t.Errorf("bentConnector3 %+v", c)
	}
	// Rotated 90: the first segment runs down.
	x := xf(375, 125, 100, 50)
	x.Rot = 90
	c = connOf(t, "bentConnector2", nil, x)
	if c.First != "v" || !ptNear(c.From, 450, 100) || !ptNear(c.Elbows[0], 450, 200) || !ptNear(c.To, 400, 200) {
		t.Errorf("rotated bentConnector2 %+v", c)
	}
	// connectors.pptx (LibreOffice sd/qa) id 6: rot 270, flipV, glued at
	// the top centre of a rectangle at (2555742, 1836736) 720000 square.
	c6 := Xfrm{X: 1708787, Y: 629780, W: 840410, H: 1573501, Rot: 270, FlipV: true}
	p, _ := Place(c6, Root)
	g, err := ConnectorGeom("bentConnector2", nil, c6.W, c6.H, p.M)
	if err != nil {
		t.Fatal(err)
	}
	if !near(g.From.X, 2555742+360000+0.5) || !near(g.From.Y, 1836736-0.5) || g.First != "v" {
		t.Errorf("connectors.pptx id 6 from %v first %s", g.From, g.First)
	}
	// bentConnector4 and 5 have two and three elbows.
	if c := connOf(t, "bentConnector4", nil, xf(0, 0, 100, 100)); len(c.Elbows) != 3 {
		t.Errorf("bentConnector4 elbows %v", c.Elbows)
	}
	if c := connOf(t, "bentConnector5", nil, xf(0, 0, 100, 100)); len(c.Elbows) != 4 {
		t.Errorf("bentConnector5 elbows %v", c.Elbows)
	}
}

func TestConnectorCurved(t *testing.T) {
	c := connOf(t, "curvedConnector3", []Adjust{{"adj1", "val 30000"}}, xf(0, 0, 100, 40))
	if !ptNear(c.From, 0, 0) || !ptNear(c.To, 100, 40) || c.Curve == nil || c.First != "" || c.Elbows != nil {
		t.Errorf("curved %+v", c)
	}
	if _, err := ConnectorGeom("rect", nil, 1, 1, Identity); !errors.Is(err, ErrNotConnector) {
		t.Errorf("rect as connector: %v", err)
	}
}

func TestStaleGlueAndFrame(t *testing.T) {
	if _, stale := StaleGlue(Pt{X: 0, Y: 0}, Pt{X: 0.4 * pt, Y: 0}); stale {
		t.Error("0.4 pt is within tolerance")
	}
	if d, stale := StaleGlue(Pt{X: 0, Y: 0}, Pt{X: 3 * pt, Y: 4 * pt}); !stale || !near(d, 5*pt) {
		t.Errorf("5 pt: %g %v", d, stale)
	}
	g := GroupXfrm{Xfrm: xf(0, 0, 50, 50), ChW: 100 * pt, ChH: 100 * pt}
	if d := OutsideFrame(xf(10, 10, 80, 80), g); d != 0 {
		t.Errorf("inside: %g", d)
	}
	if d := OutsideFrame(xf(90, 0, 20, 10), g); !near(d, 5*pt) {
		t.Errorf("10 child units out at scale 0.5 = %g pt", d/pt)
	}
	// A child at 270 degrees is bounded by its exchanged extents.
	r := xf(30, -20, 40, 140)
	r.Rot = 270
	if b := BoundsBox(r); !near(b.X, -20*pt) || !near(b.Y, 30*pt) || !near(b.W, 140*pt) {
		t.Errorf("bounds box %+v", b)
	}
	r.Rot = 10
	if b := BoundsBox(r); !near(b.W, 40*pt) {
		t.Errorf("bounds box at 10 degrees %+v", b)
	}
}
