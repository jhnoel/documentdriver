package model

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/text"
)

// text projects the object's text body and defaults.
func (r *reader) text(o *Object, e *tree.Node, chain []link, style *tree.Node, phType string) {
	body := derive.Child(e, derive.PML, "txBody")
	if body == nil {
		return
	}
	t := &Text{}
	o.Text = t
	var levels map[int]bool
	t.HTML, levels = r.html(o, body)
	for l := range levels {
		t.Levels = append(t.Levels, l)
	}
	sort.Ints(t.Levels)

	// Body properties through the placeholder chain: the read half the
	// body codec shares.
	dl := links(chain)
	bodyAttr := func(name string) (string, Origin, bool) { return derive.ReadBodyAttr(dl, name) }
	for i, n := range []string{"lIns", "tIns", "rIns", "bIns"} {
		if v, org, ok := bodyAttr(n); ok {
			if k, err := strconv.ParseInt(v, 10, 64); err == nil { // ST_Coordinate32Unqualified
				f := float64(k)
				t.Insets[i] = &f
				o.setOrigin("text.insets."+n, org)
			} else {
				o.omit("text.insets."+n, "a:bodyPr@"+n+"="+strconv.Quote(v)+" is not an integer")
			}
		}
	}
	if v, org, ok := bodyAttr("anchor"); ok {
		t.Anchor = v
		o.setOrigin("text.anchor", org)
	}
	if v, org, ok := bodyAttr("wrap"); ok {
		t.Wrap = v
		o.setOrigin("text.wrap", org)
	}
	// The other body properties (the same read half: derive.ReadBodyAttr
	// and derive.ReadAutofit) and the autofit choice.
	t.Attrs = map[string]string{"anchor": t.Anchor, "wrap": t.Wrap}
	for _, n := range derive.BodyAttrs[2:] {
		if v, org, ok := bodyAttr(n); ok {
			t.Attrs[n] = v
			if !strings.HasSuffix(n, "Ins") {
				o.setOrigin("text."+n, org)
			}
		}
	}
	t.Autofit = derive.ReadAutofit(dl)
	o.setOrigin("text.autofit", t.Autofit.Origin)

	// Resolved text defaults per level used.
	srcs := r.textSources(o, chain, style, phType)
	for _, lvl := range t.Levels {
		r.levelProps(o, lvl, srcs)
	}
}

// textSrc is one source of level defaults: a list style (a:lstStyle,
// p:titleStyle, p:defaultTextStyle...) or a style fontRef.
type textSrc struct {
	list    *tree.Node
	fontRef *tree.Node
	origin  Origin
}

// textSources collects level-default sources for the object.
func (r *reader) textSources(o *Object, chain []link, style *tree.Node, phType string) []textSrc {
	var srcs []textSrc
	lst := func(sp *tree.Node) *tree.Node {
		return derive.Child(derive.Child(sp, derive.PML, "txBody"), derive.DML, "lstStyle")
	}
	srcs = append(srcs, textSrc{list: lst(chain[0].sp), origin: FromSlide})
	if fr := derive.Child(style, derive.DML, "fontRef"); fr != nil {
		srcs = append(srcs, textSrc{fontRef: fr, origin: FromStyle})
	}
	var tx *tree.Node
	if r.mroot != nil {
		tx = derive.Child(r.mroot, derive.PML, "txStyles")
	}
	pres := derive.Child(r.d.Pres.Root(), derive.PML, "defaultTextStyle")
	if o.Ph != nil {
		for _, l := range chain[1:] {
			srcs = append(srcs, textSrc{list: lst(l.sp), origin: l.origin})
		}
		name := derive.MasterTextStyle(phType)
		srcs = append(srcs, textSrc{list: derive.Child(tx, derive.PML, name), origin: FromMaster})
		if name == "otherStyle" {
			srcs = append(srcs, textSrc{list: pres, origin: FromPres})
		}
	} else {
		srcs = append(srcs, textSrc{list: derive.Child(tx, derive.PML, "otherStyle"), origin: FromMaster})
		srcs = append(srcs, textSrc{list: pres, origin: FromPres})
	}
	return srcs
}

// levelProps resolves the default paragraph and run properties of level
// lvl (1-based) through srcs, recording them as Props.
func (r *reader) levelProps(o *Object, lvl int, srcs []textSrc) {
	pfx := "text.lvl" + strconv.Itoa(lvl) + "."
	type getter func(ppr *tree.Node) (string, bool)
	resolve := func(key string, fromPPr getter, fromFontRef func(fr *tree.Node) (string, bool)) {
		for _, s := range srcs {
			if s.fontRef != nil {
				if fromFontRef != nil {
					if v, ok := fromFontRef(s.fontRef); ok {
						o.Props = append(o.Props, Prop{pfx + key, v, s.origin})
						return
					}
				}
				continue
			}
			if s.list == nil {
				continue
			}
			for _, name := range []string{"lvl" + strconv.Itoa(lvl) + "pPr", "defPPr"} {
				if ppr := derive.Child(s.list, derive.DML, name); ppr != nil {
					if v, ok := fromPPr(ppr); ok {
						o.Props = append(o.Props, Prop{pfx + key, v, s.origin})
						return
					}
				}
			}
		}
	}
	pAttr := func(a string) getter {
		return func(ppr *tree.Node) (string, bool) { return ppr.Attr(a) }
	}
	rAttr := func(a string) getter {
		return func(ppr *tree.Node) (string, bool) {
			d := derive.Child(ppr, derive.DML, "defRPr")
			if d == nil {
				return "", false
			}
			return d.Attr(a)
		}
	}
	resolve("align", pAttr("algn"), nil)
	resolve("size", func(ppr *tree.Node) (string, bool) {
		v, ok := rAttr("sz")(ppr)
		if !ok {
			return "", false
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", false
		}
		return derive.FormatNum(f / 100), true
	}, nil)
	resolve("b", rAttr("b"), nil)
	resolve("i", rAttr("i"), nil)
	resolve("u", rAttr("u"), nil)
	resolve("color", func(ppr *tree.Node) (string, bool) {
		f := derive.FillElem(derive.Child(ppr, derive.DML, "defRPr"))
		if f == nil || f.Local() != "solidFill" {
			return "", false
		}
		c := r.color(o, pfx+"color", derive.ColorElem(f), nil)
		return ColorString(c), c != nil
	}, func(fr *tree.Node) (string, bool) {
		ce := derive.ColorElem(fr)
		if ce == nil {
			return "", false
		}
		c := r.color(o, pfx+"color", ce, nil)
		return ColorString(c), c != nil
	})
	resolve("font", func(ppr *tree.Node) (string, bool) {
		l := derive.Child(derive.Child(ppr, derive.DML, "defRPr"), derive.DML, "latin")
		if l == nil {
			return "", false
		}
		return l.Attr("typeface")
	}, func(fr *tree.Node) (string, bool) {
		switch idx, _ := fr.Attr("idx"); idx {
		case "major":
			return "+mj-lt", true
		case "minor":
			return "+mn-lt", true
		}
		return "", false
	})
	resolve("bullet", func(ppr *tree.Node) (string, bool) {
		return bulletOf(ppr)
	}, nil)
}

// bulletOf reads a paragraph's bullet choice: "none", "char:•" or
// "autonum:arabicPeriod".
func bulletOf(ppr *tree.Node) (string, bool) {
	for c := range ppr.ElementsSeq() {
		switch c.Local() {
		case "buNone":
			return "none", true
		case "buChar":
			ch, _ := c.Attr("char")
			return "char:" + ch, true
		case "buAutoNum":
			t, _ := c.Attr("type")
			return "autonum:" + t, true
		case "buBlip":
			return "picture", true
		}
	}
	return "", false
}

// html projects a text body as an XHTML fragment of local formatting, in
// the text grammar (internal/pptx/ops/text): text.ReadBodyWith reads it, with
// the slide's theme for displayed colours and its relationships for
// hyperlink targets, and text.Render writes it, so that the views show
// exactly what text.set reads back. Inherited formatting is not repeated
// in the HTML; it appears in the resolved level defaults.
func (r *reader) html(o *Object, body *tree.Node) (string, map[int]bool) {
	b := text.ReadBodyWith(body, textCtx{r: r, o: o})
	levels := map[int]bool{}
	for _, p := range b.Paras {
		levels[p.Lvl] = true
	}
	for p := range body.ElementsSeq() {
		if derive.Is(p, derive.DML, "p") {
			r.mathOmissions(o, p)
		}
	}
	return text.Render(b), levels
}

// mathOmissions records equations (a14:m), which the HTML does not show.
func (r *reader) mathOmissions(o *Object, n *tree.Node) {
	for c := range n.ElementsSeq() {
		switch {
		case derive.Is(c, derive.MC, "AlternateContent"):
			if br := derive.SelectBranch(c); br.Node != nil {
				r.mathOmissions(o, br.Node)
			}
		case c.Local() == "m":
			o.omit("text.math", "equation (a14:m) not projected")
		}
	}
}

// textCtx resolves a text body's colours through the slide's theme (the
// read half the colour codecs share, recording failures on the object)
// and its hyperlinks through the slide's relationships.
type textCtx struct {
	r *reader
	o *Object
}

// Color returns the hex of a colour element, or false.
func (c textCtx) Color(prop string, el *tree.Node) (string, bool) {
	col := c.r.color(c.o, prop, el, nil)
	if col == nil {
		return "", false
	}
	return strings.TrimPrefix(col.Hex, "#"), true
}

// Link returns the target and external flag of a relationship.
func (c textCtx) Link(rid string) (string, bool, bool) {
	if c.r.rels == nil {
		return "", false, false
	}
	rel, ok := c.r.rels.Get(rid)
	if !ok {
		return "", false, false
	}
	return rel.Target, rel.External(), true
}
