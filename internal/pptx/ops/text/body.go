package text

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Body is a text body as the views show it: only the object's own
// paragraph and run formatting, never inherited values.
type Body struct {
	Paras []Para
}

// Para is one paragraph (a:p).
type Para struct {
	// Algn is a:pPr@algn as written ("l", "ctr", "r", "just", "dist",
	// ...), "" when the paragraph does not set it.
	Algn string
	// Lvl is the 1-based level shown; 1 when the paragraph shows none.
	Lvl int
	// Bullet is the paragraph's own bullet choice: "" (none set),
	// "none" (a:buNone), "char:X" (a:buChar), "autonum:T" (a:buAutoNum) or
	// "picture" (a:buBlip, which HTML cannot create).
	Bullet string
	// PF is the rest of the paragraph formatting the HTML shows.
	PF   PFmt
	Runs []Run
}

// PFmt is the paragraph formatting the HTML shows beyond alignment,
// level and bullet choice, each in its canonical HTML form and "" when
// the paragraph does not set it (see "Text writes (M3)" in
// docs/design.md for the encoding).
type PFmt struct {
	// LnSpc, SpcBef and SpcAft are a:lnSpc, a:spcBef and a:spcAft:
	// "150%" (a:spcPct, percent of a line) or "12pt" (a:spcPts).
	LnSpc, SpcBef, SpcAft string
	// MarL, MarR and Indent are @marL, @marR and @indent in points
	// ("18pt"; indent may be negative).
	MarL, MarR, Indent string
	// DefTab is @defTabSz in points.
	DefTab string
	// Tabs is a:tabLst: each stop's position in points, with ":algn"
	// when the stop states one, space separated ("36pt 72pt:ctr").
	Tabs string
	// Dir is @rtl: "rtl" (true) or "ltr" (false).
	Dir string
	// StartAt is a:buAutoNum@startAt.
	StartAt string
	// BuFont is the bullet font: "text" (a:buFontTx), or the a:buFont
	// typeface as a CSS font-family value ("'Wingdings'", or
	// "var(--mn-lt)" for a theme font).
	BuFont string
	// BuSize is the bullet size: "text" (a:buSzTx), "75%" (a:buSzPct) or
	// "12pt" (a:buSzPts).
	BuSize string
	// BuClr is the bullet colour: Kind "" (not set), "text" (a:buClrTx)
	// or "solid" (a:buClr with Color).
	BuClr Fill
}

// Run is one text run (a:r or a:fld) or, when Br is set, a line break
// (a:br). Runs are kept canonical: adjacent runs that differ in nothing
// HTML shows but their language are merged when their language agrees,
// and empty ones that show nothing are dropped, because HTML cannot show
// their boundaries.
type Run struct {
	Br   bool
	Text string
	Fmt  Fmt
}

// Fmt is a run's own formatting. The zero Fmt sets nothing.
type Fmt struct {
	// B and I are "" (not set), "on" or "off".
	B, I string
	// U is a:rPr@u: "" (not set), "sng", "none" or another underline type.
	U string
	// Strike is a:rPr@strike: "", "noStrike", "sngStrike" or "dblStrike".
	Strike string
	// Size is the font size in points as shown ("18", "10.5"), "" if not
	// set.
	Size string
	Fill Fill
	// Font is the latin typeface as written ("+mj-lt" for a theme font),
	// meaningful when HasFont is set.
	Font    string
	HasFont bool
	// Baseline is a:rPr@baseline as a percentage ("30%", "-25%").
	Baseline string
	// Cap is a:rPr@cap: "", "none", "small" or "all".
	Cap string
	// Spc is a:rPr@spc (letter spacing) in points ("1.5pt").
	Spc string
	// Highlight is a:highlight: Kind "" or "solid" with its colour.
	Highlight Fill
	// Lang is a:rPr@lang ("" when not set).
	Lang string
	// Link is a:hlinkClick.
	Link Link
}

// Link is a run's click hyperlink (a:hlinkClick).
type Link struct {
	// Href is the target of an external hyperlink relationship (a URL),
	// the link HTML can create.
	Href string
	// Other is a link HTML shows but cannot create: the action
	// (ppaction://...) or "internal" for a link to a part of the package,
	// "unresolved" when the relationship cannot be read.
	Other string
	// Title is @tooltip.
	Title string
}

// zero reports whether the link is the zero value.
func (l Link) zero() bool { return l == Link{} }

// Fill is a run's text fill.
type Fill struct {
	// Kind is "" (not set, or a:solidFill without a colour), "solid",
	// "none" (a:noFill) or the element name of a fill HTML shows but
	// cannot create: gradFill, blipFill, pattFill, grpFill. For a
	// bullet colour it is "", "solid" or "text" (a:buClrTx).
	Kind  string
	Color Color
}

// Color is a solid fill's colour as written, plus its displayed hex.
type Color struct {
	derive.Color
	// Hex is the displayed RRGGBB without '#', "" if unknown. It is a
	// derived value: it is shown, never compared or written.
	Hex string
}

// zero reports whether f sets nothing the HTML would show.
func (f Fmt) zero() bool { return fmtKey(f) == "" }

// bare reports whether f shows nothing but (possibly) a language: such
// runs show no boundary between them when their languages agree.
func (f Fmt) bare() bool {
	g := f
	g.Lang = ""
	return g.zero()
}

// spec is the colour as written in the form of the read model's
// data-color attribute, without the displayed hex.
func (c Color) spec() string {
	var parts []string
	switch c.Kind {
	case "srgb":
		if len(c.Mods) == 0 {
			return "#" + c.Val
		}
		parts = append(parts, "srgb:"+c.Val)
	case "scheme":
		parts = append(parts, c.Val)
	case "hsl", "scrgb":
		parts = append(parts, c.Kind+":"+strconv.FormatInt(c.Comp[0], 10)+","+strconv.FormatInt(c.Comp[1], 10)+","+strconv.FormatInt(c.Comp[2], 10))
	default:
		parts = append(parts, c.Kind+":"+c.Val)
	}
	for _, m := range c.Mods {
		if m.Val == "" {
			parts = append(parts, m.Name)
		} else {
			parts = append(parts, m.Name+"="+m.Val)
		}
	}
	return strings.Join(parts, " ")
}

// plain reports whether the colour shows as a CSS variable.
func (c Color) plain() bool { return c.Kind == "scheme" && len(c.Mods) == 0 }

// dataColor is the colour in data-color form: the spec, followed by the
// displayed hex (or #? when unknown) unless it is a plain sRGB colour.
func (c Color) dataColor(hex bool) string {
	s := c.spec()
	if hex && !(c.Kind == "srgb" && len(c.Mods) == 0) {
		if c.Hex != "" {
			s += " #" + c.Hex
		} else {
			s += " #?"
		}
	}
	return s
}

// Render writes b as HTML in the grammar the read view emits, with
// displayed colours.
func Render(b Body) string { return render(b, true) }

// Equal reports whether a and b show the same text body, colours compared
// as written rather than as displayed.
func Equal(a, b Body) bool { return key(a) == key(b) }

// key renders b without displayed colour values: two bodies with the
// same key show the same thing whatever the theme.
func key(b Body) string { return render(b, false) }

// render writes a text body as HTML, with displayed colours when hex is set.
func render(b Body, hex bool) string {
	var w strings.Builder
	list := ""
	for _, p := range b.Paras {
		want, tag := "", "p"
		switch {
		case strings.HasPrefix(p.Bullet, "char:"):
			want, tag = "ul", "li"
		case strings.HasPrefix(p.Bullet, "autonum:"):
			want, tag = "ol", "li"
		}
		if want != list {
			if list != "" {
				w.WriteString("</" + list + ">")
			}
			if want != "" {
				w.WriteString("<" + want + ">")
			}
			list = want
		}
		renderPara(&w, p, tag, hex)
	}
	if list != "" {
		w.WriteString("</" + list + ">")
	}
	return w.String()
}

var algnCSS = map[string]string{"l": "left", "ctr": "center", "r": "right", "just": "justify"}

// paraLang is the language a paragraph's tag shows: the language of
// every run (line breaks aside) when they all have the same one, else "".
func paraLang(runs []Run) string {
	lang, n := "", 0
	for _, r := range runs {
		if r.Br {
			continue
		}
		if n == 0 {
			lang = r.Fmt.Lang
		} else if r.Fmt.Lang != lang {
			return ""
		}
		n++
	}
	return lang
}

// paraStyles are the paragraph's CSS declarations, in canonical order.
func paraStyles(p Para) (styles []string) {
	if p.Algn != "" {
		if css, ok := algnCSS[p.Algn]; ok {
			styles = append(styles, "text-align:"+css)
		}
	}
	f := p.PF
	for _, d := range []struct{ prop, v string }{
		{"line-height", f.LnSpc}, {"margin-top", f.SpcBef}, {"margin-bottom", f.SpcAft},
		{"margin-left", f.MarL}, {"margin-right", f.MarR}, {"text-indent", f.Indent}, {"tab-size", f.DefTab},
	} {
		if d.v != "" {
			styles = append(styles, d.prop+":"+d.v)
		}
	}
	return styles
}

// renderPara writes one paragraph as HTML in the given tag.
func renderPara(w *strings.Builder, p Para, tag string, hex bool) {
	runs := canonical(p.Runs)
	lang := paraLang(runs)
	w.WriteString("<" + tag)
	var attrs []string
	if styles := paraStyles(p); len(styles) > 0 {
		w.WriteString(` style="` + attrEsc(strings.Join(styles, ";")) + `"`)
	}
	if p.Algn != "" {
		if _, ok := algnCSS[p.Algn]; !ok {
			attrs = append(attrs, `data-algn="`+attrEsc(p.Algn)+`"`)
		}
	}
	if p.Lvl > 1 {
		attrs = append(attrs, `data-level="`+strconv.Itoa(p.Lvl)+`"`)
	}
	switch {
	case strings.HasPrefix(p.Bullet, "char:"):
		attrs = append(attrs, `data-char="`+attrEsc(strings.TrimPrefix(p.Bullet, "char:"))+`"`)
	case strings.HasPrefix(p.Bullet, "autonum:"):
		attrs = append(attrs, `data-type="`+attrEsc(strings.TrimPrefix(p.Bullet, "autonum:"))+`"`)
	case p.Bullet != "":
		attrs = append(attrs, `data-bullet="`+attrEsc(p.Bullet)+`"`)
	}
	f := p.PF
	if f.StartAt != "" {
		attrs = append(attrs, `data-start="`+attrEsc(f.StartAt)+`"`)
	}
	if f.BuFont != "" {
		attrs = append(attrs, `data-bu-font="`+attrEsc(f.BuFont)+`"`)
	}
	if f.BuSize != "" {
		attrs = append(attrs, `data-bu-size="`+attrEsc(f.BuSize)+`"`)
	}
	switch f.BuClr.Kind {
	case "text":
		attrs = append(attrs, `data-bu-color="text"`)
	case "solid":
		attrs = append(attrs, `data-bu-color="`+attrEsc(f.BuClr.Color.dataColor(hex))+`"`)
	}
	if f.Tabs != "" {
		attrs = append(attrs, `data-tabs="`+attrEsc(f.Tabs)+`"`)
	}
	if f.Dir != "" {
		attrs = append(attrs, `dir="`+f.Dir+`"`)
	}
	if lang != "" {
		attrs = append(attrs, `lang="`+attrEsc(lang)+`"`)
	}
	for _, a := range attrs {
		w.WriteString(" " + a)
	}
	w.WriteString(">")
	for _, r := range runs {
		renderRun(w, r, hex, lang)
	}
	w.WriteString("</" + tag + ">")
}

// colorCSS writes a colour property: var(--scheme) for a plain scheme
// colour, else the displayed #hex (when known and hex is set) with the
// colour as written in the data attribute.
func colorCSS(prop, data string, c Color, hex bool, styles, attrs *[]string) {
	if c.plain() {
		*styles = append(*styles, prop+":var(--"+c.Val+")")
		return
	}
	if hex && c.Hex != "" {
		*styles = append(*styles, prop+":#"+c.Hex)
	}
	*attrs = append(*attrs, data+`="`+attrEsc(c.dataColor(hex))+`"`)
}

// renderRun writes a run; plang is the language its paragraph's tag
// shows, which the run does not repeat.
func renderRun(w *strings.Builder, r Run, hex bool, plang string) {
	if r.Br {
		w.WriteString("<br/>")
		return
	}
	f := r.Fmt
	var closes []string
	open := func(tag, end string) {
		w.WriteString(tag)
		closes = append(closes, end)
	}
	switch {
	case f.Link.Href != "":
		a := `<a href="` + attrEsc(f.Link.Href) + `"`
		if f.Link.Title != "" {
			a += ` title="` + attrEsc(f.Link.Title) + `"`
		}
		open(a+">", "</a>")
	case f.Link.Other != "":
		a := `<a data-link="` + attrEsc(f.Link.Other) + `"`
		if f.Link.Title != "" {
			a += ` title="` + attrEsc(f.Link.Title) + `"`
		}
		open(a+">", "</a>")
	}
	if f.B == "on" {
		open("<b>", "</b>")
	}
	if f.I == "on" {
		open("<i>", "</i>")
	}
	switch f.U {
	case "", "none":
	case "sng":
		open("<u>", "</u>")
	default:
		open(`<u data-u="`+attrEsc(f.U)+`">`, "</u>")
	}
	switch f.Strike {
	case "", "noStrike":
	case "sngStrike":
		open("<s>", "</s>")
	default:
		open(`<s data-strike="`+attrEsc(f.Strike)+`">`, "</s>")
	}
	var styles, attrs []string
	if f.B == "off" {
		styles = append(styles, "font-weight:normal")
	}
	if f.I == "off" {
		styles = append(styles, "font-style:normal")
	}
	if f.U == "none" {
		styles = append(styles, "text-decoration:none")
	}
	if f.Size != "" {
		styles = append(styles, "font-size:"+f.Size+"pt")
	}
	switch f.Fill.Kind {
	case "solid":
		colorCSS("color", "data-color", f.Fill.Color, hex, &styles, &attrs)
	case "none":
		styles = append(styles, "color:transparent")
	case "":
	default:
		attrs = append(attrs, `data-fill="`+f.Fill.Kind+`"`)
	}
	if f.HasFont {
		styles = append(styles, "font-family:"+fontCSS(f.Font))
	}
	if f.Baseline != "" {
		styles = append(styles, "vertical-align:"+f.Baseline)
	}
	switch f.Cap {
	case "all":
		styles = append(styles, "text-transform:uppercase")
	case "small":
		styles = append(styles, "font-variant:small-caps")
	case "none":
		styles = append(styles, "text-transform:none")
	}
	if f.Spc != "" {
		styles = append(styles, "letter-spacing:"+f.Spc)
	}
	if f.Highlight.Kind == "solid" {
		colorCSS("background-color", "data-highlight", f.Highlight.Color, hex, &styles, &attrs)
	}
	if f.Strike == "noStrike" {
		attrs = append(attrs, `data-strike="noStrike"`)
	}
	if f.Lang != plang {
		attrs = append(attrs, `lang="`+attrEsc(f.Lang)+`"`)
	}
	txt := xmlText(r.Text)
	if len(styles) > 0 || len(attrs) > 0 {
		w.WriteString("<span")
		if len(styles) > 0 {
			w.WriteString(` style="` + attrEsc(strings.Join(styles, ";")) + `"`)
		}
		for _, a := range attrs {
			w.WriteString(" " + a)
		}
		w.WriteString(">" + txt + "</span>")
	} else {
		w.WriteString(txt)
	}
	for i := len(closes) - 1; i >= 0; i-- {
		w.WriteString(closes[i])
	}
}

// fontCSS writes a typeface as a CSS font-family value: var(--mj-lt) for
// a theme font reference, else the quoted name.
func fontCSS(tf string) string {
	if strings.HasPrefix(tf, "+") {
		return "var(--" + tf[1:] + ")"
	}
	return "'" + cssStr(tf) + "'"
}

// canonical merges adjacent runs that show nothing but the same language
// and drops empty ones: HTML cannot show either boundary.
func canonical(runs []Run) []Run {
	var out []Run
	for _, r := range runs {
		if !r.Br && r.Fmt.bare() {
			if r.Text == "" {
				continue
			}
			if n := len(out); n > 0 && !out[n-1].Br && out[n-1].Fmt.bare() && out[n-1].Fmt.Lang == r.Fmt.Lang {
				out[n-1].Text += r.Text
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// decimal writes v / 10^scale exactly, without trailing zeros.
func decimal(v int64, scale int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	for len(s) <= scale {
		s = "0" + s
	}
	ip, fp := s[:len(s)-scale], strings.TrimRight(s[len(s)-scale:], "0")
	out := ip
	if fp != "" {
		out += "." + fp
	}
	if neg && out != "0" {
		out = "-" + out
	}
	return out
}

// undecimal reads a decimal with at most scale fraction digits as an
// integer count of 10^-scale.
func undecimal(s string, scale int) (int64, bool) {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	ip, fp, _ := strings.Cut(s, ".")
	if ip == "" && fp == "" || len(fp) > scale || strings.Contains(fp, ".") {
		return 0, false
	}
	for _, c := range ip + fp {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	fp += strings.Repeat("0", scale-len(fp))
	if len(ip)+len(fp) > 17 {
		return 0, false
	}
	v, err := strconv.ParseInt(ip+fp, 10, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// xmlText escapes s for XHTML text exactly as the read model does,
// replacing characters XML cannot carry with U+FFFD.
func xmlText(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c == '&':
			b.WriteString("&amp;")
		case c == '<':
			b.WriteString("&lt;")
		case c == '>':
			b.WriteString("&gt;")
		case !xmlChar(c):
			b.WriteRune('�')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// shownText is the text as the read model shows it, before escaping.
func shownText(s string) string {
	if strings.IndexFunc(s, func(c rune) bool { return !xmlChar(c) }) < 0 {
		return s
	}
	return strings.Map(func(c rune) rune {
		if !xmlChar(c) {
			return '�'
		}
		return c
	}, s)
}

// xmlChar is the read model's test of characters it shows as they are.
func xmlChar(c rune) bool {
	switch {
	case c == utf8.RuneError, c < 0x20 && c != '\t' && c != '\n' && c != '\r', c == 0xFFFE, c == 0xFFFF:
		return false
	}
	return true
}

// attrEsc escapes s for a double-quoted HTML attribute value.
func attrEsc(s string) string {
	return strings.ReplaceAll(xmlText(s), `"`, "&quot;")
}

// cssStr escapes s for use inside a quoted CSS string.
func cssStr(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`)
}
