package text

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/htmlx"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// ErrGrammar is wrapped by every error Parse returns: the HTML uses
// something outside the text grammar. Nothing is ever dropped silently.
var ErrGrammar = errors.New("text: html outside the text grammar")

// Normalise parses h and renders it back: the form the read view shows
// after text.set writes h (given displayed colours that match the theme).
func Normalise(h string) (string, error) {
	b, err := Parse(h)
	if err != nil {
		return "", err
	}
	return Render(b), nil
}

// hnode is a parsed HTML element or text node.
type hnode struct {
	tag   string // "" for text
	attrs []hattr
	kids  []*hnode
	text  string
	off   int // byte offset in the source, for errors
}

type hattr struct{ name, val string }

// attr returns the value of the node's named attribute and whether it is present.
func (n *hnode) attr(name string) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			return a.val, true
		}
	}
	return "", false
}

// gerr returns a grammar error at byte offset off.
func gerr(off int, format string, args ...any) error {
	return fmt.Errorf("%w: at byte %d: %s", ErrGrammar, off, fmt.Sprintf(format, args...))
}

// Parse reads an HTML fragment in the grammar the read view emits:
//
//   - blocks: <p> or <ul>/<ol> holding <li>; or, instead of blocks, bare
//     inline content, which is one paragraph. Whitespace between blocks
//     is ignored.
//   - paragraph attributes: style="text-align:left|center|right|justify",
//     data-algn (other a:pPr@algn values), data-level (1-9),
//     data-bullet="none|picture" on <p>, data-char on <li> in <ul>,
//     data-type on <li> in <ol>.
//   - inline: text, <br/>, <b>, <i>, <u> (data-u for underline types other
//     than sng), <span> with style (font-weight:normal, font-style:normal,
//     text-decoration:none, font-size:Npt, color:var(--scheme) | #RRGGBB
//     | transparent, font-family:var(--theme-font) | 'name'), data-color
//     (a colour with modifiers, as the view writes it) and data-fill.
//
// Formatting elements may nest in any order; they must not contradict
// each other. Anything else is an error naming the element or attribute.
func Parse(h string) (Body, error) {
	root, err := parseDOM(h)
	if err != nil {
		return Body{}, err
	}
	block := false
	for _, k := range root.kids {
		switch k.tag {
		case "p", "ul", "ol":
			block = true
		case "li":
			return Body{}, gerr(k.off, "<li> outside <ul> or <ol>")
		}
	}
	var b Body
	if !block {
		p := Para{Lvl: 1}
		if p.Runs, err = inline(root.kids, Fmt{}); err != nil {
			return Body{}, err
		}
		b.Paras = append(b.Paras, p)
		return b, nil
	}
	for _, k := range root.kids {
		switch k.tag {
		case "":
			if strings.Trim(k.text, " \t\r\n") != "" {
				return Body{}, gerr(k.off, "text %q beside block elements; put it in a <p>", short(k.text))
			}
		case "p":
			p, err := para(k, "")
			if err != nil {
				return Body{}, err
			}
			b.Paras = append(b.Paras, p)
		case "ul", "ol":
			if len(k.attrs) > 0 {
				return Body{}, gerr(k.off, "<%s> takes no attributes (%s)", k.tag, k.attrs[0].name)
			}
			n := 0
			for _, li := range k.kids {
				switch li.tag {
				case "":
					if strings.Trim(li.text, " \t\r\n") != "" {
						return Body{}, gerr(li.off, "text %q directly in <%s>; put it in an <li>", short(li.text), k.tag)
					}
					continue
				case "li":
				default:
					return Body{}, gerr(li.off, "<%s> in <%s>: only <li> is allowed (levels are data-level, not nested lists)", li.tag, k.tag)
				}
				p, err := para(li, k.tag)
				if err != nil {
					return Body{}, err
				}
				b.Paras = append(b.Paras, p)
				n++
			}
			if n == 0 {
				return Body{}, gerr(k.off, "empty <%s>", k.tag)
			}
		default:
			return Body{}, gerr(k.off, "inline <%s> beside block elements; put it in a <p>", k.tag)
		}
	}
	return b, nil
}

// short returns s truncated to twenty bytes with an ellipsis when longer.
func short(s string) string {
	if len(s) > 20 {
		return s[:20] + "..."
	}
	return s
}

var (
	cssAlign = map[string]string{"left": "l", "center": "ctr", "right": "r", "justify": "just"}
	// The schema's values (alignments, autonumber schemes, underlines,
	// scheme, system and preset colour names, modifiers, sizes, levels)
	// are checked against the tables generated from the pinned schemas
	// (edit.CheckAttr, edit.CheckColor), not kept here.
	otherFills = set("gradFill", "blipFill", "pattFill", "grpFill")
)

// checkScheme checks a scheme colour name (a:ST_SchemeColorVal).
func checkScheme(name string) error {
	return edit.CheckSlideColor(derive.Color{Kind: "scheme", Val: name})
}

// set returns a set containing each given string.
func set(s ...string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, x := range s {
		m[x] = true
	}
	return m
}

// para reads a <p> (list "") or an <li> in a <ul> or <ol>.
func para(n *hnode, list string) (Para, error) {
	p := Para{Lvl: 1}
	allowed := map[string]bool{"style": true, "data-algn": true, "data-level": true,
		"data-bu-font": true, "data-bu-size": true, "data-bu-color": true, "data-tabs": true, "dir": true, "lang": true}
	switch list {
	case "":
		allowed["data-bullet"] = true
	case "ul":
		allowed["data-char"] = true
	case "ol":
		allowed["data-type"] = true
		allowed["data-start"] = true
	}
	for _, a := range n.attrs {
		if !allowed[a.name] {
			return p, gerr(n.off, "attribute %s on <%s> not allowed here", a.name, n.tag)
		}
	}
	if v, ok := n.attr("style"); ok {
		decls, err := cssDecls(v)
		if err != nil {
			return p, gerr(n.off, "<%s> style: %v", n.tag, err)
		}
		seen := map[string]bool{}
		for _, d := range decls {
			if seen[d.prop] {
				return p, gerr(n.off, "<%s> style sets %s twice", n.tag, d.prop)
			}
			seen[d.prop] = true
			if err := paraDecl(&p, d); err != nil {
				return p, gerr(n.off, "<%s> %v", n.tag, err)
			}
		}
	}
	if v, ok := n.attr("data-algn"); ok {
		if err := edit.CheckAttr("a:CT_TextParagraphProperties", "algn", v); err != nil {
			return p, gerr(n.off, "data-algn: %v", err)
		}
		if p.Algn != "" && p.Algn != v {
			return p, gerr(n.off, "text-align and data-algn=%q disagree", v)
		}
		p.Algn = v
	}
	if v, ok := n.attr("data-level"); ok {
		// data-level is lvl+1 (a:ST_TextIndentLevelType, 0 to 8).
		l, err := strconv.Atoi(v)
		if err != nil || strconv.Itoa(l) != v || edit.CheckAttr("a:CT_TextParagraphProperties", "lvl", strconv.Itoa(l-1)) != nil {
			return p, gerr(n.off, "data-level=%q: want 1 to 9", v)
		}
		p.Lvl = l
	}
	switch list {
	case "":
		if v, ok := n.attr("data-bullet"); ok {
			if v != "none" && v != "picture" {
				return p, gerr(n.off, "data-bullet=%q: want none or picture (bullets are <ul>/<ol>)", v)
			}
			p.Bullet = v
		}
	case "ul":
		v, ok := n.attr("data-char")
		if !ok {
			return p, gerr(n.off, "<li> in <ul> needs data-char (the bullet character); for an inherited bullet use <p>")
		}
		if utf8.RuneCountInString(v) == 0 {
			return p, gerr(n.off, "empty data-char")
		}
		p.Bullet = "char:" + v
	case "ol":
		v, ok := n.attr("data-type")
		if !ok {
			return p, gerr(n.off, "<li> in <ol> needs data-type (the autonumber scheme); for an inherited bullet use <p>")
		}
		if err := edit.CheckAttr("a:CT_TextAutonumberBullet", "type", v); err != nil {
			return p, gerr(n.off, "data-type: %v", err)
		}
		p.Bullet = "autonum:" + v
		if s, ok := n.attr("data-start"); ok {
			if err := edit.CheckAttr("a:CT_TextAutonumberBullet", "startAt", s); err != nil {
				return p, gerr(n.off, "data-start: %v", err)
			}
			p.PF.StartAt = s
		}
	}
	if err := bulletAttrs(n, &p.PF); err != nil {
		return p, err
	}
	if v, ok := n.attr("dir"); ok {
		if v != "rtl" && v != "ltr" {
			return p, gerr(n.off, "dir=%q: want rtl or ltr", v)
		}
		p.PF.Dir = v
	}
	ctx := Fmt{}
	if v, ok := n.attr("lang"); ok {
		if err := checkLang(v); err != nil {
			return p, gerr(n.off, "lang: %v", err)
		}
		ctx.Lang = v
	}
	var err error
	p.Runs, err = inline(n.kids, ctx)
	return p, err
}

// checkLang checks a language tag (s:ST_Lang); "" is no language.
func checkLang(v string) error {
	if strings.ContainsAny(v, " \t\r\n") {
		return fmt.Errorf("%q: a language tag has no spaces", v)
	}
	return edit.CheckAttr("a:CT_TextCharacterProperties", "lang", v)
}

var (
	reLen     = regexp.MustCompile(`^(-?([0-9]+(\.[0-9]+)?|\.[0-9]+))pt$`)
	rePct     = regexp.MustCompile(`^(-?([0-9]+(\.[0-9]+)?|\.[0-9]+))%$`)
	reTabStop = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?pt)(:([A-Za-z]+))?$`)
)

// lengthPt reads "Npt" (negative when neg) and returns its canonical
// form and EMU value, checked against attribute attr of a:pPr.
func lengthPt(v string, neg bool, ctype, attr string) (string, int64, error) {
	m := reLen.FindStringSubmatch(v)
	if m == nil || !neg && strings.HasPrefix(v, "-") {
		return "", 0, fmt.Errorf("%s: want points, such as 18pt", v)
	}
	x, _ := strconv.ParseFloat(m[1], 64)
	emu := derive.RoundEMU(x * derive.EMUPerPoint)
	if math.Abs(x) > 1e9 {
		return "", 0, fmt.Errorf("%s: out of range", v)
	}
	if err := edit.CheckAttr(ctype, attr, strconv.FormatInt(int64(emu), 10)); err != nil {
		return "", 0, fmt.Errorf("%s: %v", v, err)
	}
	return derive.FormatNum(float64(emu)/derive.EMUPerPoint) + "pt", int64(emu), nil
}

// spacing reads a CT_TextSpacing value: "N%" (a:spcPct, 1000ths of a
// percent, a:ST_TextSpacingPercentOrPercentString) or "Npt" (a:spcPts,
// 100ths of a point, a:ST_TextSpacingPoint).
func spacing(v string) (string, error) {
	if m := rePct.FindStringSubmatch(v); m != nil {
		x, ok := undecimal(m[1], 3)
		if !ok {
			return "", fmt.Errorf("%s: at most three decimals", v)
		}
		if err := edit.CheckAttr("a:CT_TextSpacingPercent", "val", strconv.FormatInt(x, 10)); err != nil {
			return "", fmt.Errorf("%s: %v", v, err)
		}
		return decimal(x, 3) + "%", nil
	}
	if m := reLen.FindStringSubmatch(v); m != nil {
		x, ok := undecimal(m[1], 2)
		if !ok {
			return "", fmt.Errorf("%s: at most two decimals", v)
		}
		if err := edit.CheckAttr("a:CT_TextSpacingPoint", "val", strconv.FormatInt(x, 10)); err != nil {
			return "", fmt.Errorf("%s: %v", v, err)
		}
		return decimal(x, 2) + "pt", nil
	}
	return "", fmt.Errorf("%s: want a percentage of a line (150%%) or points (12pt)", v)
}

// paraDecl applies one paragraph style declaration.
func paraDecl(p *Para, d decl) error {
	var err error
	switch d.prop {
	case "text-align":
		a, ok := cssAlign[d.val]
		if !ok {
			return fmt.Errorf("text-align:%s: want left, center, right or justify (data-algn for others)", d.val)
		}
		p.Algn = a
	case "line-height":
		p.PF.LnSpc, err = spacing(d.val)
	case "margin-top":
		p.PF.SpcBef, err = spacing(d.val)
	case "margin-bottom":
		p.PF.SpcAft, err = spacing(d.val)
	case "margin-left":
		p.PF.MarL, _, err = lengthPt(d.val, false, "a:CT_TextParagraphProperties", "marL")
	case "margin-right":
		p.PF.MarR, _, err = lengthPt(d.val, false, "a:CT_TextParagraphProperties", "marR")
	case "text-indent":
		p.PF.Indent, _, err = lengthPt(d.val, true, "a:CT_TextParagraphProperties", "indent")
	case "tab-size":
		p.PF.DefTab, _, err = lengthPt(d.val, false, "a:CT_TextParagraphProperties", "defTabSz")
	default:
		return fmt.Errorf("style property %s: paragraphs take text-align, line-height, margin-top, margin-bottom, margin-left, margin-right, text-indent and tab-size", d.prop)
	}
	if err != nil {
		return fmt.Errorf("%s:%v", d.prop, err)
	}
	return nil
}

// bulletAttrs reads the bullet font, size and colour and the tab stops.
func bulletAttrs(n *hnode, f *PFmt) error {
	if v, ok := n.attr("data-bu-font"); ok {
		if v == "text" {
			f.BuFont = v
		} else {
			tf, err := fontFamily(v)
			if err != nil {
				return gerr(n.off, "data-bu-font: %v (or text, to follow the text)", err)
			}
			if err := edit.CheckAttr("a:CT_TextFont", "typeface", tf); err != nil {
				return gerr(n.off, "data-bu-font: %v", err)
			}
			f.BuFont = fontCSS(tf)
		}
	}
	if v, ok := n.attr("data-bu-size"); ok {
		switch {
		case v == "text":
			f.BuSize = v
		case rePct.MatchString(v):
			// a:ST_TextBulletSizePercent: whole percentages from 25% to
			// 400%, written in 1000ths of a percent as PowerPoint writes
			// them.
			x, ok := undecimal(strings.TrimSuffix(v, "%"), 3)
			if !ok || x%1000 != 0 || edit.CheckValue("a:ST_TextBulletSizePercent", decimal(x, 3)+"%") != nil {
				return gerr(n.off, "data-bu-size=%q: want 25%% to 400%%, points (12pt) or text", v)
			}
			f.BuSize = decimal(x, 3) + "%"
		case reLen.MatchString(v):
			x, ok := undecimal(strings.TrimSuffix(v, "pt"), 2)
			if !ok || edit.CheckAttr("a:CT_TextBulletSizePoint", "val", strconv.FormatInt(x, 10)) != nil {
				return gerr(n.off, "data-bu-size=%q: want 1pt to 4000pt (a:ST_TextFontSize)", v)
			}
			f.BuSize = decimal(x, 2) + "pt"
		default:
			return gerr(n.off, "data-bu-size=%q: want a percentage of the text (75%%), points (12pt) or text", v)
		}
	}
	if v, ok := n.attr("data-bu-color"); ok {
		if v == "text" {
			f.BuClr = Fill{Kind: "text"}
		} else {
			c, disp, err := parseDataColor(v)
			if err != nil {
				return gerr(n.off, "data-bu-color=%q: %v", v, err)
			}
			if disp == "?" {
				disp = ""
			}
			f.BuClr = Fill{Kind: "solid", Color: Color{Color: c, Hex: disp}}
		}
	}
	if v, ok := n.attr("data-tabs"); ok {
		var stops []string
		for _, s := range strings.Fields(v) {
			m := reTabStop.FindStringSubmatch(s)
			if m == nil {
				return gerr(n.off, "data-tabs stop %q: want a position in points, optionally :l, :ctr, :r or :dec", s)
			}
			pos, _, err := lengthPt(m[1], false, "a:CT_TextTabStop", "pos")
			if err != nil {
				return gerr(n.off, "data-tabs: %v", err)
			}
			if m[4] != "" {
				if err := edit.CheckAttr("a:CT_TextTabStop", "algn", m[4]); err != nil {
					return gerr(n.off, "data-tabs stop %q: %v", s, err)
				}
				pos += ":" + m[4]
			}
			stops = append(stops, pos)
		}
		if len(stops) == 0 {
			return gerr(n.off, "empty data-tabs; leave it out for no tab stops")
		}
		f.Tabs = strings.Join(stops, " ")
	}
	return nil
}

// inline reads paragraph content under the formatting ctx.
func inline(kids []*hnode, ctx Fmt) ([]Run, error) {
	var out []Run
	var walk func(kids []*hnode, ctx Fmt) error
	walk = func(kids []*hnode, ctx Fmt) error {
		for _, k := range kids {
			if k.tag == "" {
				out = append(out, Run{Text: k.text, Fmt: ctx})
				continue
			}
			if k.tag == "br" {
				out = append(out, Run{Br: true})
				continue
			}
			f, err := apply(k, ctx)
			if err != nil {
				return err
			}
			if len(k.kids) == 0 && !f.zero() {
				out = append(out, Run{Fmt: f}) // an empty formatted run
				continue
			}
			if err := walk(k.kids, f); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(kids, ctx); err != nil {
		return nil, err
	}
	return canonical(out), nil
}

// apply returns ctx with the formatting of element n added.
func apply(n *hnode, ctx Fmt) (Fmt, error) {
	only := func(names ...string) error {
		for _, a := range n.attrs {
			ok := false
			for _, m := range names {
				ok = ok || a.name == m
			}
			if !ok {
				return gerr(n.off, "attribute %s on <%s> not allowed", a.name, n.tag)
			}
		}
		return nil
	}
	f := ctx
	switch n.tag {
	case "b", "i":
		if err := only(); err != nil {
			return f, err
		}
		cur := &f.B
		if n.tag == "i" {
			cur = &f.I
		}
		if *cur == "off" {
			return f, gerr(n.off, "<%s> inside a span that sets it normal", n.tag)
		}
		*cur = "on"
	case "u":
		if err := only("data-u"); err != nil {
			return f, err
		}
		u := "sng"
		if v, ok := n.attr("data-u"); ok {
			if v == "none" {
				return f, gerr(n.off, "data-u=\"none\": use text-decoration:none")
			}
			if err := edit.CheckAttr("a:CT_TextCharacterProperties", "u", v); err != nil {
				return f, gerr(n.off, "data-u: %v", err)
			}
			u = v
		}
		if f.U != "" && f.U != u {
			return f, gerr(n.off, "<u> contradicts an enclosing underline (%s)", f.U)
		}
		f.U = u
	case "s":
		if err := only("data-strike"); err != nil {
			return f, err
		}
		st := "sngStrike"
		if v, ok := n.attr("data-strike"); ok {
			if v == "noStrike" {
				return f, gerr(n.off, "<s data-strike=\"noStrike\">: use <span data-strike=\"noStrike\">")
			}
			if err := edit.CheckAttr("a:CT_TextCharacterProperties", "strike", v); err != nil {
				return f, gerr(n.off, "data-strike: %v", err)
			}
			st = v
		}
		if f.Strike != "" && f.Strike != st {
			return f, gerr(n.off, "<s> contradicts an enclosing strike (%s)", f.Strike)
		}
		f.Strike = st
	case "a":
		if err := only("href", "title", "data-link"); err != nil {
			return f, err
		}
		var l Link
		href, hasHref := n.attr("href")
		other, hasOther := n.attr("data-link")
		switch {
		case hasHref && hasOther:
			return f, gerr(n.off, "<a> has both href and data-link")
		case hasHref:
			if strings.TrimSpace(href) == "" {
				return f, gerr(n.off, "<a> with an empty href")
			}
			l.Href = href
		case hasOther:
			if other == "" {
				return f, gerr(n.off, "<a> with an empty data-link")
			}
			l.Other = other
		default:
			return f, gerr(n.off, "<a> needs href (an external link) or data-link (a link shown as read)")
		}
		l.Title, _ = n.attr("title")
		if !f.Link.zero() && f.Link != l {
			return f, gerr(n.off, "<a> inside another link")
		}
		f.Link = l
	case "span":
		if err := only("style", "data-color", "data-fill", "data-highlight", "data-strike", "lang"); err != nil {
			return f, err
		}
		if v, ok := n.attr("data-strike"); ok {
			if v != "noStrike" {
				return f, gerr(n.off, "data-strike=%q on <span>: only noStrike (use <s> for strikes)", v)
			}
			if f.Strike != "" && f.Strike != v {
				return f, gerr(n.off, "data-strike=\"noStrike\" inside <s>")
			}
			f.Strike = v
		}
		if v, ok := n.attr("lang"); ok {
			if err := checkLang(v); err != nil {
				return f, gerr(n.off, "lang: %v", err)
			}
			f.Lang = v
		}
		return span(n, f)
	case "p", "ul", "ol", "li":
		return f, gerr(n.off, "block <%s> inside a paragraph", n.tag)
	default:
		return f, gerr(n.off, "element <%s> is not in the text grammar", n.tag)
	}
	return f, nil
}

var (
	reHex  = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	reVar  = regexp.MustCompile(`^var\(--([A-Za-z0-9-]+)\)$`)
	reSize = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?|\.[0-9]+)pt$`)
)

// span adds a <span>'s formatting to f.
func span(n *hnode, f Fmt) (Fmt, error) {
	setTri := func(cur *string, prop string) error {
		if *cur == "on" {
			return gerr(n.off, "%s:normal inside <%s>", prop, map[string]string{"font-weight": "b", "font-style": "i"}[prop])
		}
		*cur = "off"
		return nil
	}
	var color, bg string // the style's color and background-color values
	hasColor, hasBG := false, false
	seen := map[string]bool{}
	setCap := func(c string) error {
		if f.Cap != "" && f.Cap != c {
			return gerr(n.off, "capitals %s contradict an enclosing %s", c, f.Cap)
		}
		f.Cap = c
		return nil
	}
	if v, ok := n.attr("style"); ok {
		decls, err := cssDecls(v)
		if err != nil {
			return f, gerr(n.off, "<span> style: %v", err)
		}
		for _, d := range decls {
			if seen[d.prop] {
				return f, gerr(n.off, "<span> style sets %s twice", d.prop)
			}
			seen[d.prop] = true
			switch d.prop {
			case "font-weight", "font-style":
				if d.val != "normal" {
					return f, gerr(n.off, "%s:%s: only normal (use <%s> for the other)", d.prop, d.val, map[string]string{"font-weight": "b", "font-style": "i"}[d.prop])
				}
				cur := &f.B
				if d.prop == "font-style" {
					cur = &f.I
				}
				if err := setTri(cur, d.prop); err != nil {
					return f, err
				}
			case "text-decoration":
				if d.val != "none" {
					return f, gerr(n.off, "text-decoration:%s: only none (use <u> for underlines)", d.val)
				}
				if f.U != "" && f.U != "none" {
					return f, gerr(n.off, "text-decoration:none inside <u>")
				}
				f.U = "none"
			case "font-size":
				m := reSize.FindStringSubmatch(d.val)
				if m == nil {
					return f, gerr(n.off, "font-size:%s: want points, such as 18pt", d.val)
				}
				x, _ := strconv.ParseFloat(strings.TrimSuffix(d.val, "pt"), 64)
				sz := math.Round(x * 100)
				if sz > math.MaxInt32 || edit.CheckAttr("a:CT_TextCharacterProperties", "sz", strconv.FormatInt(int64(sz), 10)) != nil {
					return f, gerr(n.off, "font-size:%s: out of range (1pt to 4000pt, a:ST_TextFontSize)", d.val)
				}
				s := derive.FormatNum(sz / 100)
				if f.Size != "" && f.Size != s {
					return f, gerr(n.off, "font-size:%s contradicts an enclosing %spt", d.val, f.Size)
				}
				f.Size = s
			case "color":
				color, hasColor = d.val, true
			case "background-color":
				bg, hasBG = d.val, true
			case "font-family":
				font, err := fontFamily(d.val)
				if err != nil {
					return f, gerr(n.off, "%v", err)
				}
				if f.HasFont && f.Font != font {
					return f, gerr(n.off, "font-family contradicts an enclosing one")
				}
				f.Font, f.HasFont = font, true
			case "vertical-align":
				m := rePct.FindStringSubmatch(d.val)
				var x int64
				ok := m != nil
				if ok {
					x, ok = undecimal(m[1], 3)
				}
				if !ok || edit.CheckAttr("a:CT_TextCharacterProperties", "baseline", strconv.FormatInt(x, 10)) != nil {
					return f, gerr(n.off, "vertical-align:%s: want the baseline shift as a percentage of the font size (30%%, -25%%), at most three decimals", d.val)
				}
				b := decimal(x, 3) + "%"
				if f.Baseline != "" && f.Baseline != b {
					return f, gerr(n.off, "vertical-align:%s contradicts an enclosing %s", d.val, f.Baseline)
				}
				f.Baseline = b
			case "text-transform":
				c := map[string]string{"uppercase": "all", "none": "none"}[d.val]
				if c == "" {
					return f, gerr(n.off, "text-transform:%s: want uppercase or none", d.val)
				}
				if err := setCap(c); err != nil {
					return f, err
				}
			case "font-variant":
				if d.val != "small-caps" {
					return f, gerr(n.off, "font-variant:%s: only small-caps", d.val)
				}
				if err := setCap("small"); err != nil {
					return f, err
				}
			case "letter-spacing":
				m := reLen.FindStringSubmatch(d.val)
				var x int64
				ok := m != nil
				if ok {
					x, ok = undecimal(m[1], 2)
				}
				if !ok || edit.CheckAttr("a:CT_TextCharacterProperties", "spc", strconv.FormatInt(x, 10)) != nil {
					return f, gerr(n.off, "letter-spacing:%s: want points from -4000pt to 4000pt, at most two decimals", d.val)
				}
				sp := decimal(x, 2) + "pt"
				if f.Spc != "" && f.Spc != sp {
					return f, gerr(n.off, "letter-spacing:%s contradicts an enclosing %s", d.val, f.Spc)
				}
				f.Spc = sp
			default:
				return f, gerr(n.off, "<span> style property %s is not in the text grammar", d.prop)
			}
		}
	}
	dc, hasDC := n.attr("data-color")
	df, hasDF := n.attr("data-fill")
	var fill Fill
	switch {
	case hasDC && hasDF:
		return f, gerr(n.off, "<span> has both data-color and data-fill")
	case hasDF:
		if !otherFills[df] {
			return f, gerr(n.off, "data-fill=%q: want gradFill, blipFill, pattFill or grpFill", df)
		}
		if hasColor {
			return f, gerr(n.off, "<span> has both color and data-fill")
		}
		fill = Fill{Kind: df}
	default:
		var err error
		if fill, err = colorValue(n, "color", color, hasColor, "data-color", dc, hasDC, true); err != nil {
			return f, err
		}
	}
	if fill.Kind != "" {
		if f.Fill.Kind != "" && !sameFill(f.Fill, fill) {
			return f, gerr(n.off, "<span> colour contradicts an enclosing one")
		}
		f.Fill = fill
	}
	dh, hasDH := n.attr("data-highlight")
	hl, err := colorValue(n, "background-color", bg, hasBG, "data-highlight", dh, hasDH, false)
	if err != nil {
		return f, err
	}
	if hl.Kind != "" {
		if f.Highlight.Kind != "" && !sameFill(f.Highlight, hl) {
			return f, gerr(n.off, "<span> highlight contradicts an enclosing one")
		}
		f.Highlight = hl
	}
	return f, nil
}

// colorValue reads a colour given as a CSS property (css, when has) and
// its data attribute (data, when hasData): var(--scheme), #RRGGBB, the
// data attribute's colour as written with the #RRGGBB it displays beside
// it, or (a text colour only) transparent.
func colorValue(n *hnode, prop, css string, has bool, attr, data string, hasData, transparent bool) (Fill, error) {
	switch {
	case hasData:
		c, disp, err := parseDataColor(data)
		if err != nil {
			return Fill{}, gerr(n.off, "%s=%q: %v", attr, data, err)
		}
		switch {
		case !has:
		case strings.HasPrefix(css, "#") && reHex.MatchString(css):
			got := strings.ToUpper(css[1:])
			if disp == "?" {
				return Fill{}, gerr(n.off, "%s:%s with %s %q, which has no displayed colour; drop %s to set a plain colour", prop, css, attr, data, attr)
			}
			if disp != "" && disp != got {
				return Fill{}, gerr(n.off, "%s:%s and %s=%q disagree; drop %s to set a plain colour", prop, css, attr, data, attr)
			}
			disp = got
		default:
			return Fill{}, gerr(n.off, "%s:%s with %s: %s goes with a #RRGGBB colour only", prop, css, attr, attr)
		}
		if disp == "?" {
			disp = ""
		}
		return Fill{Kind: "solid", Color: Color{Color: c, Hex: disp}}, nil
	case has:
		switch {
		case css == "transparent" && transparent:
			return Fill{Kind: "none"}, nil
		case reHex.MatchString(css):
			v := strings.ToUpper(css[1:])
			return Fill{Kind: "solid", Color: Color{Color: derive.Color{Kind: "srgb", Val: v}, Hex: v}}, nil
		}
		m := reVar.FindStringSubmatch(css)
		if m == nil {
			if transparent {
				return Fill{}, gerr(n.off, "%s:%s: want var(--scheme), #RRGGBB or transparent", prop, css)
			}
			return Fill{}, gerr(n.off, "%s:%s: want var(--scheme) or #RRGGBB", prop, css)
		}
		if err := checkScheme(m[1]); err != nil {
			return Fill{}, gerr(n.off, "%s:%s: %v", prop, css, err)
		}
		return Fill{Kind: "solid", Color: Color{Color: derive.Color{Kind: "scheme", Val: m[1]}}}, nil
	}
	return Fill{}, nil
}

// sameFill reports whether two fills show the same colour as written.
func sameFill(a, b Fill) bool {
	if a.Kind != b.Kind {
		return false
	}
	return a.Kind != "solid" || a.Color.spec() == b.Color.spec()
}

// parseDataColor reads a data-color value: a colour as the read model
// writes it ("#RRGGBB", "srgb:RRGGBB mods", "accent1 mods", "sys:name",
// "prst:name", "hsl:h,s,l", "scrgb:r,g,b", then modifiers "name=val" or
// "name"), optionally followed by the displayed colour ("#RRGGBB", or "#?"
// when unknown). It returns the colour and the displayed hex ("" if not
// given, "?" if unknown).
func parseDataColor(s string) (derive.Color, string, error) {
	toks := strings.Fields(s)
	if len(toks) == 0 {
		return derive.Color{}, "", errors.New("empty")
	}
	var c derive.Color
	base := toks[0]
	toks = toks[1:]
	disp := ""
	if n := len(toks); n > 0 && strings.HasPrefix(toks[n-1], "#") {
		d := toks[n-1]
		toks = toks[:n-1]
		switch {
		case d == "#?":
			disp = "?"
		case reHex.MatchString(d):
			disp = strings.ToUpper(d[1:])
		default:
			return c, "", fmt.Errorf("displayed colour %q: want #RRGGBB or #?", d)
		}
	}
	kind, val, hasKind := strings.Cut(base, ":")
	switch {
	case !hasKind && strings.HasPrefix(base, "#"):
		if !reHex.MatchString(base) {
			return c, "", fmt.Errorf("%q: want #RRGGBB", base)
		}
		if len(toks) > 0 || disp != "" {
			return c, "", errors.New("a #RRGGBB colour takes no modifiers; use srgb:RRGGBB")
		}
		v := strings.ToUpper(base[1:])
		return derive.Color{Kind: "srgb", Val: v}, v, nil
	case !hasKind:
		c = derive.Color{Kind: "scheme", Val: base}
	case kind == "srgb":
		if !reHex.MatchString("#" + val) {
			return c, "", fmt.Errorf("srgb:%s: want six hex digits", val)
		}
		c = derive.Color{Kind: "srgb", Val: strings.ToUpper(val)}
	case kind == "sys" || kind == "prst":
		c = derive.Color{Kind: kind, Val: val}
	case kind == "hsl" || kind == "scrgb":
		parts := strings.Split(val, ",")
		if len(parts) != 3 {
			return c, "", fmt.Errorf("%s:%s: want three integers", kind, val)
		}
		c = derive.Color{Kind: kind}
		for i, p := range parts {
			v, err := strconv.ParseInt(p, 10, 64)
			if err != nil {
				return c, "", fmt.Errorf("%s:%s: want three integers", kind, val)
			}
			c.Comp[i] = v
		}
	default:
		return c, "", fmt.Errorf("colour kind %q: want srgb, sys, prst, hsl, scrgb or a scheme name", kind)
	}
	for _, t := range toks {
		name, v, hasVal := strings.Cut(t, "=")
		if hasVal && v == "" {
			return c, "", fmt.Errorf("modifier %s: empty value", name)
		}
		c.Mods = append(c.Mods, derive.Mod{Name: name, Val: v})
	}
	// Names, components and modifiers against the generated schema tables.
	if err := edit.CheckSlideColor(c); err != nil {
		return c, "", err
	}
	return c, disp, nil
}

// fontFamily reads a font-family value: var(--theme-font) or one quoted
// name.
func fontFamily(v string) (string, error) {
	if m := reVar.FindStringSubmatch(v); m != nil {
		return "+" + m[1], nil
	}
	if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') {
		name, err := htmlx.QuotedName(v)
		if err != nil {
			return "", fmt.Errorf("font-family:%s: one quoted name only", v)
		}
		return name, nil
	}
	return "", fmt.Errorf("font-family:%s: want var(--theme-font) or one quoted name", v)
}

type decl struct{ prop, val string }

// cssDecls splits a style attribute into declarations, respecting quotes
// and parentheses.
func cssDecls(s string) ([]decl, error) {
	var out []decl
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ';' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if quote != 0 || depth != 0 {
		return nil, fmt.Errorf("unbalanced quotes or parentheses in %q", s)
	}
	parts = append(parts, s[start:])
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		prop, val, ok := strings.Cut(p, ":")
		if !ok {
			return nil, fmt.Errorf("declaration %q has no value", p)
		}
		out = append(out, decl{strings.TrimSpace(prop), strings.TrimSpace(val)})
	}
	return out, nil
}

// maxHTMLDepth is the deepest element nesting parseDOM accepts, which
// bounds the recursive walks over the parsed tree (inline). The grammar's
// formatting elements are few and must not contradict each other, so real
// fragments nest a handful deep.
const maxHTMLDepth = htmlx.MaxDepth - 1 // the front end counts its root

// parseDOM parses the fragment with the shared HTML front end
// (htmlx.ParseHTML in strict mode: HTML5 syntax, with only the repairs
// that cannot change meaning) and checks it against the text grammar: every element
// is one of the grammar's, <br> is empty and takes no attributes, and
// every character is one XML can store. Nothing is dropped: what the
// grammar does not have is an error naming it (docs/contract.md,
// section 7).
func parseDOM(h string) (*hnode, error) {
	root, err := htmlx.ParseHTML(h, htmlx.Options{SelfClosing: grammarTags, Strict: true})
	if err != nil {
		var se *htmlx.SyntaxError
		if errors.As(err, &se) {
			return nil, gerr(se.Offset, "HTML syntax error: %s", se.Code)
		}
		var ue *htmlx.UnsupportedError
		if errors.As(err, &ue) {
			return nil, gerr(ue.Off, "%s", ue.Msg)
		}
		return nil, err
	}
	root.Tag = "#root"
	if err := checkDOM(root); err != nil {
		return nil, err
	}
	return fromHTMLX(root), nil
}

// checkDOM checks a parsed tree against the grammar's vocabulary. The
// tree is at most htmlx.MaxDepth deep, which bounds the walk.
func checkDOM(n *htmlx.Node) error {
	for _, k := range n.Kids {
		if k.Tag == "" {
			for i, c := range k.Text {
				if !storable(c) {
					return gerr(k.Off+i, "character %U cannot be stored in XML", c)
				}
			}
			continue
		}
		if !grammarTags[k.Tag] {
			return gerr(k.Off, "element <%s> is not in the text grammar", k.Tag)
		}
		for _, a := range k.Attrs {
			for _, c := range a.Value {
				if !storable(c) {
					return gerr(k.Off, "character %U in %s cannot be stored in XML", c, a.Name)
				}
			}
		}
		if k.Tag == "br" && len(k.Attrs) > 0 {
			return gerr(k.Off, "<br> takes no attributes (%s)", k.Attrs[0].Name)
		}
		if err := checkDOM(k); err != nil {
			return err
		}
	}
	return nil
}

// fromHTMLX converts a parsed tree, at most maxHTMLDepth deep.
func fromHTMLX(n *htmlx.Node) *hnode {
	h := &hnode{tag: n.Tag, text: n.Text, off: n.Off}
	for _, a := range n.Attrs {
		h.attrs = append(h.attrs, hattr{a.Name, a.Value})
	}
	for _, k := range n.Kids {
		h.kids = append(h.kids, fromHTMLX(k))
	}
	return h
}

var grammarTags = set("p", "ul", "ol", "li", "b", "i", "u", "s", "a", "span", "br")

// storable reports whether XML 1.0 can carry c.
func storable(c rune) bool { return htmlx.Storable(c) }
