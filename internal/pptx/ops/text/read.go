package text

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Ctx resolves what a text body refers to outside itself: displayed
// colours and hyperlink relationships. The read model implements it over
// the slide's theme and relationships; a nil Ctx shows only plain sRGB
// colours' hex and no hyperlink targets.
type Ctx interface {
	// Color returns the displayed hex (RRGGBB, "" when unknown) of the
	// colour element el, read for property prop, or false when the
	// colour is not shown at all.
	Color(prop string, el *tree.Node) (hex string, ok bool)
	// Link returns the target of relationship rid of the body's part and
	// whether it is external, or false when there is no such
	// relationship.
	Link(rid string) (target string, external bool, ok bool)
}

// ReadBody projects a p:txBody (or any element holding a:p children) as
// the read model does, without displayed colours or hyperlink targets.
func ReadBody(body *tree.Node) Body { return ReadBodyWith(body, nil) }

// ReadBodyWith projects a p:txBody with ctx resolving colours and links.
// The read model shows text bodies through it, so the views and the text
// operations read text the same way.
func ReadBodyWith(body *tree.Node, ctx Ctx) Body {
	var b Body
	for _, p := range body.Elements() {
		if derive.Is(p, derive.DML, "p") {
			b.Paras = append(b.Paras, readPara(p, ctx))
		}
	}
	return b
}

// readPara projects one a:p element as a paragraph.
func readPara(p *tree.Node, ctx Ctx) Para {
	out := Para{Lvl: 1}
	if ppr := derive.Child(p, derive.DML, "pPr"); ppr != nil {
		out.Algn, out.Lvl, out.Bullet = pprShown(ppr)
		out.PF = pfmtShown(ppr, ctx)
	}
	for _, it := range items(p, ctx) {
		out.Runs = append(out.Runs, it.run)
	}
	out.Runs = canonical(out.Runs)
	return out
}

// pprShown reads the paragraph properties the HTML shows as alignment,
// level and bullet choice.
func pprShown(ppr *tree.Node) (algn string, lvl int, bullet string) {
	lvl = 1
	if v, ok := ppr.Attr("lvl"); ok {
		if n, err := strconv.Atoi(v); err == nil && n+1 > 1 {
			lvl = n + 1
		}
	}
	algn, _ = ppr.Attr("algn")
	bullet = bulletOf(ppr)
	return
}

// emuPt shows an EMU length attribute in points, "" when absent or not
// an integer.
func emuPt(n *tree.Node, attr string) string {
	v, ok := n.Attr(attr)
	if !ok {
		return ""
	}
	x, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return ""
	}
	return derive.FormatNum(float64(x)/derive.EMUPerPoint) + "pt"
}

// spacingShown reads a CT_TextSpacing element (a:lnSpc, a:spcBef,
// a:spcAft): "150%" for a:spcPct (val in 1000ths of a percent, or a
// percentage string), "12pt" for a:spcPts (val in 100ths of a point).
func spacingShown(el *tree.Node) string {
	if el == nil {
		return ""
	}
	if c := derive.Child(el, derive.DML, "spcPct"); c != nil {
		return pctShown(c, "val")
	}
	if c := derive.Child(el, derive.DML, "spcPts"); c != nil {
		v, _ := c.Attr("val")
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return decimal(n, 2) + "pt"
		}
	}
	return ""
}

// pctShown reads a percentage attribute in 1000ths of a percent (or a
// percentage string) as "N%", "" when absent or unreadable.
func pctShown(n *tree.Node, attr string) string {
	v, ok := n.Attr(attr)
	if !ok {
		return ""
	}
	if s, ok := strings.CutSuffix(v, "%"); ok {
		if x, ok := undecimal(s, 3); ok {
			return decimal(x, 3) + "%"
		}
		return ""
	}
	if x, err := strconv.ParseInt(v, 10, 64); err == nil {
		return decimal(x, 3) + "%"
	}
	return ""
}

// pfmtShown reads the rest of the paragraph formatting the HTML shows.
func pfmtShown(ppr *tree.Node, ctx Ctx) PFmt {
	var f PFmt
	f.LnSpc = spacingShown(derive.Child(ppr, derive.DML, "lnSpc"))
	f.SpcBef = spacingShown(derive.Child(ppr, derive.DML, "spcBef"))
	f.SpcAft = spacingShown(derive.Child(ppr, derive.DML, "spcAft"))
	f.MarL, f.MarR, f.Indent = emuPt(ppr, "marL"), emuPt(ppr, "marR"), emuPt(ppr, "indent")
	f.DefTab = emuPt(ppr, "defTabSz")
	if v, ok := ppr.Attr("rtl"); ok {
		switch v {
		case "1", "true":
			f.Dir = "rtl"
		case "0", "false":
			f.Dir = "ltr"
		}
	}
	if tl := derive.Child(ppr, derive.DML, "tabLst"); tl != nil {
		var stops []string
		for _, t := range tl.Elements() {
			if !derive.Is(t, derive.DML, "tab") {
				continue
			}
			s := emuPt(t, "pos")
			if s == "" {
				stops = nil // a stop HTML cannot show: show none
				break
			}
			if a, ok := t.Attr("algn"); ok {
				s += ":" + a
			}
			stops = append(stops, s)
		}
		f.Tabs = strings.Join(stops, " ")
	}
	for _, c := range ppr.Elements() {
		if !derive.In(c, derive.DML) {
			continue
		}
		switch c.Local() {
		case "buAutoNum":
			f.StartAt, _ = c.Attr("startAt")
		case "buFontTx":
			f.BuFont = "text"
		case "buFont":
			if tf, ok := c.Attr("typeface"); ok {
				f.BuFont = fontCSS(tf)
			}
		case "buSzTx":
			f.BuSize = "text"
		case "buSzPct":
			f.BuSize = pctShown(c, "val")
		case "buSzPts":
			v, _ := c.Attr("val")
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				f.BuSize = decimal(n, 2) + "pt"
			}
		case "buClrTx":
			f.BuClr = Fill{Kind: "text"}
		case "buClr":
			if col, ok := colorShown("text.buColor", derive.ColorElem(c), ctx); ok {
				f.BuClr = Fill{Kind: "solid", Color: col}
			}
		}
	}
	return f
}

// colorShown reads a colour element as written, with the hex ctx
// displays (only a plain sRGB colour's own value without ctx); false
// when it is not shown.
func colorShown(prop string, el *tree.Node, ctx Ctx) (Color, bool) {
	hex := ""
	if ctx != nil {
		var ok bool
		if hex, ok = ctx.Color(prop, el); !ok {
			return Color{}, false
		}
	}
	c, ok := derive.ParseColor(el)
	if !ok {
		return Color{}, false
	}
	out := Color{Color: c}
	if ctx != nil {
		out.Hex = hex
		return out, true
	}
	if c.Kind == "srgb" && len(c.Mods) == 0 && reHex.MatchString("#"+c.Val) {
		out.Hex = c.Val // shown without a theme
	}
	return out, true
}

// bulletOf reads a paragraph's bullet choice as the read model does.
func bulletOf(ppr *tree.Node) string {
	for _, c := range ppr.Elements() {
		switch c.Local() {
		case "buNone":
			return "none"
		case "buChar":
			ch, _ := c.Attr("char")
			return "char:" + ch
		case "buAutoNum":
			t, _ := c.Attr("type")
			return "autonum:" + t
		case "buBlip":
			return "picture"
		}
	}
	return ""
}

// item is one child of an a:p that the HTML shows: a run (a:r, a:fld) or
// a line break (a:br), with the element and, for runs inside
// mc:AlternateContent, the enclosing mc:AlternateContent child of the
// paragraph.
type item struct {
	run  Run
	el   *tree.Node
	mc   *tree.Node // non-nil when el sits in mc:AlternateContent
	text *tree.Node // a:t, nil if absent
}

// items lists what the read model shows of paragraph p, in order.
func items(p *tree.Node, ctx Ctx) []item {
	var out []item
	var walk func(n, mc *tree.Node)
	walk = func(n, mc *tree.Node) {
		for _, c := range n.Elements() {
			switch {
			case derive.Is(c, derive.MC, "AlternateContent"):
				br := derive.SelectBranch(c)
				if br.Node != nil {
					m := mc
					if m == nil {
						m = c
					}
					walk(br.Node, m)
				}
			case derive.Is(c, derive.DML, "r"), derive.Is(c, derive.DML, "fld"):
				t := derive.Child(c, derive.DML, "t")
				txt := ""
				if t != nil {
					txt = shownText(t.Text())
				}
				out = append(out, item{run: Run{Text: txt, Fmt: rprShown(derive.Child(c, derive.DML, "rPr"), ctx)}, el: c, mc: mc, text: t})
			case derive.Is(c, derive.DML, "br"):
				out = append(out, item{run: Run{Br: true}, el: c, mc: mc})
			}
		}
	}
	walk(p, nil)
	return out
}

// rprShown reads the run formatting the HTML shows from an a:rPr (nil
// for none).
func rprShown(rpr *tree.Node, ctx Ctx) Fmt {
	var f Fmt
	if rpr == nil {
		return f
	}
	tri := func(name string) string {
		v, ok := rpr.Attr(name)
		switch {
		case !ok:
			return ""
		case v == "1" || v == "true":
			return "on"
		}
		return "off"
	}
	f.B, f.I = tri("b"), tri("i")
	f.U, _ = rpr.Attr("u")
	f.Strike, _ = rpr.Attr("strike")
	if v, ok := rpr.Attr("sz"); ok {
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			f.Size = derive.FormatNum(x / 100)
		}
	}
	f.Fill = fillShown(derive.FillElem(rpr), ctx)
	if l := derive.Child(rpr, derive.DML, "latin"); l != nil {
		f.Font, f.HasFont = l.Attr("typeface")
	}
	f.Baseline = pctShown(rpr, "baseline")
	f.Cap, _ = rpr.Attr("cap")
	if v, ok := rpr.Attr("spc"); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.Spc = decimal(n, 2) + "pt"
		}
	}
	if h := derive.Child(rpr, derive.DML, "highlight"); h != nil {
		if c, ok := colorShown("text.highlight", derive.ColorElem(h), ctx); ok {
			f.Highlight = Fill{Kind: "solid", Color: c}
		}
	}
	f.Lang, _ = rpr.Attr("lang")
	if h := derive.Child(rpr, derive.DML, "hlinkClick"); h != nil {
		f.Link = linkShown(h, ctx)
	}
	return f
}

// linkShown reads an a:hlinkClick: an external relationship's target
// when it has no action, else the action ("internal" for a relationship
// to a part, "unresolved" when there is none to read).
func linkShown(h *tree.Node, ctx Ctx) Link {
	var l Link
	l.Title, _ = h.Attr("tooltip")
	action, _ := h.Attr("action")
	rid, _ := derive.AttrNS(h, derive.REL, "id")
	switch {
	case action != "":
		l.Other = action
	case rid == "":
		l.Other = "unresolved"
	case ctx == nil:
		l.Other = "unresolved"
	default:
		target, ext, ok := ctx.Link(rid)
		switch {
		case !ok:
			l.Other = "unresolved"
		case !ext:
			l.Other = "internal"
		case target == "":
			l.Other = "unresolved"
		default:
			l.Href = target
		}
	}
	return l
}

// fillShown projects a fill element as a run or bullet fill.
func fillShown(f *tree.Node, ctx Ctx) Fill {
	if f == nil {
		return Fill{}
	}
	switch f.Local() {
	case "solidFill":
		c, ok := colorShown("text.color", derive.ColorElem(f), ctx)
		if !ok {
			return Fill{} // shown as nothing
		}
		return Fill{Kind: "solid", Color: c}
	case "noFill":
		return Fill{Kind: "none"}
	}
	return Fill{Kind: f.Local()}
}
