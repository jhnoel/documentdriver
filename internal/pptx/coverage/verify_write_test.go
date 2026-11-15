package coverage

import (
	"archive/zip"
	"fmt"
	"io"
	"maps"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// A writeTmpl says how a plan sets a path: the object it targets (a
// context of the path's root), paths made absent first, the operation's
// fields for a value, and what the path then holds.
type writeTmpl struct {
	pat string
	// ctx is the context of the path's root whose object the plan
	// targets.
	ctx int
	// absent are paths removed before the plan, so that it must create
	// what it claims.
	absent []string
	// op is the operation (object.set when empty).
	op string
	// fields are the operation's fields for value v.
	fields func(v string) map[string]any
	// prep, when set, are the fields of an object.set run first in the
	// same plan: it sets another value, so that writing v is a change
	// even where v is the value shown (a default the specification
	// implies).
	prep func(v string) map[string]any
	// values lists the values tried; nil tries every value of an
	// enumeration, else one sample.
	values []string
	// expect is the value the path holds after the plan (an attribute's
	// value, an element's text); "" checks presence only.
	expect func(v string) string
}

func same(v string) string   { return v }
func presence(string) string { return "" }
func fixed(s string) func(string) string {
	return func(string) string { return s }
}

func black() map[string]any { return map[string]any{"hex": "#000000"} }

func line(kv ...any) func(string) map[string]any {
	return func(v string) map[string]any {
		l := map[string]any{"color": black()}
		for i := 0; i < len(kv); i += 2 {
			k := kv[i].(string)
			switch x := kv[i+1].(type) {
			case func(string) any:
				l[k] = x(v)
			default:
				l[k] = x
			}
		}
		return map[string]any{"line": l}
	}
}

func html(f func(v string) string) func(string) map[string]any {
	return func(v string) map[string]any { return map[string]any{"html": f(v)} }
}

// colorKinds maps a factored colour type to the plan colour key that
// writes it.
var colorKinds = map[string]string{
	"(a:CT_SRgbColor)": "rgb", "(a:CT_SchemeColor)": "scheme", "(a:CT_SystemColor)": "sys", "(a:CT_PresetColor)": "preset",
	"(a:CT_HslColor)": "hsl", "(a:CT_ScRgbColor)": "scrgb",
}

var colorBase = map[string]any{
	"rgb": "4472C4", "scheme": "accent1", "sys": "windowText", "preset": "red",
	"hsl": []int{1200000, 100000, 50000}, "scrgb": []int{100000, 0, 0},
}

// colorComps are the component attributes of the hsl and scRGB colours.
var colorComps = map[string][3]string{"hsl": {"hue", "sat", "lum"}, "scrgb": {"r", "g", "b"}}

var underline = map[string]string{"sng": "<u>X</u>", "none": `<span style="text-decoration:none">X</span>`}

var alignCSS = map[string]string{"l": "left", "ctr": "center", "r": "right", "just": "justify"}

// writeTmpls are the plan templates of the write claims. Every path a
// write claim covers must match one.
func writeTmpls() []writeTmpl {
	var ts []writeTmpl
	add := func(t writeTmpl) { ts = append(ts, t) }
	box := func(i int, v float64) func(string) map[string]any {
		return func(string) map[string]any {
			b := []float64{72, 72, 144, 72}
			b[i] = v
			return map[string]any{"box": b}
		}
	}
	for _, r := range []struct {
		root string
		ctx  int
	}{{"p:sp", 0}, {"p:pic", 0}, {"p:cxnSp", 0}, {"p:grpSp", 0}} {
		x := r.root + "/p:spPr/a:xfrm"
		if r.root == "p:grpSp" {
			x = "p:grpSp/p:grpSpPr/a:xfrm"
		}
		add(writeTmpl{pat: x + "/a:off/@x", fields: box(0, 100), expect: fixed("1270000")})
		add(writeTmpl{pat: x + "/a:off/@y", fields: box(1, 100), expect: fixed("1270000")})
		add(writeTmpl{pat: x + "/a:ext/@cx", fields: box(2, 100), expect: fixed("1270000")})
		add(writeTmpl{pat: x + "/a:ext/@cy", fields: box(3, 100), expect: fixed("1270000")})
		add(writeTmpl{pat: x + "/@rot", fields: func(string) map[string]any { return map[string]any{"rot": 30} }, expect: fixed("1800000")})
		add(writeTmpl{pat: x + "/@flipH", fields: func(string) map[string]any { return map[string]any{"flip_h": true} }, expect: fixed("1")})
		add(writeTmpl{pat: x + "/@flipV", fields: func(string) map[string]any { return map[string]any{"flip_v": true} }, expect: fixed("1")})
	}
	// A placeholder without a transform gets one.
	add(writeTmpl{pat: "p:sp/p:spPr/a:xfrm", ctx: 1, fields: box(0, 100), expect: presence})
	add(writeTmpl{pat: "p:sp/p:spPr/a:xfrm/{a:off,a:ext}", ctx: 1, fields: box(0, 100), expect: presence})
	add(writeTmpl{pat: "{p:pic,p:cxnSp}/p:spPr/a:xfrm", absent: []string{"%s"}, fields: box(0, 100), expect: presence})
	add(writeTmpl{pat: "{p:pic,p:cxnSp}/p:spPr/a:xfrm/{a:off,a:ext}", absent: []string{"%p"}, fields: box(0, 100), expect: presence})

	geom := func(v string) map[string]any { return map[string]any{"geom": v} }
	add(writeTmpl{pat: "*/p:spPr/a:prstGeom/@prst", fields: geom, expect: same})
	add(writeTmpl{pat: "*/p:spPr/a:prstGeom", absent: []string{"%s"}, fields: func(string) map[string]any { return geom("ellipse") }, expect: presence})
	adj := func(string) map[string]any {
		return map[string]any{"geom": "roundRect", "adj": map[string]any{"adj": 30000}}
	}
	add(writeTmpl{pat: "*/p:spPr/a:prstGeom/a:avLst", absent: []string{"%s"}, fields: adj, expect: presence})
	add(writeTmpl{pat: "*/p:spPr/a:prstGeom/a:avLst/a:gd", fields: adj, expect: presence})
	add(writeTmpl{pat: "*/p:spPr/a:prstGeom/a:avLst/a:gd/@name", fields: adj, expect: fixed("adj")})
	add(writeTmpl{pat: "*/p:spPr/a:prstGeom/a:avLst/a:gd/@fmla", fields: adj, expect: fixed("val 30000")})

	// Custom geometry: a path written on the preset shape makes a new
	// custGeom; custom sets its sites and text rectangle.
	cg := "*/p:spPr/a:custGeom"
	path := func(p any) func(string) map[string]any {
		return func(string) map[string]any { return map[string]any{"path": p} }
	}
	cmds := "M 1 2 L 10 0 Q 10 10 0 10 C 0 5 5 5 5 0 Z"
	add(writeTmpl{pat: cg, absent: []string{"%s"}, fields: path(cmds), expect: presence})
	add(writeTmpl{pat: cg + "/{a:avLst,a:gdLst,a:ahLst,a:pathLst}", absent: []string{"%p"}, fields: path(cmds), expect: presence})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path", fields: path(cmds), expect: presence})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/@{w,h}", fields: path(cmds), expect: presence})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/@stroke", fields: path([]any{map[string]any{"d": cmds, "stroke": false}}), expect: fixed("0")})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/@fill", fields: func(v string) map[string]any {
		return map[string]any{"path": []any{map[string]any{"d": cmds, "fill": v}}}
	}, expect: same})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/{a:moveTo,a:lnTo,a:cubicBezTo,a:quadBezTo,a:close}", fields: path(cmds), expect: presence})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/{a:moveTo,a:lnTo,a:cubicBezTo,a:quadBezTo}/a:pt", fields: path(cmds), expect: presence})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/a:moveTo/a:pt/@x", fields: path(cmds), expect: fixed("12700")})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/a:moveTo/a:pt/@y", fields: path(cmds), expect: fixed("25400")})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/a:lnTo/a:pt/@x", fields: path(cmds), expect: fixed("127000")})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/a:lnTo/a:pt/@y", fields: path(cmds), expect: fixed("0")})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/a:quadBezTo/a:pt/@{x,y}", fields: path(cmds), expect: fixed("127000")})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/a:cubicBezTo/a:pt/@x", fields: path(cmds), expect: fixed("0")})
	add(writeTmpl{pat: cg + "/a:pathLst/a:path/a:cubicBezTo/a:pt/@y", fields: path(cmds), expect: fixed("63500")})
	custom := func(string) map[string]any {
		return map[string]any{"path": cmds, "custom": map[string]any{
			"av": []any{}, "gd": []any{}, "ah": []any{}, "cxn": []any{[]any{90, 10, 20}}, "rect": []any{1, 2, "r", "b"},
		}}
	}
	add(writeTmpl{pat: cg + "/a:cxnLst", absent: []string{"%p"}, fields: custom, expect: presence})
	add(writeTmpl{pat: cg + "/a:cxnLst/a:cxn", fields: custom, expect: presence})
	add(writeTmpl{pat: cg + "/a:cxnLst/a:cxn/@ang", fields: custom, expect: fixed("5400000")})
	add(writeTmpl{pat: cg + "/a:cxnLst/a:cxn/a:pos", fields: custom, expect: presence})
	add(writeTmpl{pat: cg + "/a:cxnLst/a:cxn/a:pos/@x", fields: custom, expect: fixed("127000")})
	add(writeTmpl{pat: cg + "/a:cxnLst/a:cxn/a:pos/@y", fields: custom, expect: fixed("254000")})
	add(writeTmpl{pat: cg + "/a:rect", absent: []string{"%p"}, fields: custom, expect: presence})
	add(writeTmpl{pat: cg + "/a:rect/@l", fields: custom, expect: fixed("12700")})
	add(writeTmpl{pat: cg + "/a:rect/@t", fields: custom, expect: fixed("25400")})
	add(writeTmpl{pat: cg + "/a:rect/@r", fields: custom, expect: fixed("r")})
	add(writeTmpl{pat: cg + "/a:rect/@b", fields: custom, expect: fixed("b")})

	add(writeTmpl{pat: "*/*/p:cNvPr/@name", fields: func(string) map[string]any { return map[string]any{"name": "Renamed"} }, expect: fixed("Renamed")})

	fill := func(c map[string]any) func(string) map[string]any {
		return func(string) map[string]any { return map[string]any{"fill": c} }
	}
	for _, fp := range []string{"p:sp/p:spPr", "p:grpSp/p:grpSpPr"} {
		add(writeTmpl{pat: fp + "/a:noFill", fields: func(string) map[string]any { return map[string]any{"fill": "none"} }, expect: presence})
		add(writeTmpl{pat: fp + "/a:solidFill", fields: fill(map[string]any{"hex": "#FF0000"}), expect: presence})
		add(writeTmpl{pat: fp + "/a:solidFill/a:srgbClr", fields: fill(map[string]any{"hex": "#FF0000"}), expect: presence})
		add(writeTmpl{pat: fp + "/a:solidFill/a:schemeClr", fields: fill(map[string]any{"scheme": "accent2"}), expect: presence})
		add(writeTmpl{pat: fp + "/a:solidFill/a:sysClr", fields: fill(map[string]any{"sys": "windowText"}), expect: presence})
		add(writeTmpl{pat: fp + "/a:solidFill/a:prstClr", fields: fill(map[string]any{"preset": "red"}), expect: presence})
		add(writeTmpl{pat: fp + "/a:solidFill/a:hslClr", fields: fill(map[string]any{"hsl": colorBase["hsl"]}), expect: presence})
		add(writeTmpl{pat: fp + "/a:solidFill/a:scrgbClr", fields: fill(map[string]any{"scrgb": colorBase["scrgb"]}), expect: presence})
	}
	add(writeTmpl{pat: "(a:CT_SystemColor)/@lastClr", fields: fill(map[string]any{"sys": "windowText", "hex": "#123456"}), expect: fixed("123456")})
	for typ, key := range colorKinds {
		if names, ok := colorComps[key]; ok {
			for i, a := range names {
				comp := []int{0, 0, 0}
				comp[i] = 60000
				add(writeTmpl{pat: typ + "/@" + a, fields: fill(map[string]any{key: comp}), expect: fixed("60000")})
			}
		}
		vfill := func(v string) map[string]any {
			if key == "rgb" {
				return map[string]any{"fill": map[string]any{"hex": "#" + v}}
			}
			return map[string]any{"fill": map[string]any{key: v}}
		}
		add(writeTmpl{pat: typ + "/@val", fields: vfill, expect: same})
		mod := func(name string, val bool) func(string) map[string]any {
			return func(string) map[string]any {
				var m any = name
				if val {
					m = map[string]any{name: 50000}
				}
				c := map[string]any{key: colorBase[key], "mods": []any{m}}
				return map[string]any{"fill": c}
			}
		}
		for _, k := range uniKids[typ] {
			if k.Node != "element" {
				continue
			}
			name := strings.TrimPrefix(lastStep(k.Path), "a:")
			hasVal := uniBy[k.Path+"/@val"] != nil
			add(writeTmpl{pat: k.Path, fields: mod(name, hasVal), expect: presence})
			if hasVal {
				add(writeTmpl{pat: k.Path + "/@val", fields: mod(name, true), expect: fixed("50000")})
			}
		}
	}

	ln := spPrOwners + "/p:spPr/a:ln"
	add(writeTmpl{pat: ln, absent: []string{"%s"}, fields: line("w", 3.0), expect: presence})
	add(writeTmpl{pat: ln + "/@w", fields: line("w", 3.0), expect: fixed("38100")})
	add(writeTmpl{pat: ln + "/@cap", fields: line("cap", func(v string) any { return v }), expect: same})
	add(writeTmpl{pat: ln + "/a:noFill", fields: func(string) map[string]any { return map[string]any{"line": "none"} }, expect: presence})
	add(writeTmpl{pat: ln + "/a:solidFill", fields: line(), expect: presence})
	lc := func(c map[string]any) func(string) map[string]any {
		return func(string) map[string]any { return map[string]any{"line": map[string]any{"color": c}} }
	}
	add(writeTmpl{pat: ln + "/a:solidFill/a:srgbClr", fields: lc(map[string]any{"hex": "#FF0000"}), expect: presence})
	add(writeTmpl{pat: ln + "/a:solidFill/a:schemeClr", fields: lc(map[string]any{"scheme": "accent2"}), expect: presence})
	add(writeTmpl{pat: ln + "/a:solidFill/a:sysClr", fields: lc(map[string]any{"sys": "windowText"}), expect: presence})
	add(writeTmpl{pat: ln + "/a:solidFill/a:prstClr", fields: lc(map[string]any{"preset": "red"}), expect: presence})
	add(writeTmpl{pat: ln + "/a:solidFill/a:hslClr", fields: lc(map[string]any{"hsl": colorBase["hsl"]}), expect: presence})
	add(writeTmpl{pat: ln + "/a:solidFill/a:scrgbClr", fields: lc(map[string]any{"scrgb": colorBase["scrgb"]}), expect: presence})
	add(writeTmpl{pat: ln + "/a:prstDash", fields: line("dash", "dash"), expect: presence})
	add(writeTmpl{pat: ln + "/a:prstDash/@val", fields: line("dash", func(v string) any { return v }), expect: same})
	for _, end := range []struct{ el, key string }{{"a:headEnd", "head"}, {"a:tailEnd", "tail"}} {
		add(writeTmpl{pat: ln + "/" + end.el, fields: line(end.key, "triangle"), expect: presence})
		add(writeTmpl{pat: ln + "/" + end.el + "/@type", fields: line(end.key, func(v string) any { return v }), expect: same})
		add(writeTmpl{pat: ln + "/" + end.el + "/@w", fields: line(end.key, func(v string) any { return "triangle w=" + v }), expect: same})
		add(writeTmpl{pat: ln + "/" + end.el + "/@len", fields: line(end.key, func(v string) any { return "triangle len=" + v }), expect: same})
	}

	cx := "p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr/"
	for _, g := range []struct{ el, key string }{{"a:stCxn", "from_glue"}, {"a:endCxn", "to_glue"}} {
		glue := func(string) map[string]any { return map[string]any{g.key: "s6:2"} }
		add(writeTmpl{pat: cx + g.el, absent: []string{"%s"}, fields: glue, expect: presence})
		add(writeTmpl{pat: cx + g.el + "/@id", fields: glue, expect: fixed("6")})
		add(writeTmpl{pat: cx + g.el + "/@idx", fields: glue, expect: fixed("2")})
	}

	bp := "p:sp/p:txBody/a:bodyPr"
	body := func(k string, f func(v string) any) func(string) map[string]any {
		return func(v string) map[string]any { return map[string]any{"text": map[string]any{k: f(v)}} }
	}
	other := func(a, b string) func(string) any {
		return func(v string) any {
			if v == a {
				return b
			}
			return a
		}
	}
	for i, a := range []string{"lIns", "tIns", "rIns", "bIns"} {
		ins := []float64{1, 1, 1, 1}
		ins[i] = 10
		add(writeTmpl{pat: bp + "/@" + a, fields: body("insets", func(string) any { return ins }), expect: fixed("127000")})
	}
	add(writeTmpl{pat: bp + "/@anchor", fields: body("anchor", func(v string) any { return v }), prep: body("anchor", other("b", "t")), expect: same})
	add(writeTmpl{pat: bp + "/@wrap", fields: body("wrap", func(v string) any { return v }), prep: body("wrap", other("none", "square")), expect: same})

	// Pictures: the image (a PNG file with the probe image's bytes, so
	// the existing part and relationship are reused), crop, fit, fill
	// rectangle and locks.
	img := func(string) map[string]any { return map[string]any{"image": probePNG()} }
	add(writeTmpl{pat: "p:pic/p:blipFill", absent: []string{"%s"}, fields: img, expect: presence})
	add(writeTmpl{pat: "p:pic/p:blipFill/a:blip", absent: []string{"%s"}, fields: img, expect: presence})
	rect := func(key string, i int, v float64) func(string) map[string]any {
		return func(string) map[string]any {
			r := []float64{0, 0, 0, 0}
			r[i] = v
			return map[string]any{key: r}
		}
	}
	add(writeTmpl{pat: "p:pic/p:blipFill/a:srcRect", absent: []string{"%s"}, fields: rect("crop", 0, 10), expect: presence})
	add(writeTmpl{pat: "p:pic/p:blipFill/a:stretch", absent: []string{"%s"}, fields: func(string) map[string]any { return map[string]any{"fit": "stretch"} }, expect: presence})
	add(writeTmpl{pat: "p:pic/p:blipFill/a:stretch/a:fillRect", absent: []string{"%s"}, fields: rect("fill_rect", 0, 0), expect: presence})
	for i, side := range []string{"l", "t", "r", "b"} {
		add(writeTmpl{pat: "p:pic/p:blipFill/a:srcRect/@" + side, fields: rect("crop", i, 25), expect: fixed("25000")})
		add(writeTmpl{pat: "p:pic/p:blipFill/a:stretch/a:fillRect/@" + side, fields: rect("fill_rect", i, 12.5), expect: fixed("12500")})
	}
	pl := "p:pic/p:nvPicPr/p:cNvPicPr/a:picLocks"
	add(writeTmpl{pat: pl, absent: []string{"%s"}, fields: func(string) map[string]any { return map[string]any{"locks": map[string]any{"no_change_aspect": true}} }, expect: presence})
	for _, k := range uniKids[pl] {
		if k.Node != "attribute" {
			continue
		}
		name := strings.TrimPrefix(lastStep(k.Path), "@")
		add(writeTmpl{pat: k.Path, fields: func(string) map[string]any { return map[string]any{"locks": map[string]any{cli.Snake(name): true}} }, expect: fixed("1")})
	}

	tx := "p:sp/p:txBody"
	add(writeTmpl{pat: tx, ctx: 3, fields: html(fixed("X")), expect: presence})
	add(writeTmpl{pat: tx + "/a:bodyPr", ctx: 3, fields: html(fixed("X")), expect: presence})
	add(writeTmpl{pat: tx + "/a:p", ctx: 3, fields: html(fixed("X")), expect: presence})
	add(writeTmpl{pat: tx + "/a:p/a:pPr", absent: []string{"%s"}, fields: html(fixed(`<p style="text-align:center">X</p>`)), expect: presence})
	add(writeTmpl{pat: tx + "/a:p/a:pPr/@algn", fields: html(func(v string) string {
		if c, ok := alignCSS[v]; ok {
			return `<p style="text-align:` + c + `">X</p>`
		}
		return `<p data-algn="` + v + `">X</p>`
	}), expect: same})
	add(writeTmpl{pat: tx + "/a:p/a:pPr/@lvl", fields: html(fixed(`<p data-level="3">X</p>`)), expect: fixed("2")})
	add(writeTmpl{pat: tx + "/a:p/a:pPr/a:buNone", fields: html(fixed(`<p data-bullet="none">X</p>`)), expect: presence})
	add(writeTmpl{pat: tx + "/a:p/a:pPr/a:buChar", fields: html(fixed(`<ul><li data-char="-">X</li></ul>`)), expect: presence})
	add(writeTmpl{pat: tx + "/a:p/a:pPr/a:buChar/@char", fields: html(fixed(`<ul><li data-char="-">X</li></ul>`)), expect: fixed("-")})
	add(writeTmpl{pat: tx + "/a:p/a:pPr/a:buAutoNum", fields: html(fixed(`<ol><li data-type="arabicPeriod">X</li></ol>`)), expect: presence})
	add(writeTmpl{pat: tx + "/a:p/a:pPr/a:buAutoNum/@type", fields: html(func(v string) string {
		return `<ol><li data-type="` + v + `">X</li></ol>`
	}), expect: same})
	add(writeTmpl{pat: tx + "/a:p/a:r", ctx: 3, fields: html(fixed("X")), expect: presence})
	add(writeTmpl{pat: tx + "/a:p/a:r/a:t", fields: html(fixed("Hello")), expect: fixed("Hello")})
	add(writeTmpl{pat: tx + "/a:p/a:br", fields: html(fixed("A<br/>B")), expect: presence})
	rp := tx + "/a:p/a:r/a:rPr"
	add(writeTmpl{pat: rp, ctx: 3, fields: html(fixed("<b>X</b>")), expect: presence})
	add(writeTmpl{pat: rp + "/@b", fields: html(fixed("<b>X</b>")), expect: fixed("1")})
	add(writeTmpl{pat: rp + "/@i", fields: html(fixed("<i>X</i>")), expect: fixed("1")})
	add(writeTmpl{pat: rp + "/@u", fields: html(func(v string) string {
		if h, ok := underline[v]; ok {
			return h
		}
		return `<u data-u="` + v + `">X</u>`
	}), expect: same})
	add(writeTmpl{pat: rp + "/@sz", fields: html(fixed(`<span style="font-size:20pt">X</span>`)), expect: fixed("2000")})
	add(writeTmpl{pat: rp + "/a:noFill", fields: html(fixed(`<span style="color:transparent">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:solidFill", fields: html(fixed(`<span style="color:#FF0000">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:solidFill/a:srgbClr", fields: html(fixed(`<span style="color:#FF0000">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:solidFill/a:schemeClr", fields: html(fixed(`<span style="color:var(--accent2)">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:solidFill/a:sysClr", fields: html(fixed(`<span data-color="sys:windowText">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:solidFill/a:prstClr", fields: html(fixed(`<span data-color="prst:red">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:solidFill/a:hslClr", fields: html(fixed(`<span data-color="hsl:0,100000,50000">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:solidFill/a:scrgbClr", fields: html(fixed(`<span data-color="scrgb:100000,0,0">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:latin", fields: html(fixed(`<span style="font-family:'Courier New'">X</span>`)), expect: presence})
	add(writeTmpl{pat: rp + "/a:latin/@typeface", fields: html(fixed(`<span style="font-family:'Courier New'">X</span>`)), expect: fixed("Courier New")})
	textFormatTmpls(add)
	bodyTmpls(add)
	nonVisualTmpls(add)
	return ts
}

// textFormatTmpls are the templates of the paragraph and run formatting
// the HTML grammar writes beyond alignment, level, bullet choice, bold,
// italic, underline, size, colour and font.
func textFormatTmpls(add func(writeTmpl)) {
	pp := "p:sp/p:txBody/a:p/a:pPr"
	para := func(attrs string) func(string) map[string]any {
		return html(fixed(`<p ` + attrs + `>X</p>`))
	}
	style := func(s string) func(string) map[string]any { return para(`style="` + s + `"`) }
	add(writeTmpl{pat: pp + "/@marL", fields: style("margin-left:18pt"), expect: fixed("228600")})
	add(writeTmpl{pat: pp + "/@marR", fields: style("margin-right:9pt"), expect: fixed("114300")})
	add(writeTmpl{pat: pp + "/@indent", fields: style("text-indent:-18pt"), expect: fixed("-228600")})
	add(writeTmpl{pat: pp + "/@defTabSz", fields: style("tab-size:36pt"), expect: fixed("457200")})
	add(writeTmpl{pat: pp + "/@rtl", fields: para(`dir="rtl"`), expect: fixed("1")})
	for _, sp := range []struct{ el, prop string }{{"a:lnSpc", "line-height"}, {"a:spcBef", "margin-top"}, {"a:spcAft", "margin-bottom"}} {
		add(writeTmpl{pat: pp + "/" + sp.el, fields: style(sp.prop + ":150%"), expect: presence})
		add(writeTmpl{pat: pp + "/" + sp.el + "/a:spcPct", fields: style(sp.prop + ":150%"), expect: presence})
		add(writeTmpl{pat: pp + "/" + sp.el + "/a:spcPct/@val", fields: style(sp.prop + ":150%"), expect: fixed("150000")})
		add(writeTmpl{pat: pp + "/" + sp.el + "/a:spcPts", fields: style(sp.prop + ":12pt"), expect: presence})
		add(writeTmpl{pat: pp + "/" + sp.el + "/a:spcPts/@val", fields: style(sp.prop + ":12.5pt"), expect: fixed("1250")})
	}
	add(writeTmpl{pat: pp + "/a:buClrTx", fields: para(`data-bu-color="text"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:buClr", fields: para(`data-bu-color="#FF0000"`), expect: presence})
	for _, c := range []struct{ el, spec string }{
		{"a:srgbClr", "#FF0000"}, {"a:schemeClr", "accent2"}, {"a:sysClr", "sys:windowText"}, {"a:prstClr", "prst:red"},
		{"a:hslClr", "hsl:0,100000,50000"}, {"a:scrgbClr", "scrgb:100000,0,0"},
	} {
		add(writeTmpl{pat: pp + "/a:buClr/" + c.el, fields: para(`data-bu-color="` + c.spec + `"`), expect: presence})
	}
	add(writeTmpl{pat: pp + "/a:buSzTx", fields: para(`data-bu-size="text"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:buSzPct", fields: para(`data-bu-size="75%"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:buSzPct/@val", fields: para(`data-bu-size="75%"`), expect: fixed("75000")})
	add(writeTmpl{pat: pp + "/a:buSzPts", fields: para(`data-bu-size="12pt"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:buSzPts/@val", fields: para(`data-bu-size="12pt"`), expect: fixed("1200")})
	add(writeTmpl{pat: pp + "/a:buFontTx", fields: para(`data-bu-font="text"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:buFont", fields: para(`data-bu-font="'Wingdings'"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:buFont/@typeface", fields: para(`data-bu-font="'Wingdings'"`), expect: fixed("Wingdings")})
	add(writeTmpl{pat: pp + "/a:buAutoNum/@startAt", fields: html(fixed(`<ol><li data-type="arabicPeriod" data-start="3">X</li></ol>`)), expect: fixed("3")})
	add(writeTmpl{pat: pp + "/a:tabLst", fields: para(`data-tabs="36pt"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:tabLst/a:tab", fields: para(`data-tabs="36pt"`), expect: presence})
	add(writeTmpl{pat: pp + "/a:tabLst/a:tab/@pos", fields: para(`data-tabs="36pt"`), expect: fixed("457200")})
	add(writeTmpl{pat: pp + "/a:tabLst/a:tab/@algn", fields: func(v string) map[string]any {
		return map[string]any{"html": `<p data-tabs="36pt:` + v + `">X</p>`}
	}, expect: same})

	rp := "p:sp/p:txBody/a:p/a:r/a:rPr"
	span := func(attrs string) func(string) map[string]any { return html(fixed(`<span ` + attrs + `>X</span>`)) }
	add(writeTmpl{pat: rp + "/@strike", fields: html(func(v string) string {
		switch v {
		case "sngStrike":
			return "<s>X</s>"
		case "noStrike":
			return `<span data-strike="noStrike">X</span>`
		}
		return `<s data-strike="` + v + `">X</s>`
	}), expect: same})
	add(writeTmpl{pat: rp + "/@baseline", fields: span(`style="vertical-align:30%"`), expect: fixed("30000")})
	add(writeTmpl{pat: rp + "/@cap", fields: html(func(v string) string {
		switch v {
		case "all":
			return `<span style="text-transform:uppercase">X</span>`
		case "small":
			return `<span style="font-variant:small-caps">X</span>`
		case "none":
			return `<span style="text-transform:none">X</span>`
		}
		return `<span data-cap="` + v + `">X</span>`
	}), expect: same})
	add(writeTmpl{pat: rp + "/@spc", fields: span(`style="letter-spacing:1.5pt"`), expect: fixed("150")})
	add(writeTmpl{pat: rp + "/@lang", fields: html(fixed(`<p lang="de-DE">X</p>`)), expect: fixed("de-DE")})
	add(writeTmpl{pat: rp + "/a:highlight", fields: span(`style="background-color:#FFFF00"`), expect: presence})
	for _, c := range []struct{ el, h string }{
		{"a:srgbClr", `style="background-color:#FFFF00"`}, {"a:schemeClr", `style="background-color:var(--accent2)"`},
		{"a:sysClr", `data-highlight="sys:windowText"`}, {"a:prstClr", `data-highlight="prst:yellow"`},
		{"a:hslClr", `data-highlight="hsl:0,100000,50000"`}, {"a:scrgbClr", `data-highlight="scrgb:100000,0,0"`},
	} {
		add(writeTmpl{pat: rp + "/a:highlight/" + c.el, fields: span(c.h), expect: presence})
	}
	link := html(fixed(`<a href="https://example.com/" title="Example">X</a>`))
	add(writeTmpl{pat: rp + "/a:hlinkClick", fields: link, expect: presence})
	add(writeTmpl{pat: rp + "/a:hlinkClick/@r:id", fields: link, expect: presence})
	add(writeTmpl{pat: rp + "/a:hlinkClick/@tooltip", fields: link, expect: fixed("Example")})
}

// bodyTmpls are the templates of the text body properties beyond the
// insets, anchor and wrap.
func bodyTmpls(add func(writeTmpl)) {
	bp := "p:sp/p:txBody/a:bodyPr"
	body := func(kv map[string]any) func(string) map[string]any {
		return func(string) map[string]any { return map[string]any{"text": kv} }
	}
	enum := func(attr, other string) (func(string) map[string]any, func(string) map[string]any) {
		key := cli.Snake(attr) // the plan's key for the attribute
		return func(v string) map[string]any { return map[string]any{"text": map[string]any{key: v}} },
			func(v string) map[string]any {
				o := other
				if v == other {
					o = "horz"
					if attr != "vert" {
						o = "overflow"
					}
				}
				return map[string]any{"text": map[string]any{key: o}}
			}
	}
	for _, e := range []struct{ key, other string }{{"vert", "vert270"}, {"vertOverflow", "clip"}, {"horzOverflow", "clip"}} {
		f, prep := enum(e.key, e.other)
		add(writeTmpl{pat: bp + "/@" + e.key, fields: f, prep: prep, expect: same})
	}
	add(writeTmpl{pat: bp + "/@rot", fields: body(map[string]any{"rot": 90}), expect: fixed("5400000")})
	add(writeTmpl{pat: bp + "/@numCol", fields: body(map[string]any{"num_col": 3}), expect: fixed("3")})
	add(writeTmpl{pat: bp + "/@spcCol", fields: body(map[string]any{"spc_col": 10}), expect: fixed("127000")})
	for _, k := range []string{"rtlCol", "anchorCtr", "upright", "compatLnSpc", "spcFirstLastPara", "fromWordArt", "forceAA"} {
		add(writeTmpl{pat: bp + "/@" + k, fields: body(map[string]any{cli.Snake(k): true}), expect: fixed("1")})
	}
	other := map[string]string{"none": "shape", "shape": "shrink", "shrink": "none"}
	for _, a := range []struct{ el, mode string }{{"a:noAutofit", "none"}, {"a:spAutoFit", "shape"}, {"a:normAutofit", "shrink"}} {
		add(writeTmpl{pat: bp + "/" + a.el, fields: body(map[string]any{"autofit": a.mode}),
			prep: body(map[string]any{"autofit": other[a.mode]}), expect: presence})
	}
	add(writeTmpl{pat: bp + "/a:normAutofit/@fontScale", fields: body(map[string]any{"autofit": "shrink", "font_scale": 62.5}), expect: fixed("62500")})
	add(writeTmpl{pat: bp + "/a:normAutofit/@lnSpcReduction", fields: body(map[string]any{"autofit": "shrink", "ln_spc_reduction": 20}), expect: fixed("20000")})
}

// nonVisualTmpls are the templates of descr, title, hidden and txBox,
// and of a connector's fill none.
func nonVisualTmpls(add func(writeTmpl)) {
	nv := "{p:sp/p:nvSpPr,p:cxnSp/p:nvCxnSpPr,p:grpSp/p:nvGrpSpPr,p:pic/p:nvPicPr}/p:cNvPr"
	add(writeTmpl{pat: nv + "/@descr", fields: func(string) map[string]any { return map[string]any{"descr": "Alt text"} }, expect: fixed("Alt text")})
	add(writeTmpl{pat: nv + "/@title", fields: func(string) map[string]any { return map[string]any{"title": "Title"} }, expect: fixed("Title")})
	add(writeTmpl{pat: nv + "/@hidden", fields: func(string) map[string]any { return map[string]any{"hidden": true} }, expect: fixed("1")})
	add(writeTmpl{pat: "p:sp/p:nvSpPr/p:cNvSpPr/@txBox", fields: func(string) map[string]any { return map[string]any{"tx_box": true} }, expect: fixed("1")})
	add(writeTmpl{pat: "p:cxnSp/p:spPr/a:noFill", fields: func(string) map[string]any { return map[string]any{"fill": "none"} }, expect: presence})
}

// tmplFor returns the first template matching path.
func tmplFor(ts []writeTmpl, path string) *writeTmpl {
	for i := range ts {
		c := Claim{Paths: []string{ts[i].pat}}
		if c.Matches(path) {
			return &ts[i]
		}
	}
	return nil
}

// writeOutcome is what the write probe saw for one path.
type writeOutcome struct {
	passed   []string // values that read back as set
	failed   map[string]string
	identity bool // the value written back as shown changed nothing
	idErr    string
}

// probeWrite runs a template's plan for each value and checks the path,
// then writes the first passing value back as shown.
func probeWrite(t testing.TB, dir, path string, tp *writeTmpl) writeOutcome {
	it := uniBy[path]
	out := writeOutcome{failed: map[string]string{}}
	vals := tp.values
	if vals == nil {
		if it.Enum != "" {
			vals = uni.Enums[it.Enum]
		} else {
			vals = []string{sample(it, true)}
		}
	}
	root, _, err := resolve(path)
	if err != nil {
		out.failed[""] = err.Error()
		return out
	}
	loc := rootParts[root][tp.ctx]
	var firstDst string
	for _, v := range vals {
		f := newFixture(t)
		for _, a := range tp.absent {
			ap := strings.ReplaceAll(strings.ReplaceAll(a, "%s", path), "%p", parentPath(path))
			if err := f.setState(ap, uniBy[ap], "", false, tp.ctx, false); err != nil {
				out.failed[v] = err.Error()
				continue
			}
		}
		src := f.write(t, dir)
		dst := strings.TrimSuffix(src, ".pptx") + "-out.pptx"
		opName := tp.op
		if opName == "" {
			opName = "object.set"
		}
		op := map[string]any{"op": opName, "at": loc.id}
		maps.Copy(op, tp.fields(v))
		var err error
		if tp.prep != nil {
			pre := map[string]any{"op": "object.set", "at": loc.id}
			maps.Copy(pre, tp.prep(v))
			err = applyPlan(src, dst, pre, op)
		} else {
			err = applyPlan(src, dst, op)
		}
		if err != nil {
			out.failed[v] = err.Error()
			continue
		}
		got, err := readFixture(dst)
		if err != nil {
			out.failed[v] = err.Error()
			continue
		}
		if msg := check(got, path, it, tp.ctx, tp.expect(v)); msg != "" {
			out.failed[v] = msg
			continue
		}
		out.passed = append(out.passed, v)
		if firstDst == "" {
			firstDst = dst
		}
	}
	if firstDst == "" {
		return out
	}
	// Identity: the fields the template wrote, written back as the views
	// show them, change nothing.
	op, err := shownOp(t, firstDst, loc.id, tp)
	if err != nil {
		out.idErr = err.Error()
		return out
	}
	changed, err := changedParts(firstDst, op)
	switch {
	case err != nil:
		out.idErr = err.Error()
	case len(changed) > 0:
		out.idErr = fmt.Sprintf("writing back %v changed %v", op, changed)
	default:
		out.identity = true
	}
	return out
}

func compilePlan(opm map[string]any) ([]ops.Op, error) {
	p, err := ops.ParsePlan(testdeck.Plan("slide1", opm))
	if err != nil {
		return nil, err
	}
	return p.Compile()
}

func applyPlan(src, dst string, opms ...map[string]any) error {
	var compiled []ops.Op
	for _, opm := range opms {
		c, err := compilePlan(opm)
		if err != nil {
			return err
		}
		compiled = append(compiled, c...)
	}
	return ops.ApplyTo(src, dst, compiled...)
}

// changedParts runs a plan on a deck without saving and returns the
// parts it changed.
func changedParts(path string, opm map[string]any) ([]string, error) {
	compiled, err := compilePlan(opm)
	if err != nil {
		return nil, err
	}
	tx, err := ops.Begin(path)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, op := range compiled {
		if err := tx.Run(op); err != nil {
			return nil, err
		}
	}
	return tx.ChangedParts()
}

// shownOp builds the template's operation with each field it writes
// taken from what the views show of the object.
func shownOp(t testing.TB, deck, id string, tp *writeTmpl) (map[string]any, error) {
	f, err := readFixture(deck)
	if err != nil {
		return nil, err
	}
	p, err := f.project(t, os.TempDir())
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	for _, o := range p["objects"].([]any) {
		if m := o.(map[string]any); m["id"] == id {
			obj = m
		}
	}
	if obj == nil {
		return nil, fmt.Errorf("no object %s", id)
	}
	opName := tp.op
	if opName == "" {
		opName = "object.set"
	}
	op := map[string]any{"op": opName, "at": id}
	for k := range tp.fields("") {
		v, ok := obj[k]
		if !ok {
			return nil, fmt.Errorf("the view shows no %s", k)
		}
		op[k] = v
	}
	return op, nil
}

// readFixture reads a deck's probe parts back into a fixture.
func readFixture(path string) (fixture, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	f := fixture{}
	for _, zf := range zr.File {
		// The slide's relationships too: a hyperlink adds one.
		for _, n := range []string{partSlide, partLayout, partMaster, partPres, partTheme, "ppt/slides/_rels/slide1.xml.rels"} {
			if zf.Name == n {
				rc, err := zf.Open()
				if err != nil {
					return nil, err
				}
				b, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return nil, err
				}
				f[n] = parseEl(string(b))
			}
		}
	}
	return f, nil
}

// check reports how the path in the deck differs from what the plan
// should have left ("" when it holds want, or is present when want is
// "").
func check(f fixture, path string, it *Item, ctx int, want string) string {
	holder, last, _, err := f.place(path, ctx)
	if err != nil {
		return err.Error()
	}
	switch it.Node {
	case "attribute":
		v, ok := holder.attr(strings.TrimPrefix(last.name, "@"))
		switch {
		case !ok:
			return "absent after the plan"
		case want != "" && v != want:
			return fmt.Sprintf("holds %q, want %q", v, want)
		}
	default:
		c := holder.child(last.name)
		switch {
		case c == nil:
			return "absent after the plan"
		case want != "" && c.text != want:
			return fmt.Sprintf("holds %q, want %q", c.text, want)
		}
	}
	return ""
}

var (
	writeOnce sync.Once
	writeRes  map[string]writeOutcome
)

// writeScan probes every path a write claim (other than a non-goal or a
// carry claim) covers.
func writeScan(t *testing.T) map[string]writeOutcome {
	u := loadUniverse(t)
	writeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "coverage-write")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		ts := writeTmpls()
		var paths []string
		for _, it := range u.Items {
			if c := Decide(Write, it.Path); c != nil && (c.Status == RoundTripLaw || c.Status == WriteCodec) {
				paths = append(paths, it.Path)
			}
		}
		res := make([]writeOutcome, len(paths))
		var wg sync.WaitGroup
		next := make(chan int)
		for w := 0; w < runtime.NumCPU(); w++ {
			wg.Go(func() {
				for i := range next {
					tp := tmplFor(ts, paths[i])
					if tp == nil {
						res[i] = writeOutcome{failed: map[string]string{"": "no plan template"}}
						continue
					}
					res[i] = probeWrite(t, dir, paths[i], tp)
				}
			})
		}
		for i := range paths {
			next <- i
		}
		close(next)
		wg.Wait()
		writeRes = map[string]writeOutcome{}
		for i, p := range paths {
			writeRes[p] = res[i]
		}
	})
	return writeRes
}

// TestWriteClaimsMatchBehaviour checks every write claim against a plan
// that sets the path: the values the claim covers must read back as set
// (and no other value of an enumeration may), and a round-trip-law claim
// needs the value written back as shown to change nothing.
func TestWriteClaimsMatchBehaviour(t *testing.T) {
	res := writeScan(t)
	var bad []string
	var paths []string
	for p := range res {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		o := res[p]
		c := Decide(Write, p)
		it := uniBy[p]
		if len(o.passed) == 0 {
			var why []string
			for v, e := range o.failed {
				why = append(why, v+": "+e)
			}
			sort.Strings(why)
			bad = append(bad, fmt.Sprintf("%s (%s): no value written: %s", p, c.By, strings.Join(why, "; ")))
			continue
		}
		if it.Enum != "" {
			for _, v := range uni.Enums[it.Enum] {
				_, failed := o.failed[v]
				switch {
				case c.Covers(v) && failed:
					bad = append(bad, fmt.Sprintf("%s=%s (%s): claimed, not written: %s", p, v, c.By, o.failed[v]))
				case !c.Covers(v) && !failed:
					bad = append(bad, fmt.Sprintf("%s=%s (%s): written, not claimed", p, v, c.By))
				}
			}
		}
		switch {
		case c.Status == RoundTripLaw && !o.identity:
			bad = append(bad, fmt.Sprintf("%s (%s): claimed round-trip-law, identity fails: %s", p, c.By, o.idErr))
		case c.Status == WriteCodec && o.identity:
			bad = append(bad, fmt.Sprintf("%s (%s): claimed write-codec, identity holds: claim round-trip-law", p, c.By))
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d write claims disagree with the plans:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}
