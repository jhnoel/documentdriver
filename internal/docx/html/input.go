package html

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/htmlx"
)

// The HTML5 input path (docxdriver html/input.rs). docxdriver parses HTML5,
// lowers the supported subset into a private markup dialect, and re-parses
// that string (parse.rs). Here the lowering emits the dialect as a stream
// of structured events that the dialect state machine (parse.go) consumes
// directly; nothing is serialised except note bodies, whose canonical
// markup is data (Atom.Inner).

// LatexToOMML converts an equation's LaTeX to Office Math: an m:oMath, or
// for a block equation one m:oMathPara of m:oMath elements, with the m
// prefix declared on the root (the merge of consecutive block equations
// relies on that shape). math.LaTeXToOMML is the converter callers pass;
// tests may pass stubs. Equations are LaTeX only: docx drops 0.5.2's
// MathML input (an intended difference).
type LatexToOMML func(latex string, block bool) (omml string, err error)

var errNoEquations = errors.New("equation support is not configured")

// evKind is the kind of a dialect event.
type evKind uint8

const (
	evText evKind = iota
	evOpen
	evClose
)

// ev is one token of the dialect: text, an open tag (selfClosing for
// <br/>, <image/>, <comment-start/>, <section/>) or a close tag.
type ev struct {
	kind        evKind
	name        string
	attrs       []tagAttr
	selfClosing bool
	text        string
	omml        string // an equation's Office Math, carried to the atom
	ref         string // an equation's or placeholder's data-ref, carried to the atom
	content     string // a placeholder's text content, carried to the atom
}

// textEv returns a text event holding s.
func textEv(s string) ev { return ev{kind: evText, text: s} }

// openEv returns an open event for name with attributes attrs.
func openEv(name string, attrs []tagAttr) ev { return ev{kind: evOpen, name: name, attrs: attrs} }

// closeEv returns a close event for name.
func closeEv(name string) ev { return ev{kind: evClose, name: name} }

// emptyEv returns a self-closing open event for name with attributes attrs.
func emptyEv(name string, attrs []tagAttr) ev {
	return ev{kind: evOpen, name: name, attrs: attrs, selfClosing: true}
}

// valued returns a tag attribute named name with value value.
func valued(name, value string) tagAttr { return tagAttr{name: name, value: Some(value)} }

// emptyMarkers are the elements whose <name/> form is empty, accepted on
// input only. docxdriver rewrites them with a regular expression over the
// raw text before parsing, which also rewrites matching text inside
// attribute values and raw text; here the parser applies the rule to tags.
var emptyMarkers = map[string]bool{
	"image": true, "comment-start": true, "comment-end": true, "header": true, "footer": true,
	"section": true, "page-number": true, "page-count": true,
}

// parseHTML is the front of normalize: the document/fragment decision and
// the HTML5 parse.
func parseHTML(input string) (*htmlx.Node, error) {
	full := ascii.Lower(strings.TrimLeftFunc(input, unicode.IsSpace))
	document := strings.HasPrefix(full, "<!doctype") || strings.HasPrefix(full, "<html")
	root, err := htmlx.ParseHTML(input, htmlx.Options{Document: document, SelfClosing: emptyMarkers, Strict: true})
	return root, htmlError("invalid HTML: ", err)
}

// errMathML replaces the parser's rejection of <math>.
var errMathML = errors.New(`MathML is not supported: write an equation as <equation display="inline">LaTeX</equation> (or display="block")`)

// htmlError prefixes tokenizer errors as docxdriver does; unsupported
// constructs keep their own message, except <math>, which points at
// <equation>.
func htmlError(prefix string, err error) error {
	if err == nil {
		return nil
	}
	if syntax, ok := errors.AsType[*htmlx.SyntaxError](err); ok {
		return errors.New(prefix + syntax.Error())
	}
	if u, ok := errors.AsType[*htmlx.UnsupportedError](err); ok && u.Msg == "unsupported HTML element <math>" {
		return errMathML
	}
	return err
}

// lowerHTML parses and lowers input into dialect events (input.rs
// normalize); document enables sections, headers and footers.
func lowerHTML(input string, document bool, eq LatexToOMML) ([]ev, error) {
	root, err := parseHTML(input)
	if err != nil {
		return nil, err
	}
	l := &lower{document: document, eq: eq}
	return l.children(root, false)
}

type list struct {
	id, level int
	format    string
	start     uint32
}

type lower struct {
	nextList int
	lists    []list
	document bool
	eq       LatexToOMML
}

// hasAttr reports whether node n carries attribute name.
func hasAttr(n *htmlx.Node, name string) bool {
	_, ok := n.Attr(name)
	return ok
}

// attrs maps the attributes of n named in pairs (from, to), in pair order.
func attrs(n *htmlx.Node, pairs ...string) []tagAttr {
	var out []tagAttr
	for i := 0; i+1 < len(pairs); i += 2 {
		if v, ok := n.Attr(pairs[i]); ok {
			out = append(out, valued(pairs[i+1], v))
		}
	}
	return out
}

// children lowers every child of n into events.
func (l *lower) children(n *htmlx.Node, inline bool) ([]ev, error) {
	var out []ev
	for _, c := range n.Kids {
		evs, err := l.node(c, inline)
		if err != nil {
			return nil, err
		}
		out = append(out, evs...)
	}
	return out, nil
}

// oneOf reports whether s equals any element of set.
func oneOf(s string, set ...string) bool {
	return slices.Contains(set, s)
}

var tableFormattingAttrs = []string{
	"style", "class", "width", "height", "border", "cellpadding", "cellspacing", "align",
	"valign", "bgcolor", "rules", "frame", "background",
}

// checkAttrs refuses the attributes n may not carry. In document HTML
// (create) a table, row or cell may carry the style read writes for it,
// which the table lowering stores (build.TableCSS); elsewhere tables are
// formatted by their operations.
func checkAttrs(n *htmlx.Node, document bool) error {
	name := n.Tag
	tableStyle := document && oneOf(name, "table", "tr", "td", "th")
	// These containers have no formatting lowering: authored presentation
	// is rejected rather than dropped.
	if oneOf(name, "table", "tr", "td", "th", "tbody", "thead", "tfoot") {
		for _, a := range n.Attrs {
			if oneOf(a.Name, tableFormattingAttrs...) && (a.Name != "style" || !tableStyle) {
				return errorf("unsupported table formatting attribute %s on <%s>; set table formatting with format_table, format_row or format_cell", a.Name, name)
			}
		}
	}
	consumesStyle := isParagraphTag(name) || tableStyle ||
		(oneOf(name, "span", "strong", "b", "em", "i", "u", "s", "strike", "sup", "sub") &&
			!hasAttr(n, "data-field") && !hasAttr(n, "data-comment-start") &&
			!hasAttr(n, "data-comment-end") && !hasAttr(n, "data-format-change") &&
			!hasAttr(n, "pending"))
	if hasAttr(n, "style") && !consumesStyle {
		return errorf("unsupported HTML style attribute on <%s>; apply formatting to paragraphs or inline spans", name)
	}
	for _, a := range n.Attrs {
		if strings.HasPrefix(a.Name, "on") || a.Name == "srcdoc" || a.Name == "srcset" {
			return errorf("unsupported HTML attribute %s", a.Name)
		}
		if !knownAttrs[a.Name] && (name != "section" || !sectionAttrs[a.Name]) {
			if rest, old := strings.CutPrefix(a.Name, "data-docx-"); old {
				return errorf("unknown HTML attribute %s on <%s>: attributes have no docx prefix, write data-%s", a.Name, name, rest)
			}
			return errorf("unknown HTML attribute %s on <%s>", a.Name, name)
		}
	}
	return nil
}

// knownAttrs are the attributes HTML input may carry: the ones the
// lowering reads, and the ones read writes to describe the document
// (list markers, authors, revision ids, addresses, losses), which are
// accepted and ignored. Any other attribute is refused by name, never
// dropped (docs/contract.md, section 7).
var knownAttrs = set(
	// HTML's own, and the input's element attributes.
	"id", "style", "class", "href", "src", "alt", "width", "height", "w", "h", "type", "start", "colspan", "rowspan",
	"lang", "dir", "hidden", "display", "pending", "contenteditable", "xmlns", "instr", "author", "rid", "kind",
	"first-page", "first", "even", "default", "latex", "name", "title",
	// What the lowering reads.
	"data-comment-start", "data-comment-end", "data-field", "data-first-page", "data-format-change", "data-kind",
	"data-highlight", "data-break", "data-format", "data-style",
	// What read writes to describe the document; ignored on input.
	"data-ref", "data-ord", "data-revision", "data-author", "data-number", "data-marker", "data-list", "data-level",
	"data-tab", "data-loss", "data-table", "data-section", "data-paragraph", "data-notes", "data-note", "data-note-id",
	"data-note-body", "data-break-ins", "data-break-del", "data-href", "data-image-part", "data-image-rid",
	"data-image-width", "data-image-height", "data-image-alt", "data-image-unavailable",
)

// sectionAttrs are the section properties read writes on a <section>
// (view's sectionMeta): derived, accepted there and ignored, so that
// read's markup of a whole document is create's input unchanged.
var sectionAttrs = set(
	"data-page-width-twips", "data-page-height-twips", "data-orientation", "data-columns",
	"data-column-spacing-twips", "data-column-separator", "data-break-type",
	"data-page-number-format", "data-page-number-start",
)

// set returns a lookup set containing names.
func set(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// isParagraphTag reports whether name is p or a heading tag.
func isParagraphTag(name string) bool {
	return oneOf(name, "p", "h1", "h2", "h3", "h4", "h5", "h6")
}

// hasChildNamed reports whether n has an element child named in names.
func hasChildNamed(n *htmlx.Node, names ...string) bool {
	for _, c := range n.Kids {
		if c.Tag != "" && oneOf(c.Tag, names...) {
			return true
		}
	}
	return false
}

// wrap surrounds body with an open and close event.
func wrap(name string, a []tagAttr, body []ev) []ev {
	out := make([]ev, 0, len(body)+2)
	out = append(out, openEv(name, a))
	out = append(out, body...)
	return append(out, closeEv(name))
}

// node lowers node n into dialect events, dispatching on its element.
//
//nolint:gocyclo // one case per element, mirroring input.rs.
func (l *lower) node(n *htmlx.Node, inline bool) ([]ev, error) {
	if n.Tag == "" {
		t := n.Text
		if inline {
			t = strings.NewReplacer("\n", " ", "\r", " ").Replace(t)
		}
		return []ev{textEv(t)}, nil
	}
	if err := checkAttrs(n, l.document); err != nil {
		return nil, err
	}
	name := n.Tag
	if l.document && name == "span" && isPageField(n) {
		// read shows a page field as a placeholder (data-ref); a new
		// document has nothing to keep, so it is a new field, as
		// <page-number/> and <page-count/> are.
		return l.span(n, inline)
	}
	newImage := l.document && (name == "img" || name == "image")
	if ref, ok := n.Attr("data-ref"); ok && strings.Contains(ref, "opaque:") && !newImage || name == "opaque" {
		return placeholder(n, inline)
	}
	switch {
	case oneOf(name, "html", "body", "main", "article"):
		return l.children(n, inline)
	case name == "head":
		return nil, lowerHead(n)
	case name == "div" || name == "blockquote":
		if inline {
			return nil, errorf("<%s> is block content; use a paragraph operation", name)
		}
		if hasChildNamed(n, "p", "h1", "h2", "h3", "h4", "h5", "h6", "div", "table", "ul", "ol", "blockquote") {
			return l.children(n, false)
		}
		body, err := l.children(n, true)
		if err != nil {
			return nil, err
		}
		var a []tagAttr
		if name == "blockquote" {
			a = []tagAttr{valued("class", "Quote")}
		}
		return wrap("p", a, body), nil
	case name == "section":
		if inline || !l.document {
			return nil, errorf("sections require document HTML")
		}
		var a []tagAttr
		if v, ok := n.Attr("data-first-page"); hasAttr(n, "first-page") || (ok && v == "true") {
			a = []tagAttr{{name: "first-page"}}
		}
		body, err := l.children(n, false)
		return append([]ev{emptyEv("section", a)}, body...), err
	case name == "header" || name == "footer":
		return l.chrome(n, inline)
	case isParagraphTag(name):
		return l.paragraph(n, inline)
	case name == "ol" || name == "ul":
		return l.list(n, inline)
	case name == "li":
		return l.listItem(n, inline)
	case oneOf(name, "table", "tr", "td", "th"):
		if inline {
			return nil, errorf("tables are not inline content")
		}
		tag := name
		if name == "th" {
			tag = "td"
		}
		a := attrs(n, "colspan", "colspan", "rowspan", "rowspan", "data-format", "data-format", "style", "style")
		var body []ev
		var err error
		if (name == "td" || name == "th") && !hasChildNamed(n, "p", "h1", "h2", "h3", "h4", "h5", "h6", "table", "ul", "ol") {
			body, err = l.children(n, true)
			body = wrap("p", nil, body)
		} else {
			body, err = l.children(n, false)
		}
		if err != nil {
			return nil, err
		}
		return wrap(tag, a, body), nil
	case oneOf(name, "tbody", "thead", "tfoot"):
		return l.children(n, false)
	case oneOf(name, "strong", "b", "em", "i", "u", "s", "strike", "sup", "sub", "ins", "del"):
		return l.formatting(n, inline)
	case name == "mark":
		if !inline {
			return nil, errorf("<mark> requires a paragraph")
		}
		value, ok := n.Attr("data-highlight")
		if !ok {
			value = "yellow"
		}
		if _, err := ParseHighlightColor(value); err != nil {
			return nil, err
		}
		body, err := l.children(n, true)
		return wrap("mark", []tagAttr{valued("data-highlight", value)}, body), err
	case name == "span":
		return l.span(n, inline)
	case name == "a":
		href, ok := n.Attr("href")
		if !ok {
			return nil, errorf("<a> requires href")
		}
		if !SafeURL(href) {
			return nil, errorf("unsupported hyperlink URL scheme")
		}
		body, err := l.children(n, true)
		return wrap("a", []tagAttr{valued("href", href)}, body), err
	case name == "br":
		return []ev{emptyEv("br", attrs(n, "data-break", "type", "type", "type"))}, nil
	case name == "img" || name == "image":
		if !inline {
			return nil, errorf("images require a paragraph")
		}
		if src, _ := n.Attr("src"); l.document && !strings.HasPrefix(src, "data:") {
			// A new document has no images to point at: read's <img>
			// (its data-ref and data-image-* derived) is a new image
			// once its src is the image's data: URL.
			return nil, &ImageSourceError{Src: src}
		}
		return []ev{emptyEv("image", attrs(n, "src", "src", "width", "w", "height", "h", "w", "w", "h", "h", "alt", "alt"))}, nil
	case name == "equation":
		return l.equation(n)
	case name == "page-number" || name == "page-count":
		body, err := l.children(n, true)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(serialize(body)) != "" {
			return nil, errorf("page-number/page-count helpers must be empty")
		}
		instr := "NUMPAGES"
		if name == "page-number" {
			instr = "PAGE"
		}
		return wrap("field", []tagAttr{valued("instr", instr)}, []ev{textEv("1")}), nil
	case oneOf(name, "field", "format", "footnote", "endnote"):
		a := attrs(n, "instr", "instr", "pending", "pending", "id", "id", "author", "author")
		body, err := l.children(n, true)
		return wrap(name, a, body), err
	case name == "comment-start" || name == "comment-end":
		return []ev{emptyEv(name, attrs(n, "id", "id"))}, nil
	}
	return nil, errorf("unsupported HTML element <%s>", name)
}

// ImageSourceError is an image of document HTML whose src is not a
// data: URL: read's asset address (assets/iN.png), a URL, or none.
type ImageSourceError struct{ Src string }

// Error returns the message.
func (e *ImageSourceError) Error() string {
	if strings.HasPrefix(e.Src, "assets/") {
		return `<img src="` + e.Src + `"> is read's address of an image of the document it read; ` +
			`a new document builds an image from its bytes: put the asset's data_url from ` +
			"`docx read <file> --kind assets` in src"
	}
	return "an image's src is a data: URL (data:<mime>;base64,<bytes>); create fetches nothing"
}

// isPageField reports a span read writes for a PAGE or NUMPAGES field.
func isPageField(n *htmlx.Node) bool {
	f, ok := n.Attr("data-field")
	return ok && (f == "PAGE" || f == "NUMPAGES")
}

// lowerHead validates the document head children, allowing only meta, title and stylesheets.
func lowerHead(n *htmlx.Node) error {
	for _, c := range n.Kids {
		if c.Tag == "" {
			continue
		}
		if !oneOf(c.Tag, "meta", "title", "style") {
			return errorf("unsupported document head element <%s>", c.Tag)
		}
		if c.Tag == "style" && strings.TrimSpace(c.TextContent()) != strings.TrimSpace(CSS) {
			return errorf("authored stylesheets are unsupported; use inline document formatting")
		}
	}
	return nil
}

// chrome lowers a header or footer element, resolving its kind and body.
func (l *lower) chrome(n *htmlx.Node, inline bool) ([]ev, error) {
	if inline || !l.document {
		return nil, errorf("headers/footers require document HTML")
	}
	kind, ok := n.Attr("data-kind")
	if !ok {
		switch {
		case hasAttr(n, "first"):
			kind = "first"
		case hasAttr(n, "even"):
			kind = "even"
		default:
			kind = "default"
		}
	}
	var a []tagAttr
	switch kind {
	case "default":
	case "first", "even":
		a = []tagAttr{{name: kind}}
	default:
		return nil, errorf("unknown header/footer kind")
	}
	body, err := l.children(n, false)
	if err != nil {
		return nil, err
	}
	return wrap(n.Tag, a, body), nil
}

// listAttrs returns the tag attributes describing list li.
func listAttrs(li list) []tagAttr {
	return []tagAttr{
		valued("list-id", strconv.Itoa(li.id)),
		valued("list-level", strconv.Itoa(li.level)),
		valued("list-format", li.format),
		valued("list-start", strconv.FormatUint(uint64(li.start), 10)),
	}
}

var runCSSProperties = []string{
	"color", "font-family", "font-size", "font-weight", "font-style", "text-decoration", "vertical-align",
}

// paragraph lowers a paragraph element, appending list attributes and a trailing newline.
func (l *lower) paragraph(n *htmlx.Node, inline bool) ([]ev, error) {
	if inline {
		return nil, errorf("paragraph elements are not allowed in inline content")
	}
	a := attrs(n,
		"id", "id",
		"data-style", "class",
		"class", "class",
		"style", "style",
		"break-ins", "break-ins",
		"break-del", "break-del",
		"data-break-ins", "break-ins",
		"data-break-del", "break-del")
	if len(l.lists) > 0 {
		a = append(a, listAttrs(l.lists[len(l.lists)-1])...)
	}
	body, err := l.children(n, true)
	if err != nil {
		return nil, err
	}
	if css, ok := n.Attr("style"); ok {
		if _, err := ParagraphProperties(css); err != nil {
			return nil, err
		}
		decls, err := Declarations(css)
		if err != nil {
			return nil, err
		}
		var run strings.Builder
		for _, d := range decls {
			if oneOf(d.Name, runCSSProperties...) {
				run.WriteString(d.Name + ":" + d.Value + ";")
			}
		}
		if run.Len() > 0 {
			body = wrap("span", []tagAttr{valued("style", run.String())}, body)
		}
	}
	return append(wrap(n.Tag, a, body), textEv("\n")), nil
}

// list lowers an ordered or unordered list, assigning it a fresh list id.
func (l *lower) list(n *htmlx.Node, inline bool) ([]ev, error) {
	if inline {
		return nil, errorf("lists are not inline content")
	}
	l.nextList++
	startText, ok := n.Attr("start")
	if !ok {
		startText = "1"
	}
	start, ok := parseU32(startText)
	if !ok {
		return nil, errorf("list start must be a positive integer")
	}
	if start == 0 {
		return nil, errorf("list start must be positive")
	}
	format := "bullet"
	if n.Tag == "ol" {
		t, ok := n.Attr("type")
		if !ok {
			t = "1"
		}
		switch t {
		case "1":
			format = "decimal"
		case "a":
			format = "lowerLetter"
		case "A":
			format = "upperLetter"
		case "i":
			format = "lowerRoman"
		case "I":
			format = "upperRoman"
		default:
			return nil, errorf("unsupported ordered list type")
		}
	}
	l.lists = append(l.lists, list{id: l.nextList, level: len(l.lists), format: format, start: start})
	out, err := l.children(n, false)
	l.lists = l.lists[:len(l.lists)-1]
	return out, err
}

// listParagraph wraps text as a paragraph carrying the current list attributes.
func (l *lower) listParagraph(text []ev) []ev {
	return wrap("p", listAttrs(l.lists[len(l.lists)-1]), text)
}

// blank reports whether events serialize to only whitespace.
func blank(evs []ev) bool { return strings.TrimSpace(serialize(evs)) == "" }

// listItem lowers an li, wrapping loose text and nested lists into paragraphs.
func (l *lower) listItem(n *htmlx.Node, inline bool) ([]ev, error) {
	if inline || len(l.lists) == 0 {
		return nil, errorf("<li> requires an enclosing list")
	}
	var out, pending []ev
	for _, c := range n.Kids {
		if c.Tag != "" && oneOf(c.Tag, "p", "h1", "h2", "h3", "h4", "h5", "h6", "ol", "ul") {
			if !blank(pending) {
				out = append(out, l.listParagraph(pending)...)
			}
			pending = nil
			evs, err := l.node(c, false)
			if err != nil {
				return nil, err
			}
			out = append(out, evs...)
			continue
		}
		evs, err := l.node(c, true)
		if err != nil {
			return nil, err
		}
		pending = append(pending, evs...)
	}
	if !blank(pending) || len(out) == 0 {
		out = append(out, l.listParagraph(pending)...)
	}
	// Each additional paragraph of this list explicitly belongs to this
	// item; nested lists have their own ids and are left untouched.
	id := strconv.Itoa(l.lists[len(l.lists)-1].id)
	seen := false
	for i, e := range out {
		if e.kind != evOpen {
			continue
		}
		for j, a := range e.attrs {
			if a.name != "list-id" || a.value.V != id {
				continue
			}
			if seen {
				na := make([]tagAttr, 0, len(e.attrs)+1)
				na = append(na, e.attrs[:j+1]...)
				na = append(na, valued("list-continuation", "true"))
				out[i].attrs = append(na, e.attrs[j+1:]...)
			}
			seen = true
			break
		}
	}
	return out, nil
}

// formatting lowers inline formatting elements into their dialect tags.
func (l *lower) formatting(n *htmlx.Node, inline bool) ([]ev, error) {
	name := n.Tag
	if !inline {
		return nil, errorf("<%s> requires a paragraph", name)
	}
	tag := name
	switch name {
	case "strong":
		tag = "b"
	case "em":
		tag = "i"
	case "strike":
		tag = "s"
	}
	if tag != "ins" && tag != "del" && !hasAttr(n, "data-format-change") && !hasAttr(n, "pending") {
		var base string
		switch tag {
		case "b":
			base = "font-weight:bold"
		case "i":
			base = "font-style:italic"
		case "u":
			base = "text-decoration:underline"
		case "s":
			base = "text-decoration:line-through"
		case "sup":
			base = "vertical-align:super"
		default:
			base = "vertical-align:sub"
		}
		style, _ := n.Attr("style") // absent style is ""
		// Nested native formatting is legal HTML; a stack of spans keeps
		// the inherited format instead of toggling it off.
		body, err := l.children(n, true)
		return wrap("span", []tagAttr{valued("style", base+";"+style)}, body), err
	}
	a := attrs(n,
		"data-revision", "id",
		"data-author", "author",
		"data-format-change", "pending",
		"id", "id",
		"author", "author",
		"pending", "pending")
	body, err := l.children(n, true)
	return wrap(tag, a, body), err
}

// span lowers a span into comment, field or styled inline events.
func (l *lower) span(n *htmlx.Node, inline bool) ([]ev, error) {
	if !inline {
		return nil, errorf("<span> requires a paragraph")
	}
	if id, ok := n.Attr("data-comment-start"); ok {
		return []ev{emptyEv("comment-start", []tagAttr{valued("id", id)})}, nil
	}
	if id, ok := n.Attr("data-comment-end"); ok {
		return []ev{emptyEv("comment-end", []tagAttr{valued("id", id)})}, nil
	}
	if instr, ok := n.Attr("data-field"); ok {
		body, err := l.children(n, true)
		return wrap("field", []tagAttr{valued("instr", instr)}, body), err
	}
	a := attrs(n,
		"style", "style",
		"data-format-change", "pending",
		"data-revision", "id",
		"data-author", "author")
	body, err := l.children(n, true)
	return wrap("span", a, body), err
}

// equation lowers <equation display="inline|block">LaTeX</equation>, the
// one equation form docx accepts. Its content is HTML text, so character
// references are decoded (write a less-than sign as &lt; or \lt) and a raw
// < starts a tag, which is an error. The LaTeX is converted here, so a bad
// equation fails the input as a whole.
func (l *lower) equation(n *htmlx.Node) ([]ev, error) {
	if _, ok := n.Attr("latex"); ok {
		return nil, errorf("<equation> takes its LaTeX as text content, not a latex attribute")
	}
	display, ok := n.Attr("display")
	if !ok || (display != "inline" && display != "block") {
		return nil, errorf(`<equation> requires display="inline" or display="block"`)
	}
	var latex strings.Builder
	for _, c := range n.Kids {
		if c.Tag != "" {
			return nil, errorf("<equation> holds LaTeX text only; found <%s> (write < as &lt;)", c.Tag)
		}
		latex.WriteString(c.Text)
	}
	if strings.TrimSpace(latex.String()) == "" {
		return nil, errorf("<equation> is empty")
	}
	if l.eq == nil {
		return nil, errNoEquations
	}
	omml, err := l.eq(latex.String(), display == "block")
	if err != nil {
		return nil, errorf("invalid LaTeX equation: %s", err.Error())
	}
	open := openEv("equation", []tagAttr{valued("display", display)})
	open.omml = omml
	open.ref, _ = n.Attr("data-ref")
	return []ev{open, textEv(latex.String()), closeEv("equation")}, nil
}

// opaqueRef matches a placeholder's address: opaque:N, after a
// paragraph address and a slash where the paragraph has one.
var opaqueRef = regexp.MustCompile(`^([0-9A-Fa-f]{8}(~[1-9][0-9]*)?/)?opaque:[1-9][0-9]{0,8}$`)

// placeholder lowers an element that carries a placeholder's address
// (docs/contract.md, section 7): <opaque data-ref data-kind>, or the
// rendering read gives the content (an <img>, a note reference's <sup>,
// a field's or content control's <span>) with its data-ref. It stands
// for that existing content, wherever the HTML puts it; its other
// attributes are derived, and its text must be the content's as read
// shows it (it is not editable here).
func placeholder(n *htmlx.Node, inline bool) ([]ev, error) {
	ref, ok := n.Attr("data-ref")
	if !ok {
		return nil, errorf("<opaque> stands for existing content and needs the data-ref read gave it; new content cannot be written as a placeholder")
	}
	if !opaqueRef.MatchString(ref) {
		return nil, errorf("malformed placeholder address data-ref=\"%s\" (expected 0A1B2C3D/opaque:N)", ref)
	}
	if !inline {
		return nil, errorf("a placeholder (data-ref=\"%s\") requires a paragraph", ref)
	}
	kind, _ := n.Attr("data-kind")
	e := emptyEv("opaque", []tagAttr{valued("data-ref", ref), valued("data-kind", kind)})
	e.ref = strings.ToUpper(ref[:strings.LastIndex(ref, "opaque:")]) + ref[strings.LastIndex(ref, "opaque:"):]
	e.content = n.TextContent()
	return []ev{e}, nil
}

// SafeURL reports whether a hyperlink target is relative or uses the
// https, http, mailto or tel scheme, and has no control characters.
func SafeURL(value string) bool {
	value = ascii.Lower(strings.TrimSpace(value))
	if strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	if !strings.Contains(value, ":") {
		return true
	}
	for _, s := range []string{"https:", "http:", "mailto:", "tel:"} {
		if strings.HasPrefix(value, s) {
			return true
		}
	}
	return false
}

// serialize writes events as dialect markup, escaped as docxdriver's
// lowering writes it. Note bodies (Atom.Inner) keep this form.
func serialize(evs []ev) string {
	var b strings.Builder
	for _, e := range evs {
		switch e.kind {
		case evText:
			b.WriteString(EscapeText(e.text))
		case evClose:
			b.WriteString("</" + e.name + ">")
		case evOpen:
			b.WriteString("<" + e.name)
			for _, a := range e.attrs {
				b.WriteString(" " + a.name)
				if a.value.OK {
					b.WriteString(`="` + EscapeAttr(a.value.V) + `"`)
				}
			}
			if e.selfClosing {
				b.WriteString("/")
			}
			b.WriteString(">")
		}
	}
	return b.String()
}
