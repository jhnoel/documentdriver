package view

import (
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// CSS is the preview stylesheet docxdriver 0.5.2 ships with the HTML
// projection (html/styles.rs CSS), scoped to a .docxdriver container.
const CSS = `.docxdriver { font-family: Calibri, Arial, sans-serif; line-height: 1.2; color: #111; }
.docxdriver p, .docxdriver h1, .docxdriver h2, .docxdriver h3, .docxdriver h4, .docxdriver h5, .docxdriver h6 { white-space: pre-wrap; tab-size: 4; min-height: 1em; }
.docxdriver p { margin: 0 0 .6em; }
.docxdriver table { border-collapse: collapse; max-width: 100%; }
.docxdriver td, .docxdriver th { border: 1px solid #aaa; padding: .3em .5em; vertical-align: top; }
.docxdriver img { max-width: 100%; }
.docxdriver ins { color: #067a35; text-decoration: underline; }
.docxdriver del { color: #b32323; text-decoration: line-through; }
.docxdriver [data-format-change] { background: #fff1b8; }
.docxdriver [data-field] { background: #f2f4f7; }
.docxdriver ol, .docxdriver ul { padding-left: 2em; }
.docxdriver li > p { margin-bottom: .25em; }
.docxdriver [data-marker]::marker { content: attr(data-marker) " "; }
.docxdriver aside { border-top: 1px solid #ddd; margin-top: 1em; font-size: .9em; }
.docxdriver [data-break="page"] { break-after: page; }
.docxdriver [data-break="column"] { break-after: column; }
.docxdriver header, .docxdriver footer { color: #555; }
`

// vertAlign is a run's w:vertAlign as the projection shows it.
type vertAlign uint8

const (
	baseline vertAlign = iota
	superscript
	subscript
)

// tag returns the HTML tag for a vertical alignment, sup or sub.
func (v vertAlign) tag() string {
	if v == superscript {
		return "sup"
	}
	return "sub"
}

// The reset bits of fmtState.resets: an explicitly disabled property.
const (
	resetBold      = 1
	resetItalic    = 2
	resetUnderline = 4
	resetStrike    = 8
	resetBaseline  = 16
)

// fmtState is a character's formatting as the projection renders it
// (docxdriver's html::Fmt). It is comparable.
type fmtState struct {
	resets                          uint8
	bold, italic, underline, strike bool
	vert                            vertAlign
	highlight                       string // palette name, "" for none
	color                           uint32
	hasColor                        bool
	size                            uint32 // half points
	hasSize                         bool
	font                            string
	hasFont                         bool
}

// highlights is the ST_HighlightColor palette and the CSS color reads write
// for each entry, in docxdriver's order.
var highlights = []struct{ name, css string }{
	{"black", "#000000"}, {"blue", "#0000FF"}, {"cyan", "#00FFFF"}, {"green", "#00FF00"},
	{"magenta", "#FF00FF"}, {"red", "#FF0000"}, {"yellow", "#FFFF00"}, {"white", "#FFFFFF"},
	{"darkBlue", "#000080"}, {"darkCyan", "#008080"}, {"darkGreen", "#008000"},
	{"darkMagenta", "#800080"}, {"darkRed", "#800000"}, {"darkYellow", "#808000"},
	{"darkGray", "#808080"}, {"lightGray", "#C0C0C0"}, {"none", "transparent"},
}

// highlightCSS returns the CSS color for highlight palette name, or empty when unknown.
func highlightCSS(name string) string {
	for _, h := range highlights {
		if h.name == name {
			return h.css
		}
	}
	return ""
}

// isHighlight reports whether name is a known highlight palette name.
func isHighlight(name string) bool { return highlightCSS(name) != "" }

// formatFloat writes f as Rust's f64 Display does: the shortest digits
// that read back as f, never in exponent form.
func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// rustTrim trims Unicode white space, as Rust's str::trim.
func rustTrim(s string) string { return strings.TrimFunc(s, unicode.IsSpace) }

// isControl is Rust's char::is_control (general category Cc).
func isControl(r rune) bool { return unicode.Is(unicode.Cc, r) }

// parseRustF64 parses s as Rust's str::parse::<f64> does: decimal or
// exponent notation with an optional sign, or inf, infinity and nan in any
// case. Go's hexadecimal and underscore forms are refused.
func parseRustF64(s string) (float64, bool) {
	if strings.ContainsAny(s, "_xXpP") {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) {
			return f, true // Rust saturates to ±inf or rounds to 0
		}
		return 0, false
	}
	return f, true
}

// parseFontFamily validates a font family name: quotes trimmed, at most
// 127 bytes, no control characters or CSS/markup delimiters.
func parseFontFamily(v string) (string, bool) {
	v = strings.Trim(rustTrim(v), `'"`)
	if len(v) > 127 || strings.ContainsFunc(v, func(r rune) bool {
		return isControl(r) || strings.ContainsRune(";<>{}\\", r)
	}) {
		return "", false
	}
	return v, true
}

// inlineCSS is the inline style a run with f carries beyond its tags.
func inlineCSS(f fmtState) string {
	var b strings.Builder
	if f.resets&resetBold != 0 && !f.bold {
		b.WriteString("font-weight:normal;")
	}
	if f.resets&resetItalic != 0 && !f.italic {
		b.WriteString("font-style:normal;")
	}
	if f.resets&(resetUnderline|resetStrike) != 0 && !f.underline && !f.strike {
		b.WriteString("text-decoration:none;")
	}
	if f.resets&resetBaseline != 0 && f.vert == baseline {
		b.WriteString("vertical-align:baseline;")
	}
	if f.highlight != "" {
		b.WriteString("background-color:" + highlightCSS(f.highlight) + ";")
	}
	if f.hasColor {
		b.WriteString("color:#" + hex6(f.color) + ";")
	}
	if f.hasSize {
		b.WriteString("font-size:" + formatFloat(float64(f.size)/2) + "pt;")
	}
	if f.hasFont {
		b.WriteString("font-family:" + f.font + ";")
	}
	return b.String()
}

// hex6 formats v as at least six uppercase hex digits.
func hex6(v uint32) string {
	s := strings.ToUpper(strconv.FormatUint(uint64(v), 16))
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}

// withoutInheritedCSS drops the run properties the paragraph already gives
// its content; a font size stays on super- and subscript text.
func withoutInheritedCSS(f fmtState, in inheritFmt) fmtState {
	if f.hasColor && in.hasColor && in.color == f.color {
		f.color, f.hasColor = 0, false
	}
	if f.hasSize && in.hasSize && f.vert == baseline && in.size == f.size {
		f.size, f.hasSize = 0, false
	}
	if f.hasFont && in.hasFont && in.font == f.font {
		f.font, f.hasFont = "", false
	}
	if f.resets&resetBold != 0 && !f.bold && in.weightNormal {
		f.resets &^= resetBold
	}
	if f.resets&resetItalic != 0 && !f.italic && in.styleNormal {
		f.resets &^= resetItalic
	}
	return f
}

// cssString writes a declaration map in key order.
func cssString(m map[string]string) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(m)) {
		b.WriteString(k + ":" + m[k] + ";")
	}
	return b.String()
}

// Styles is the styles part's cascade: document defaults, style chains by
// w:basedOn, and the CSS each yields (docxdriver's styles::Styles).
type Styles struct {
	x     *model.XML
	nodes map[string]*tree.Node
	// def is the default paragraph style, if any.
	def    string
	hasDef bool
	// Document defaults: the w:pPrDefault/w:pPr and w:rPrDefault/w:rPr.
	pDefault, rDefault *tree.Node
	chains             map[string][]*tree.Node
	runFmt             map[runKey]fmtState
	// fmtMemo is runFormat by value: the formatting of a run is fixed by
	// its paragraph's style, its character style and the bytes of its
	// property bag (fmtKey), and most runs of a document share a few.
	fmtMemo map[fmtKey]map[string]fmtState
	// catalog are the resolved style declarations (catalogDecls).
	catalog map[string]catalogEntry
}

type runKey struct {
	run      *tree.Node
	original bool
}

// fmtKey is what runFormat reads besides the bytes of the run's w:rPr:
// the view, the paragraph style (and whether the run is in a paragraph
// with one) and the character style. Everything else runFormat reads
// from the w:rPr goes by local names and attribute values, which its
// bytes fix; only the w:rStyle lookup is namespace-aware, so the
// character style is in the key itself.
type fmtKey struct {
	original, inPara, hasPStyle, hasRPr, hasRStyle bool
	pStyle, rStyle                                 string
}

// LoadStyles reads the styles part beside the main document part. A part
// that is absent or not well formed gives an empty cascade.
func LoadStyles(pkg *model.Package) *Styles {
	s := &Styles{
		nodes: map[string]*tree.Node{}, chains: map[string][]*tree.Node{}, runFmt: map[runKey]fmtState{},
		fmtMemo: map[fmtKey]map[string]fmtState{}, catalog: map[string]catalogEntry{},
	}
	x, ok := pkg.Part(pkg.StylesPartName())
	if !ok {
		return s
	}
	s.x = x
	x.Doc.Node().Walk(func(n *tree.Node) bool {
		switch {
		case model.IsW(n, "style"):
			if id, ok := model.Attr(n, "styleId"); ok {
				s.nodes[id] = n
				if !s.hasDef {
					t, _ := model.Attr(n, "type")
					d, _ := model.Attr(n, "default")
					if t == "paragraph" && (d == "1" || d == "true") {
						s.def, s.hasDef = id, true
					}
				}
			}
		case model.IsW(n, "pPrDefault"):
			if s.pDefault == nil {
				s.pDefault = n
			}
		case model.IsW(n, "rPrDefault"):
			if s.rDefault == nil {
				s.rDefault = n
			}
		}
		return true
	})
	return s
}

// DefaultParagraphStyle is the paragraph style marked w:default, which
// unstyled paragraphs use.
func (s *Styles) DefaultParagraphStyle() (string, bool) { return s.def, s.hasDef }

// paragraphStyle is the paragraph's style id, or the default style.
func (s *Styles) paragraphStyle(p *tree.Node, original bool) (string, bool) {
	if ppr := model.PropertyBag(p, "pPr", original); ppr != nil {
		if ps := model.ChildW(ppr, "pStyle"); ps != nil {
			if v, ok := model.Attr(ps, "val"); ok {
				return v, true
			}
		}
	}
	return s.def, s.hasDef
}

// chain returns the style elements of id's w:basedOn chain, base first.
func (s *Styles) chain(id string) []*tree.Node {
	if c, ok := s.chains[id]; ok {
		return c
	}
	var ids []*tree.Node
	seen := map[string]bool{}
	cur, ok := id, true
	for ok && !seen[cur] {
		seen[cur] = true
		n := s.nodes[cur]
		if n == nil {
			break
		}
		ids = append(ids, n)
		cur, ok = "", false
		if b := model.ChildW(n, "basedOn"); b != nil {
			cur, ok = model.Attr(b, "val")
		}
	}
	slices.Reverse(ids)
	s.chains[id] = ids
	return ids
}

// applyChain adds style id's chain CSS to m, paragraph or run as run selects.
func (s *Styles) applyChain(id string, has, run bool, m map[string]string) {
	if s.x == nil || !has {
		return
	}
	name := "pPr"
	if run {
		name = "rPr"
	}
	for _, n := range s.chain(id) {
		if p := model.ChildW(n, name); p != nil {
			collectCSS(p, run, m)
		}
	}
}

// defaults adds document default paragraph or run CSS to m.
func (s *Styles) defaults(run bool, m map[string]string) {
	d, name := s.pDefault, "pPr"
	if run {
		d, name = s.rDefault, "rPr"
	}
	if d == nil {
		return
	}
	if p := model.ChildW(d, name); p != nil {
		collectCSS(p, run, m)
	}
}

// paragraphDecls is the paragraph's resolved paragraph declarations.
func (s *Styles) paragraphDecls(p *tree.Node, original bool) map[string]string {
	m := map[string]string{}
	s.defaults(false, m)
	id, has := s.paragraphStyle(p, original)
	s.applyChain(id, has, false, m)
	if ppr := model.PropertyBag(p, "pPr", original); ppr != nil {
		collectCSS(ppr, false, m)
	}
	return m
}

// Catalog is the resolved CSS of every style, keyed by style id.
func (s *Styles) Catalog() map[string]string {
	out := map[string]string{}
	for id := range s.nodes {
		_, out[id], _ = s.catalogDecls(id)
	}
	return out
}

// runFormat is the run's resolved formatting: document defaults, its
// paragraph's style, its character style and its direct properties. CSS
// the projection cannot carry gives no formatting at all, as in docxdriver.
func (s *Styles) runFormat(run *tree.Node, original bool) fmtState {
	k := runKey{run, original}
	if f, ok := s.runFmt[k]; ok {
		return f
	}
	key := fmtKey{original: original}
	for p := run.Parent(); p != nil; p = p.Parent() {
		if model.IsW(p, "p") {
			key.inPara = true
			key.pStyle, key.hasPStyle = s.paragraphStyle(p, original)
			break
		}
	}
	rpr := model.PropertyBag(run, "rPr", original)
	var raw []byte
	if rpr != nil {
		key.hasRPr = true
		raw = rpr.Bytes()
		if rs := model.ChildW(rpr, "rStyle"); rs != nil {
			key.rStyle, key.hasRStyle = model.Attr(rs, "val")
		}
	}
	byRaw := s.fmtMemo[key]
	if f, ok := byRaw[string(raw)]; ok {
		s.runFmt[k] = f
		return f
	}
	f := s.resolveRunFormat(key, rpr)
	if byRaw == nil {
		byRaw = map[string]fmtState{}
		s.fmtMemo[key] = byRaw
	}
	byRaw[string(raw)] = f
	s.runFmt[k] = f
	return f
}

// resolveRunFormat computes runFormat for a run with key and property
// bag rpr (nil when it has none): document defaults, the paragraph's
// style, the character style and the direct properties, in that order.
func (s *Styles) resolveRunFormat(key fmtKey, rpr *tree.Node) fmtState {
	var r runProps
	if s.rDefault != nil {
		if p := model.ChildW(s.rDefault, "rPr"); p != nil {
			collectRunProps(p, &r)
		}
	}
	if key.inPara {
		s.runChain(key.pStyle, key.hasPStyle, &r)
	}
	if rpr != nil {
		if key.hasRStyle {
			s.runChain(key.rStyle, true, &r)
		}
		collectRunProps(rpr, &r)
	}
	return r.format()
}

// runChain adds the run properties of style id's chain to r, base first.
func (s *Styles) runChain(id string, has bool, r *runProps) {
	if s.x == nil || !has {
		return
	}
	for _, n := range s.chain(id) {
		if p := model.ChildW(n, "rPr"); p != nil {
			collectRunProps(p, r)
		}
	}
}

var offValues = map[string]bool{"0": true, "false": true, "off": true}

// isHexByte reports whether c is a hexadecimal digit.
func isHexByte(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// allHex reports whether every byte of v is a hexadecimal digit.
func allHex(v string) bool {
	for i := range len(v) {
		if !isHexByte(v[i]) {
			return false
		}
	}
	return true
}

// localName returns n's local name, or empty for a non-element.
func localName(n *tree.Node) string {
	if !model.IsElement(n) {
		return ""
	}
	return n.Local()
}

// collectCSS adds the CSS of property container p (a w:rPr when run, else
// a w:pPr) to m.
func collectCSS(p *tree.Node, run bool, m map[string]string) {
	for n := range p.ChildrenSeq() {
		name := localName(n)
		val := model.AttrOr(n, "val")
		if run {
			collectRunCSS(n, name, val, m)
		} else {
			collectParagraphCSS(n, name, val, m)
		}
	}
}

// collectRunCSS adds run property n's CSS for name and val to m.
func collectRunCSS(n *tree.Node, name, val string, m map[string]string) {
	switch name {
	case "highlight":
		if isHighlight(val) {
			m["--docx-highlight"] = val
		}
	case "b":
		m["font-weight"] = "bold"
		if offValues[val] {
			m["font-weight"] = "normal"
		}
	case "i":
		m["font-style"] = "italic"
		if offValues[val] {
			m["font-style"] = "normal"
		}
	case "u", "strike":
		token := "line-through"
		if name == "u" {
			token = "underline"
		}
		var deco []string
		for _, v := range strings.FieldsFunc(m["text-decoration"], unicode.IsSpace) {
			if v != token && v != "none" {
				deco = append(deco, v)
			}
		}
		if val != "0" && val != "none" && val != "false" && val != "off" {
			deco = append(deco, token)
		}
		if len(deco) == 0 {
			m["text-decoration"] = "none"
		} else {
			m["text-decoration"] = strings.Join(deco, " ")
		}
	case "vertAlign":
		switch val {
		case "superscript":
			m["vertical-align"] = "super"
		case "subscript":
			m["vertical-align"] = "sub"
		default:
			m["vertical-align"] = "baseline"
		}
	case "color":
		if len(val) == 6 && allHex(val) {
			m["color"] = "#" + val
		}
	case "sz":
		if v, ok := model.ParseU32(val); ok {
			m["font-size"] = formatFloat(float64(v)/2) + "pt"
		}
	case "rFonts":
		v, ok := model.Attr(n, "ascii")
		if !ok {
			v, ok = model.Attr(n, "hAnsi")
		}
		if ok {
			if _, valid := parseFontFamily(v); valid {
				m["font-family"] = v
			}
		}
	}
}

// collectParagraphCSS adds paragraph property n's CSS for name and val to m.
func collectParagraphCSS(n *tree.Node, name, val string, m map[string]string) {
	switch name {
	case "jc":
		switch val {
		case "left", "right", "center":
			m["text-align"] = val
		case "both":
			m["text-align"] = "justify"
		}
	case "spacing":
		for _, a := range [][2]string{{"before", "margin-top"}, {"after", "margin-bottom"}} {
			if v, ok := model.ParseI64(model.AttrOr(n, a[0])); ok {
				m[a[1]] = formatFloat(float64(v)/20) + "pt"
			}
		}
		if v, ok := model.ParseU32(model.AttrOr(n, "line")); ok {
			rule, has := model.Attr(n, "lineRule")
			if !has {
				rule = "auto"
			}
			if rule == "auto" {
				m["line-height"] = formatFloat(float64(v) / 240)
			} else {
				m["line-height"] = formatFloat(float64(v)/20) + "pt"
			}
		}
	case "ind":
		for _, a := range [][2]string{{"left", "margin-left"}, {"right", "margin-right"}, {"firstLine", "text-indent"}} {
			if v, ok := model.ParseI64(model.AttrOr(n, a[0])); ok {
				m[a[1]] = formatFloat(float64(v)/20) + "pt"
			}
		}
		if v, ok := model.ParseU32(model.AttrOr(n, "hanging")); ok {
			m["text-indent"] = "-" + formatFloat(float64(v)/20) + "pt"
		}
	}
}
