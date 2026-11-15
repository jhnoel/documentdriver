package htmlx

import (
	"slices"
	"strings"
)

// The "in body" rules of the HTML dialect (see html5.go), including
// tables. Where the specification would repair the tree in a way that
// moves content, the builder fails with an UnsupportedError instead.

// unsupportedTags are HTML elements whose parsing rules are not ported.
// docxdriver's lowering rejects every one of them wherever it meets one.
var unsupportedTags = map[string]bool{
	"address": true, "applet": true, "area": true, "aside": true, "basefont": true, "bgsound": true,
	"big": true, "button": true, "caption": true, "center": true, "code": true, "col": true,
	"colgroup": true, "dd": true, "details": true, "dialog": true, "dir": true, "dl": true, "dt": true,
	"embed": true, "fieldset": true, "figcaption": true, "figure": true, "font": true, "form": true,
	"frame": true, "frameset": true, "hgroup": true, "hr": true, "iframe": true, "input": true,
	"keygen": true, "listing": true, "marquee": true, "math": true, "menu": true, "nav": true, "nobr": true,
	"noembed": true, "noframes": true, "noscript": true, "object": true, "optgroup": true,
	"option": true, "param": true, "plaintext": true, "pre": true, "rb": true, "rp": true, "rt": true,
	"rtc": true, "script": true, "search": true, "select": true, "small": true, "source": true,
	"summary": true, "svg": true, "template": true, "textarea": true, "track": true, "tt": true,
	"wbr": true, "xmp": true,
}

// special is the WHATWG "special" category restricted to the elements this
// builder creates; it decides where stray end tags stop.
func isSpecial(n *Node) bool {
	switch n.Tag {
	case "article", "base", "blockquote", "body", "br", "div", "footer", "h1", "h2", "h3", "h4",
		"h5", "h6", "head", "header", "html", "img", "li", "link", "main", "meta", "ol", "p",
		"section", "style", "table", "tbody", "td", "tfoot", "th", "thead", "title", "tr", "ul":
		return true
	}
	return false
}

// isScopeBoundary reports whether n is html, table, td, or th, a scope-stopping element.
func isScopeBoundary(n *Node) bool {
	switch n.Tag {
	case "html", "table", "td", "th":
		return true
	}
	return false
}

// isHeading reports whether name is h1 through h6.
func isHeading(name string) bool {
	return len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6'
}

// isTableSection reports whether name is table or one of its section and row elements.
func isTableSection(name string) bool {
	switch name {
	case "table", "tbody", "thead", "tfoot", "tr":
		return true
	}
	return false
}

// impliedEnd are the elements the specification closes implicitly before
// most end tags; of them the builder only ever opens p and li.
func impliedEnd(n *Node) bool { return (n.Tag == "p" || n.Tag == "li") }

// inScope finds the nearest open HTML element matching match, stopping at
// a scope boundary (and at ul/ol for list-item scope); it returns -1 when
// none is in scope.
func (b *h5) inScope(match func(string) bool, listItem bool) int {
	for i, n := range slices.Backward(b.stack) {

		if match(n.Tag) {
			return i
		}
		if isScopeBoundary(n) || (listItem && (n.Tag == "ul" || n.Tag == "ol")) {
			return -1
		}
	}
	return -1
}

// named returns a matcher reporting whether a tag equals name.
func named(name string) func(string) bool { return func(t string) bool { return t == name } }

// closeTo pops the stack down to and including index i. Implied-end
// elements (p, li) may always sit above i. For an end tag (why starts with
// "</") so may other inline elements that the specification closes without
// reopening (span, sup, ins and the like), as an unclosed inline at
// the end of its block loses nothing. Anything else fails: formatting
// elements the specification would reopen in the following content, and
// any element a start tag would close, which would move that tag's
// content out of an element the author left open around it.
func (b *h5) closeTo(i int, why string) {
	// Strict input closes only what HTML lets an author leave open.
	lenient := strings.HasPrefix(why, "</") && !b.strict
	for j := len(b.stack) - 1; j > i; j-- {
		n := b.stack[j]
		if !impliedEnd(n) && !(lenient && !isFormatting(n.Tag) && !isSpecial(n)) {
			b.fail(unsupportedStructure("%s while <%s> is still open", why, b.stack[j].Tag))
			return
		}
	}
	b.stack = b.stack[:i]
}

// closeP closes the open p element, if any, for the given start-tag reason.
func (b *h5) closeP(why string) {
	if i := b.inScope(named("p"), false); i >= 0 {
		b.closeTo(i, why)
	}
}

// inBody handles body tokens, delegating start and end tags and rejecting strict stray end tags.
func (b *h5) inBody(t token) {
	switch t.kind {
	case tokChar:
		b.insertBodyChar(t.r)
	case tokStartTag:
		b.startTag(t)
		if c := b.current(); t.selfClosing && b.selfClose[t.name] && c != nil && c.Tag == t.name {
			b.pop()
		}
	case tokEndTag:
		open := len(b.stack)
		b.endTag(t)
		// An end tag that closed nothing was ignored, or turned into an
		// element (</p>, </br>): strict input refuses both.
		if b.strict && !b.failed() && len(b.stack) >= open && t.name != "body" && t.name != "html" {
			b.fail(unsupportedStructure("</%s> has no open <%s> to close", t.name, t.name))
		}
	}
}

// startTag handles a body start tag, applying its element-specific insertion rules.
//
//nolint:gocyclo // one case per tag group.
func (b *h5) startTag(t token) {
	name := t.name
	if name == "image" {
		name = "img"
	}
	if unsupportedTags[name] {
		b.fail(unsupportedElement(name))
		return
	}
	cur := b.current()
	if isTableSection(cur.Tag) && !isTableStructure(name) && name != "html" && name != "body" && name != "head" {
		b.fail(unsupportedStructure("<%s> directly inside <%s> (docxdriver moves it before the table)", name, cur.Tag))
		return
	}
	switch {
	case name == "html":
		b.mergeAttrs(b.root, t.attrs)
	case name == "head":
	case name == "body":
		if len(b.stack) >= 2 && b.stack[1].Tag == "body" {
			b.mergeAttrs(b.stack[1], t.attrs)
		}
	case name == "meta" || name == "link" || name == "base":
		b.insertVoid(name, t.attrs)
	case name == "title":
		b.rawText(t, sRCData)
	case name == "style":
		b.rawText(t, sRawText)
	case name == "p" || name == "div" || name == "ul" || name == "ol" || name == "blockquote" ||
		name == "section" || name == "header" || name == "footer" || name == "main" || name == "article":
		b.closeP("<" + name + "> closes the open <p>")
		b.insert(name, t.attrs)
	case isHeading(name):
		b.closeP("<" + name + "> closes the open <p>")
		if isHeading(b.currentTag()) {
			b.pop()
		}
		b.insert(name, t.attrs)
	case name == "li":
		b.closeListItem()
		b.closeP("<li> closes the open <p>")
		b.insert(name, t.attrs)
	case name == "table":
		if !b.quirks {
			b.closeP("<table> closes the open <p>")
		}
		b.insert(name, t.attrs)
	case isTableStructure(name):
		b.tableStart(name, t.attrs)
	case name == "a":
		if b.openFormatting("a") >= 0 {
			b.fail(unsupportedStructure("<a> inside an open <a>"))
			return
		}
		b.insert(name, t.attrs)
	case name == "br" || name == "img":
		b.insertVoid(name, t.attrs)
	default:
		// Formatting and generic elements; a self-closing flag on a
		// non-void HTML element is ignored, as HTML does.
		b.insert(name, t.attrs)
	}
}

// isTableStructure reports whether name is tbody, thead, tfoot, tr, td, or th.
func isTableStructure(name string) bool {
	switch name {
	case "tbody", "thead", "tfoot", "tr", "td", "th":
		return true
	}
	return false
}

// isFormatting reports whether name is a, b, i, u, s, strike, strong, or em.
func isFormatting(name string) bool {
	switch name {
	case "a", "b", "i", "u", "s", "strike", "strong", "em":
		return true
	}
	return false
}

// openFormatting finds an open element named name above the nearest table
// cell (the specification's active formatting elements since the last
// marker); -1 when there is none.
func (b *h5) openFormatting(name string) int {
	for i, n := range slices.Backward(b.stack) {

		if n.Tag == "td" || n.Tag == "th" {
			return -1
		}
		if n.Tag == name {
			return i
		}
	}
	return -1
}

// closeListItem closes an open <li> before a new one, as the specification
// does, stopping at special elements other than div and p.
func (b *h5) closeListItem() {
	for i, n := range slices.Backward(b.stack) {

		if n.Tag == "li" {
			b.closeTo(i, "<li> closes the open <li>")
			return
		}
		if isSpecial(n) && n.Tag != "div" && n.Tag != "p" {
			return
		}
	}
}

// tableContext is the nearest table-structure element on the stack: the
// insertion mode the specification would be in.
func (b *h5) tableContext() (string, int) {
	for i, n := range slices.Backward(b.stack) {

		switch n.Tag {
		case "td", "th", "tr", "tbody", "thead", "tfoot", "table":
			return n.Tag, i
		case "html":
			return "", -1
		}
	}
	return "", -1
}

// closeCell closes the open cell; only implied-end elements may be open
// inside it.
func (b *h5) closeCell(why string) bool {
	i := b.inScope(func(t string) bool { return t == "td" || t == "th" }, false)
	if i < 0 {
		return false
	}
	b.closeTo(i, why)
	return !b.failed()
}

// tableStart handles a table-structure start tag, implying sections and rows as needed.
func (b *h5) tableStart(name string, attrs []Attr) {
	ctx, _ := b.tableContext()
	if ctx == "" {
		return // ignored outside a table, as in the specification
	}
	if ctx == "td" || ctx == "th" {
		if !b.closeCell("<" + name + "> closes the open cell") {
			return
		}
		ctx = "tr"
	}
	switch name {
	case "td", "th":
		switch ctx {
		case "table":
			b.insert("tbody", nil)
			b.insert("tr", nil)
		case "tbody", "thead", "tfoot":
			b.insert("tr", nil)
		}
		b.insert(name, attrs)
	case "tr":
		switch ctx {
		case "tr":
			b.pop()
		case "table":
			b.insert("tbody", nil)
		}
		b.insert(name, attrs)
	default: // tbody, thead, tfoot
		switch ctx {
		case "tr":
			b.pop()
			b.pop()
		case "tbody", "thead", "tfoot":
			b.pop()
		}
		b.insert(name, attrs)
	}
}

// endTag handles a body end tag, closing its element and inserting empty p or br where specified.
//
//nolint:gocyclo // one case per tag group.
func (b *h5) endTag(t token) {
	name := t.name
	switch {
	case name == "body" || name == "html":
		if b.inScope(named("body"), false) >= 0 {
			b.mode = hAfterBody
			if name == "html" {
				b.process(t)
			}
		}
	case name == "p":
		if b.inScope(named("p"), false) < 0 {
			if isTableSection(b.currentTag()) {
				b.fail(unsupportedStructure("</p> directly inside <%s>", b.currentTag()))
				return
			}
			b.insertVoid("p", nil)
			return
		}
		b.closeP("</p>")
	case name == "li":
		if i := b.inScope(named("li"), true); i >= 0 {
			b.closeTo(i, "</li>")
		}
	case isHeading(name):
		if i := b.inScope(isHeading, false); i >= 0 {
			b.closeTo(i, "</"+name+">")
		}
	case name == "div" || name == "ul" || name == "ol" || name == "blockquote" || name == "section" ||
		name == "header" || name == "footer" || name == "main" || name == "article":
		if i := b.inScope(named(name), false); i >= 0 {
			b.closeTo(i, "</"+name+">")
		}
	case name == "td" || name == "th":
		if i := b.inTableScope(name); i >= 0 {
			b.closeTo(i, "</"+name+">")
		}
	case name == "tr" || name == "tbody" || name == "thead" || name == "tfoot" || name == "table":
		b.tableEnd(name)
	case name == "br":
		if isTableSection(b.currentTag()) {
			b.fail(unsupportedStructure("</br> directly inside <%s>", b.currentTag()))
			return
		}
		b.insertVoid("br", nil)
	case isFormatting(name):
		if c := b.current(); c.Tag == name {
			b.pop()
			return
		}
		if b.openFormatting(name) >= 0 {
			b.fail(unsupportedStructure("</%s> does not close the innermost open element <%s>", name, b.currentTag()))
			return
		}
		b.anyOtherEnd(name)
	default:
		b.anyOtherEnd(name)
	}
}

// inTableScope finds an open element named name before any table or html.
func (b *h5) inTableScope(name string) int {
	for i, n := range slices.Backward(b.stack) {

		if n.Tag == name {
			return i
		}
		if n.Tag == "table" || n.Tag == "html" {
			return -1
		}
	}
	return -1
}

// tableEnd closes table-structure elements up to name, first closing any open cell.
func (b *h5) tableEnd(name string) {
	if b.inTableScope(name) < 0 {
		return
	}
	if ctx, _ := b.tableContext(); ctx == "td" || ctx == "th" {
		if !b.closeCell("</" + name + ">") {
			return
		}
	}
	for !b.failed() {
		c := b.currentTag()
		if c == name {
			b.pop()
			return
		}
		if c != "tr" && c != "tbody" && c != "thead" && c != "tfoot" {
			b.fail(unsupportedStructure("</%s> while <%s> is still open", name, c))
			return
		}
		b.pop()
	}
}

// anyOtherEnd closes the nearest open element named name unless a special
// element comes first, in which case the end tag is ignored.
func (b *h5) anyOtherEnd(name string) {
	for i, n := range slices.Backward(b.stack) {

		if n.Tag == name {
			b.closeTo(i, "</"+name+">")
			return
		}
		if isSpecial(n) {
			return
		}
	}
}
