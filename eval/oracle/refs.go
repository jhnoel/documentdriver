package oracle

import (
	"fmt"
	"math"
)

// Ref is a reference solution: it edits the slide in place.
type Ref func(e *Ed) error

// Answers are the reference and a wrong answer for each Identify task.
var Answers = map[string][2]string{
	"id-ow-gateway":           {"216", "136"},
	"id-ow-python":            {"s129", "s512"},
	"id-sp7-twin-links":       {"7\n60", "s7, s67"},
	"id-co11-studying-target": {"slide11#16", "Content Placeholder 6"},
	"id-aa33-teachers-links":  {"Oval 42\nOval 43", "s43\ns12"},
	"id-cg6-topics-callout":   {"s20", "s23"},
}

const (
	black    = `<a:srgbClr val="000000"/>`
	aaBlue   = `<a:schemeClr val="accent1"><a:shade val="95000"/><a:satMod val="105000"/></a:schemeClr>`
	accent   = `<a:schemeClr val="%s"/>`
	whiteClr = `<a:srgbClr val="FFFFFF"/>`
)

// scheme returns a scheme colour fragment for slot s.
func scheme(s string) string { return fmt.Sprintf(accent, s) }

// fills returns a reference that sets colour as fill on each id.
func fills(ids []string, colour string) Ref {
	return func(e *Ed) error {
		for _, id := range ids {
			if err := e.SetFill(id, colour); err != nil {
				return err
			}
		}
		return nil
	}
}

// moveBoxTo moves a top-level object so its box top-left is at (x, y)
// points; NaN keeps that coordinate exactly.
func (e *Ed) moveBoxTo(id string, x, y float64) error {
	o, err := e.Obj(id)
	if err != nil {
		return err
	}
	var dx, dy int64
	if !math.IsNaN(x) {
		dx = int64(math.Round(x*emu - o.Box.X))
	}
	if !math.IsNaN(y) {
		dy = int64(math.Round(y*emu - o.Box.Y))
	}
	if err := e.MoveBy(id, dx, dy); err != nil {
		return err
	}
	return e.Refresh()
}

// alignTopTo moves objects so their tops equal ref's exact top.
func (e *Ed) alignTopTo(ref string, ids ...string) error {
	r, err := e.Obj(ref)
	if err != nil {
		return err
	}
	y := r.Box.Y / emu
	for _, id := range ids {
		if err := e.moveBoxTo(id, math.NaN(), y); err != nil {
			return err
		}
	}
	return nil
}

var nan = math.NaN()

// Refs holds the reference solution of every editing task.
var Refs = map[string]Ref{
	// Restyle.
	"rs-sp7-white-boxes":  fills([]string{"s6", "s50", "s51", "s58", "s69", "s71"}, scheme("accent2")),
	"rs-aa33-triangles":   fills([]string{"s45", "s46", "s51", "s52", "s53"}, scheme("accent4")),
	"rs-hpc8-answer-card": fills([]string{"s21"}, `<a:srgbClr val="2E2585"/>`),
	"rs-cg6-callouts":     fills([]string{"s19", "s20", "s23"}, scheme("accent2")),
	"rs-ow-source-links": func(e *Ed) error {
		for _, id := range []string{"s281", "s282", "s283", "s284"} {
			if err := e.SetLine(id, 25400, `<a:srgbClr val="FF0000"/>`); err != nil {
				return err
			}
		}
		return nil
	},

	// Move and align.
	"mv-el2-year-labels": func(e *Ed) error {
		ids := []string{"s59", "s60", "s61", "s62", "s63", "s64", "s65", "s66", "s69", "s70"}
		first, err := e.Obj(ids[0])
		if err != nil {
			return err
		}
		last, err := e.Obj(ids[len(ids)-1])
		if err != nil {
			return err
		}
		x0, x1, y := first.Box.X/emu, last.Box.X/emu, first.Box.Y/emu
		for i, id := range ids[1 : len(ids)-1] {
			if err := e.moveBoxTo(id, x0+float64(i+1)*(x1-x0)/9, y); err != nil {
				return err
			}
		}
		return e.moveBoxTo(ids[len(ids)-1], nan, y)
	},
	"mv-el2-offslide-box": func(e *Ed) error {
		if err := e.moveBoxTo("s127", 835.7, nan); err != nil {
			return err
		}
		o, _ := e.Obj("s127")
		return e.Reglue("s128", Conn{From: "s127", FromSite: 0, ToPt: [2]float64{(o.Box.X + o.Box.W/2) / emu, 289.2}})
	},
	"mv-sp7-top-row": func(e *Ed) error {
		if err := e.alignTopTo("s69", "s71", "s58"); err != nil {
			return err
		}
		if err := e.Reglue("s72", Conn{From: "s69", FromSite: 3, To: "s71", ToSite: 1}); err != nil {
			return err
		}
		return e.Reglue("s66", Conn{From: "s71", FromSite: 3, To: "s63", ToSite: 0})
	},
	"mv-hd4-center-labels": func(e *Ed) error {
		pairs := [][2]string{{"s4", "s5"}, {"s7", "s8"}, {"s10", "s11"}, {"s13", "s14"}, {"s16", "s17"}, {"s19", "s20"}, {"s22", "s23"}}
		for _, p := range pairs {
			sq, err := e.Obj(p[0])
			if err != nil {
				return err
			}
			lb, err := e.Obj(p[1])
			if err != nil {
				return err
			}
			if err := e.moveBoxTo(p[1], (sq.Box.X+sq.Box.W/2-lb.Box.W/2)/emu, nan); err != nil {
				return err
			}
		}
		return nil
	},
	"mv-hpc8-level-dash": func(e *Ed) error { return e.alignTopTo("s38", "s40") },

	// Connect.
	"cn-sp7-physical-digital": func(e *Ed) error {
		_, err := e.AddConn(Conn{Geom: "straightConnector1", From: "s50", FromSite: 3, To: "s51", ToSite: 1, LineW: 9525, Line: black, ArrowAtEnd: true})
		return err
	},
	"cn-hd4-step1-step2": func(e *Ed) error {
		if err := e.Delete("s6"); err != nil {
			return err
		}
		if err := e.Refresh(); err != nil {
			return err
		}
		_, err := e.AddConn(Conn{Geom: "straightConnector1", From: "s4", FromSite: 3, To: "s7", ToSite: 1, LineW: 15875, Line: `<a:srgbClr val="5E6A72"/>`, ArrowAtEnd: true})
		return err
	},
	"cn-co11-attach-arrow": func(e *Ed) error {
		return e.Reglue("s19", Conn{From: "s5", FromSite: 3, To: "s8", ToSite: 1})
	},
	"cn-lc1-square-circle": func(e *Ed) error {
		_, err := e.AddConn(Conn{Geom: "bentConnector3", From: "s2", FromSite: 2, To: "s28", ToSite: 0, VertFirst: true, LineW: 12700, Line: scheme("accent1")})
		return err
	},
	"cn-aa33-employers-peers": func(e *Ed) error {
		_, err := e.AddConn(Conn{Geom: "line", From: "s6", FromSite: 6, To: "s36", ToSite: 2, LineW: 25400, Line: aaBlue})
		return err
	},

	// Insert.
	"in-sp7-twin-model": func(e *Ed) error {
		m, err := e.AddShape(Shape{Name: "Rectangle", Geom: "rect", Box: [4]float64{210, 219, 70, 24}, Fill: scheme("bg1"), Line: black, LineW: 19050, Text: "Twin model", SizePt: 9, TextColour: black})
		if err != nil {
			return err
		}
		if err := e.Reglue("s7", Conn{From: "s3", FromSite: 2, To: m, ToSite: 1, VertFirst: true}); err != nil {
			return err
		}
		_, err = e.AddConn(Conn{Geom: "straightConnector1", From: m, FromSite: 3, To: "s6", ToSite: 1, LineW: 9525, Line: black, ArrowAtEnd: true})
		return err
	},
	"in-sp7-sync": func(e *Ed) error {
		s, err := e.AddShape(Shape{Name: "Rectangle", Geom: "rect", Box: [4]float64{395, 165, 40, 24}, Fill: scheme("bg1"), Line: black, LineW: 19050, Text: "Sync", SizePt: 9, TextColour: black})
		if err != nil {
			return err
		}
		if err := e.Reglue("s60", Conn{From: "s3", FromSite: 3, To: s, ToSite: 1}); err != nil {
			return err
		}
		_, err = e.AddConn(Conn{Geom: "straightConnector1", From: s, FromSite: 3, To: "s57", ToSite: 1, LineW: 9525, Line: black, ArrowAtEnd: true})
		return err
	},
	"in-ow-webhooks": func(e *Ed) error {
		w, err := e.AddShape(Shape{Name: "Rectangle", Geom: "roundRect", Box: [4]float64{230, 390, 110, 30}, Fill: scheme("accent6"), Line: `<a:srgbClr val="1E5E70"/>`, LineW: 9525, Text: "Webhooks", SizePt: 12, TextColour: black})
		if err != nil {
			return err
		}
		_, err = e.AddConn(Conn{Geom: "bentConnector3", From: w, FromSite: 3, To: "s216", ToSite: 1, LineW: 9525, Line: `<a:srgbClr val="1E5E70"/>`, ArrowAtEnd: true})
		return err
	},
	"in-aa33-mentoring": func(e *Ed) error {
		m, err := e.AddShape(Shape{Name: "Oval", Geom: "ellipse", Box: [4]float64{6, 196, 112, 60}, Fill: `<a:srgbClr val="00B0F0"/>`, Line: black, LineW: 19050, Text: "Mentoring Programs", SizePt: 11, TextColour: black})
		if err != nil {
			return err
		}
		_, err = e.AddConn(Conn{Geom: "line", From: "s38", FromSite: 2, To: m, ToSite: 6, LineW: 25400, Line: aaBlue})
		return err
	},

	// Author.
	"au-ow-chevrons": func(e *Ed) error {
		for i, t := range []string{"Event", "Trigger", "Rule", "Action", "Result"} {
			if _, err := e.AddShape(Shape{Name: "Chevron", Geom: "chevron", Box: [4]float64{190 + float64(i)*120, 420, 110, 40}, Fill: scheme("accent5"), Text: t, SizePt: 12, TextColour: whiteClr}); err != nil {
				return err
			}
		}
		return nil
	},
	"au-sp3-orgchart": func(e *Ed) error {
		box := func(t string, x, y float64) (string, error) {
			return e.AddShape(Shape{Name: "Rectangle", Geom: "rect", Box: [4]float64{x, y, 85, 30}, Fill: scheme("accent3"), Line: black, LineW: 12700, Text: t, SizePt: 11, TextColour: black})
		}
		c, err := box("Coordinator", 72.5, 220)
		if err != nil {
			return err
		}
		p, err := box("Partners", 25, 300)
		if err != nil {
			return err
		}
		u, err := box("Users", 120, 300)
		if err != nil {
			return err
		}
		for _, k := range []string{p, u} {
			if _, err := e.AddConn(Conn{Geom: "bentConnector3", From: c, FromSite: 2, To: k, ToSite: 0, VertFirst: true, LineW: 12700, Line: black}); err != nil {
				return err
			}
		}
		return nil
	},
	"au-hd4-quarters": func(e *Ed) error {
		grey := `<a:srgbClr val="5E6A72"/>`
		if _, err := e.AddShape(Shape{Name: "Straight Connector", Geom: "line", Box: [4]float64{120, 440, 720, 0}, Line: grey, LineW: 19050}); err != nil {
			return err
		}
		for i, q := range []string{"Q1", "Q2", "Q3", "Q4"} {
			x := 210 + float64(i)*180
			if _, err := e.AddShape(Shape{Name: "Oval", Geom: "ellipse", Box: [4]float64{x - 7, 433, 14, 14}, Fill: `<a:srgbClr val="1D5B8F"/>`}); err != nil {
				return err
			}
			if _, err := e.AddShape(Shape{Name: "TextBox", Geom: "rect", Box: [4]float64{x - 20, 449, 40, 20}, Text: q, SizePt: 11, TextColour: grey}); err != nil {
				return err
			}
		}
		return nil
	},

	// Freeform.
	"ff-ss1-numeral-four": func(e *Ed) error {
		pts := [][2]float64{{100, 0}, {130, 0}, {130, 110}, {155, 110}, {155, 135}, {130, 135}, {130, 170}, {100, 170}, {100, 135}, {5, 135}, {5, 110}}
		_, err := e.AddShape(Shape{Name: "Freeform", Box: [4]float64{60, 320, 160, 170}, Path: emuPts(pts), Fill: scheme("accent2")})
		return err
	},
	"ff-sp7-callout": func(e *Ed) error {
		pts := [][2]float64{{0, 40}, {100, 40}, {120, 1}, {112, 40}, {140, 40}, {140, 90}, {0, 90}}
		_, err := e.AddShape(Shape{Name: "Freeform", Box: [4]float64{30, 198, 140, 90}, Path: emuPts(pts), Fill: scheme("bg1"), Line: black, LineW: 9525, Text: "Core concept", SizePt: 10, TextColour: black, Anchor: "b"})
		return err
	},
}

// emuPts converts point pairs to EMU pairs, rounded per coordinate.
func emuPts(p [][2]float64) [][2]int64 {
	out := make([][2]int64, len(p))
	for i, q := range p {
		out[i] = [2]int64{int64(math.Round(q[0] * emu)), int64(math.Round(q[1] * emu))}
	}
	return out
}
