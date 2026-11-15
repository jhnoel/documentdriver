package html

import (
	"errors"
	"strings"
	"unicode"
)

// The dialect state machine (docxdriver html/parse.rs parse_dialect), fed
// the lowering's events instead of re-lexed markup. Its errors name the
// rule, not just the position, and are kept verbatim.

type tagAttr struct {
	name  string
	value Opt[string] // absent for a bare attribute
}

type tag struct {
	name        string
	attrs       []tagAttr
	closing     bool
	selfClosing bool
	omml        string
	ref         string
	content     string
}

// get returns the attribute named name: absent, bare (present, value
// absent) or valued.
func (t *tag) get(name string) Opt[Opt[string]] {
	for _, a := range t.attrs {
		if a.name == name {
			return Some(a.value)
		}
	}
	return Opt[Opt[string]]{}
}

// value is attr(..).and_then(|v| v.clone()): the value, absent when the
// attribute is absent or bare.
func (t *tag) value(name string) Opt[string] {
	if a := t.get(name); a.OK {
		return a.V
	}
	return Opt[string]{}
}

// nonEmpty is value(name) with an empty value treated as absent.
func (t *tag) nonEmpty(name string) Opt[string] {
	if v := t.value(name); v.OK && v.V != "" {
		return v
	}
	return Opt[string]{}
}

// marker reads a marker attribute (break-ins, or id on ins/del): absent is
// not wrapped, bare or empty a new wrapper, a value an existing one.
func (t *tag) marker(name string) WrapID {
	a := t.get(name)
	switch {
	case !a.OK:
		return WrapID{}
	case !a.V.OK || a.V.V == "":
		return Some(Opt[string]{})
	}
	return Some(Some(a.V.V))
}

// isSpace reports whether r is whitespace.
func isSpace(r rune) bool { return unicode.IsSpace(r) }

// preview cuts text to its first 40 characters, marking the cut with "…".
func preview(text string) string {
	n := 0
	for i := range text {
		if n == 40 {
			return text[:i] + "…"
		}
		n++
	}
	return text
}

// ParseFragment parses an HTML fragment and lowers it into items
// (docxdriver parse_fragment: normalize, then parse_dialect). Fragments
// carry no sections, headers or footers.
func ParseFragment(input string, eq LatexToOMML) ([]Item, error) {
	evs, err := lowerHTML(input, false, eq)
	if err != nil {
		return nil, err
	}
	return parseEvents(evs)
}

// dialectState is the open-element state of ParseDialect.
type dialectState struct {
	items         []Item
	inPara        bool
	paraStart     int
	fmt           Fmt
	pendingB      Opt[FmtPending]
	pendingI      Opt[FmtPending]
	pendingU      Opt[FmtPending]
	pendingS      Opt[FmtPending]
	pendingVert   Opt[FmtPending]
	pendingFormat Opt[FmtPending]
	ins, del      WrapID
	link, field   Opt[string]
	spans         []spanState
	// blockEquations: a block equation may stand outside a paragraph,
	// as a paragraph of its own (parseEventsIn).
	blockEquations bool
}

type spanState struct {
	fmt     Fmt
	pending Opt[FmtPending]
}

const newlineInParagraph = "raw newline inside a paragraph: paragraph content is a single line. " +
	"For a NEW PARAGRAPH, close and reopen the paragraph element — replace " +
	"<p>whole paragraph</p> with <p break-ins>first half</p> + newline + <p>second half</p> " +
	"(the bare break-ins records the break as a tracked split). " +
	"For a line break within the paragraph, use <br/>."

// firstPending returns the first present pending format, or empty when none.
func firstPending(ps ...Opt[FmtPending]) Opt[FmtPending] {
	for _, p := range ps {
		if p.OK {
			return p
		}
	}
	return Opt[FmtPending]{}
}

// text appends text atoms, refusing text outside paragraphs or with pending formatting.
func (s *dialectState) text(text string) error {
	if !s.inPara {
		if strings.TrimFunc(text, isSpace) != "" {
			return errorf("text outside a paragraph element: %s — content lives inside <p>/<h1..6>", rustDebugString(preview(text)))
		}
		return nil
	}
	if s.pendingFormat.OK && strings.ContainsRune(text, '\t') {
		return errors.New("<format pending> cannot wrap tabs — only character content is supported")
	}
	if strings.ContainsAny(text, "\n\r") {
		return errors.New(newlineInParagraph) //nolint:staticcheck // ST1005: docxdriver's message, verbatim
	}
	pending := firstPending(s.pendingFormat, s.pendingB, s.pendingI, s.pendingU, s.pendingS, s.pendingVert)
	for text != "" {
		chars, rest, tab := strings.Cut(text, "\t")
		if chars != "" {
			s.items = append(s.items, Item{Kind: ItemAtom, Atom: Atom{
				Kind: AtomChar, Text: chars, Fmt: s.fmt, FmtPending: pending,
				Ins: s.ins, Del: s.del, Link: s.link, Field: s.field,
			}})
		}
		if tab {
			s.items = append(s.items, Item{Kind: ItemAtom, Atom: Atom{Kind: AtomTab, Fmt: s.fmt, Ins: s.ins, Del: s.del, Link: s.link, Field: s.field}})
		}
		text = rest
	}
	return nil
}

// newPending returns the pending marker from tag t, omitting empty id and author when filterEmpty.
func newPending(t *tag, filterEmpty bool) FmtPending {
	if filterEmpty {
		return FmtPending{ID: t.nonEmpty("id"), Author: t.nonEmpty("author")}
	}
	return FmtPending{ID: t.value("id"), Author: t.value("author")}
}

// paraOpen opens a paragraph in the dialect state, reading its attributes.
func (s *dialectState) paraOpen(t *tag) error {
	if s.inPara {
		return errorf("<%s> inside a paragraph: close the previous paragraph first", t.name)
	}
	*s = dialectState{items: s.items, inPara: true, paraStart: len(s.items), spans: s.spans, blockEquations: s.blockEquations}
	attrs := ParaAttrs{
		Tag:      t.name,
		Class:    t.value("class"),
		BreakIns: t.marker("break-ins"),
		BreakDel: t.marker("break-del"),
		Num:      t.value("num"),
		CSS:      t.value("style"),
	}
	if id := t.value("list-id"); id.OK {
		n, ok := parseU32(id.V)
		if !ok {
			return errors.New("invalid list id")
		}
		la := ListAttrs{ID: n, Format: "decimal", Start: 1}
		la.Continuation = t.value("list-continuation") == Some("true")
		if v := t.value("list-level"); v.OK {
			if lv, ok := parseU32(v.V); ok {
				la.Level = lv
			}
		}
		if v := t.value("list-format"); v.OK {
			la.Format = v.V
		}
		if v := t.value("list-start"); v.OK {
			if st, ok := parseU32(v.V); ok {
				la.Start = st
			}
		}
		attrs.List = Some(la)
	}
	s.items = append(s.items, Item{Kind: ItemParaOpen, Para: &attrs})
	return nil
}

// paraClose closes the open paragraph, refusing unclosed inline elements.
func (s *dialectState) paraClose(name string) error {
	switch {
	case !s.inPara:
		return errorf("</%s> without an open paragraph", name)
	case s.ins.OK || s.del.OK:
		return errorf("</%s> with an unclosed <ins>/<del>", name)
	case s.fmt != Fmt{}:
		return errorf("</%s> with unclosed formatting tags", name)
	case s.pendingFormat.OK:
		return errorf("</%s> with an unclosed <format pending>", name)
	case s.link.OK:
		return errorf("</%s> with an unclosed <a>", name)
	case s.field.OK:
		return errorf("</%s> with an unclosed <field>", name)
	}
	s.inPara = false
	merged, err := mergeBlockEquations(s.items[s.paraStart:])
	if err != nil {
		return err
	}
	s.items = append(s.items[:s.paraStart], merged...)
	s.items = append(s.items, Item{Kind: ItemParaClose})
	return nil
}

// atom appends item atom a to the state stream.
func (s *dialectState) atom(a Atom) { s.items = append(s.items, Item{Kind: ItemAtom, Atom: a}) }

// cursor walks a dialect event stream.
type cursor struct {
	evs []ev
	i   int
}

// nextText merges the run of text events at the cursor, as the dialect
// lexer reads text up to the next tag.
func (c *cursor) nextText() (string, bool) {
	if c.i >= len(c.evs) || c.evs[c.i].kind != evText {
		return "", false
	}
	var b strings.Builder
	for c.i < len(c.evs) && c.evs[c.i].kind == evText {
		b.WriteString(c.evs[c.i].text)
		c.i++
	}
	return b.String(), true
}

// nextTag advances the cursor and returns the next tag, or nil at text or end.
func (c *cursor) nextTag() *tag {
	if c.i >= len(c.evs) || c.evs[c.i].kind == evText {
		return nil
	}
	e := c.evs[c.i]
	c.i++
	return &tag{name: e.name, attrs: e.attrs, closing: e.kind == evClose, selfClosing: e.selfClosing, omml: e.omml, ref: e.ref, content: e.content}
}

// parseEvents runs the dialect state machine (parse.rs parse_dialect)
// over lowered events.
func parseEvents(evs []ev) ([]Item, error) { return parseEventsIn(evs, false) }

// parseEventsIn is parseEvents; blockEquations lets a block equation
// stand outside a paragraph as a paragraph of its own (a document's
// body, which create builds).
func parseEventsIn(evs []ev, blockEquations bool) ([]Item, error) {
	c := &cursor{evs: evs}
	s := &dialectState{blockEquations: blockEquations}
	for {
		if text, ok := c.nextText(); ok {
			if err := s.text(text); err != nil {
				return nil, err
			}
			continue
		}
		t := c.nextTag()
		if t == nil {
			break
		}
		if err := s.tag(c, t); err != nil {
			return nil, err
		}
	}
	switch {
	case s.inPara:
		return nil, errors.New("fragment ends inside an open paragraph")
	case s.ins.OK || s.del.OK || s.fmt != Fmt{} || s.pendingFormat.OK || s.link.OK || s.field.OK:
		return nil, errors.New("fragment ends with unclosed tags")
	}
	return s.items, nil
}

// tag dispatches dialect tag t to its handler in the state machine.
//
//nolint:gocyclo // one case per dialect tag, mirroring parse.rs.
func (s *dialectState) tag(c *cursor, t *tag) error {
	name := t.name
	switch {
	case isParagraphTag(name) && !t.closing:
		return s.paraOpen(t)
	case isParagraphTag(name):
		return s.paraClose(name)
	case (name == "ins" || name == "del") && !t.closing:
		if !s.inPara {
			return errorf("<%s> outside a paragraph", name)
		}
		slot := &s.ins
		if name == "del" {
			slot = &s.del
		}
		if slot.OK {
			return errorf("<%s> nested inside <%s>", name, name)
		}
		id := t.marker("id")
		*slot = Some(id.V)
	case name == "ins" || name == "del":
		slot := &s.ins
		if name == "del" {
			slot = &s.del
		}
		if !slot.OK {
			return errorf("</%s> without an open <%s>", name, name)
		}
		*slot = WrapID{}
	case name == "mark" && !t.closing:
		s.spans = append(s.spans, spanState{s.fmt, s.pendingFormat})
		value := t.value("data-highlight")
		if !value.OK {
			value = Some("yellow")
		}
		h, err := ParseHighlightColor(value.V)
		if err != nil {
			return err
		}
		s.fmt.Highlight = Some(h)
	case (name == "mark" || name == "span") && t.closing:
		if len(s.spans) == 0 {
			return errorf("</%s> without open %s", name, name)
		}
		top := s.spans[len(s.spans)-1]
		s.spans = s.spans[:len(s.spans)-1]
		s.fmt, s.pendingFormat = top.fmt, top.pending
	case name == "span":
		s.spans = append(s.spans, spanState{s.fmt, s.pendingFormat})
		if style := t.value("style"); style.OK {
			f, err := ApplyInlineCSS(s.fmt, style.V)
			if err != nil {
				return err
			}
			s.fmt = f
		}
		if t.get("pending").OK {
			s.pendingFormat = Some(newPending(t, false))
		}
	case oneOf(name, "b", "i", "u", "s"):
		return s.toggle(t)
	case name == "sup" || name == "sub":
		return s.vertAlign(t)
	case name == "format" && !t.closing:
		return s.formatOpen(t)
	case name == "format":
		if !s.pendingFormat.OK {
			return errors.New("</format> without an open <format pending>")
		}
		s.pendingFormat = Opt[FmtPending]{}
	case name == "br" && !t.closing:
		return s.br(t)
	case name == "image" && !t.closing:
		return s.image(t)
	case name == "image":
		return errors.New("</image> without a matching <image/> — images are self-closing")
	case name == "field" && !t.closing:
		if !s.inPara {
			return errors.New("<field> outside a paragraph")
		}
		if s.field.OK {
			return errors.New("<field> nested inside <field>")
		}
		instr := t.value("instr")
		if !instr.OK {
			return errors.New(`<field> requires instr="…"`)
		}
		s.field = instr
	case name == "field":
		if !s.field.OK {
			return errors.New("</field> without an open <field>")
		}
		s.field = Opt[string]{}
	case name == "opaque" && !t.closing:
		if !s.inPara {
			return errors.New("<opaque> outside a paragraph")
		}
		s.atom(Atom{Kind: AtomOpaque, Text: t.content, AtomObject: &AtomObject{Ref: t.ref, PlaceholderKind: t.value("data-kind").V},
			Ins: s.ins, Del: s.del, Link: s.link, Field: s.field})
	case name == "opaque":
		return errors.New("</opaque> without a matching <opaque>")
	case name == "equation" && !t.closing:
		return s.equation(c, t)
	case name == "equation":
		return errors.New("</equation> without a matching <equation>")
	case name == "a" && !t.closing:
		if !s.inPara {
			return errors.New("<a> outside a paragraph")
		}
		if s.link.OK {
			return errors.New("<a> nested inside <a>")
		}
		href := t.value("href")
		if !href.OK {
			return errors.New(`<a> requires href="…"`)
		}
		s.link = href
	case name == "a":
		if !s.link.OK {
			return errors.New("</a> without an open <a>")
		}
		s.link = Opt[string]{}
	case (name == "comment-start" || name == "comment-end") && !t.closing:
		if !t.selfClosing {
			return errorf(`<%s> must be self-closing (<%s id="…"/>)`, name, name)
		}
		id := t.value("id")
		if !id.OK {
			return errorf("<%s/> requires an id", name)
		}
		s.atom(Atom{Kind: AtomMilestone, AtomObject: &AtomObject{Start: name == "comment-start", ID: id.V}})
	case name == "footnote-ref" || name == "endnote-ref":
		return errorf("<%s> was removed — notes are inline <footnote>/<endnote> elements at the reference site", name)
	case (name == "footnote" || name == "endnote") && !t.closing:
		if !s.inPara {
			return errorf("<%s> outside a paragraph", name)
		}
		inner := ""
		if !t.selfClosing {
			var err error
			if inner, err = takeBalancedInner(c, name); err != nil {
				return err
			}
		}
		s.atom(Atom{Kind: AtomNote, AtomObject: &AtomObject{Endnote: name == "endnote", Inner: CanonicalizeNoteInner(inner)}, Ins: s.ins, Del: s.del})
	case name == "footnote" || name == "endnote":
		return errorf("</%s> without a matching <%s>", name, name)
	case oneOf(name, "table", "tr", "td"):
		if s.inPara {
			return errorf("<%s> inside a paragraph", name)
		}
		token := name
		if t.closing {
			token = "/" + name
		}
		if !t.closing && name == "td" {
			// Canonical span attributes ride in the token so the lockstep
			// walk compares them verbatim like the rest of the structure.
			for _, span := range []string{"colspan", "rowspan"} {
				if v := t.value(span); v.OK {
					token += " " + span + `="` + v.V + `"`
				}
			}
		}
		if v := t.value("data-format"); !t.closing && v.OK {
			token += ` data-format="` + EscapeAttr(v.V) + `"`
		}
		it := Item{Kind: ItemStruct, Struct: token}
		if !t.closing {
			it.Table = &TableAttrs{Colspan: t.value("colspan"), Rowspan: t.value("rowspan"), CSS: t.value("style")}
		}
		s.items = append(s.items, it)
	case name == "comments" || name == "comment":
		return errors.New("the <comments> footer is read-only: it is not part of the editable body")
	case name == "footnotes" || name == "endnotes":
		return errorf("<%s> footers were removed — note bodies render inline inside <footnote>/<endnote> at the reference", name)
	case oneOf(name, "page-chrome", "sect", "hdr", "ftr"):
		return errorf("<%s> was removed — use <section/>, <header>, and <footer> in the package-wide projection", name)
	case oneOf(name, "section", "header", "footer"):
		return errorf("<%s> is a document-surface element — editHtml matches the full projection; structural chrome edits are applied by the surface compiler, not the body fragment parser", name)
	default:
		slash := ""
		if t.closing {
			slash = "/"
		}
		return errorf("unsupported tag <%s%s> — the dialect allows p, h1-h6, a, field, equation, image, footnote, endnote, ins, del, b, i, u, sup, sub, format, br, comment-start/end, table, tr, td", slash, name)
	}
	return nil
}

// toggle opens or closes a character formatting flag with its pending slot.
func (s *dialectState) toggle(t *tag) error {
	name := t.name
	var flag *bool
	var slot *Opt[FmtPending]
	switch name {
	case "b":
		flag, slot = &s.fmt.Bold, &s.pendingB
	case "i":
		flag, slot = &s.fmt.Italic, &s.pendingI
	case "u":
		flag, slot = &s.fmt.Underline, &s.pendingU
	default:
		flag, slot = &s.fmt.Strike, &s.pendingS
	}
	if t.closing {
		if !*flag {
			return errorf("</%s> without an open <%s>", name, name)
		}
		*flag = false
		*slot = Opt[FmtPending]{}
		return nil
	}
	if s.pendingFormat.OK {
		return errorf("<%s> inside <format pending> — the neutral wrapper is only for live dialect format all off", name)
	}
	if *flag {
		return errorf("<%s> nested inside <%s>", name, name)
	}
	*flag = true
	if t.get("pending").OK {
		if slot.OK {
			return errorf("<%s pending> nested inside another pending <%s>", name, name)
		}
		*slot = Some(newPending(t, true))
	}
	return nil
}

// vertAlign opens or closes superscript or subscript formatting.
func (s *dialectState) vertAlign(t *tag) error {
	name := t.name
	align := Superscript
	if name == "sub" {
		align = Subscript
	}
	if t.closing {
		switch {
		case s.fmt.VertAlign == Some(align):
			s.fmt.VertAlign = Opt[VertAlign]{}
			s.pendingVert = Opt[FmtPending]{}
			return nil
		case s.fmt.VertAlign.OK:
			return errorf("</%s> does not match open <%s>", name, s.fmt.VertAlign.V.DialectTag())
		}
		return errorf("</%s> without an open <%s>", name, name)
	}
	if s.pendingFormat.OK {
		return errorf("<%s> inside <format pending> — the neutral wrapper is only for live dialect format all off", name)
	}
	if s.fmt.VertAlign.OK {
		return errorf("<%s> nested inside <%s> — superscript and subscript are mutually exclusive", name, s.fmt.VertAlign.V.DialectTag())
	}
	s.fmt.VertAlign = Some(align)
	if t.get("pending").OK {
		if s.pendingVert.OK {
			return errorf("<%s pending> nested inside another pending vertical-align tag", name)
		}
		s.pendingVert = Some(newPending(t, true))
	}
	return nil
}

// formatOpen opens a pending-format element, validating placement and nesting.
func (s *dialectState) formatOpen(t *tag) error {
	switch {
	case !s.inPara:
		return errors.New("<format> outside a paragraph")
	case !t.get("pending").OK:
		return errors.New("<format> requires the pending attribute (<format pending>…)")
	case s.pendingFormat.OK:
		return errors.New("<format pending> nested inside another <format pending>")
	case s.fmt != Fmt{} || s.pendingB.OK || s.pendingI.OK || s.pendingU.OK || s.pendingS.OK || s.pendingVert.OK:
		return errors.New("<format pending> cannot nest with <b>/<i>/<u>/<s>/<sup>/<sub> — it means live dialect format is all off")
	}
	s.pendingFormat = Some(newPending(t, true))
	return nil
}

// br appends a line, page or column break atom to the paragraph.
func (s *dialectState) br(t *tag) error {
	if !s.inPara {
		return errors.New("<br/> outside a paragraph")
	}
	if s.pendingFormat.OK {
		return errors.New("<format pending> cannot wrap breaks — only character content is supported")
	}
	kind := BrLine
	if a := t.get("type"); a.OK {
		if !a.V.OK {
			return errors.New("<br type> requires a value (page or column)")
		}
		k, err := BrKindFromDialect(a.V)
		if err != nil {
			return err
		}
		kind = k
	}
	s.atom(Atom{Kind: AtomBr, Br: kind, Ins: s.ins, Del: s.del, Link: s.link, Field: s.field})
	return nil
}

// image appends an image atom, requiring self-closing form and a data URL source.
func (s *dialectState) image(t *tag) error {
	if !s.inPara {
		return errors.New("<image/> outside a paragraph")
	}
	if !t.selfClosing {
		return errors.New("<image> must be self-closing (<image/>)")
	}
	// Bound existing sites omit src (the rid is recovered from the XML);
	// authored inserts carry src="data:<mime>;base64,<payload>".
	src := t.value("src").V
	if src != "" && !strings.HasPrefix(src, "data:") {
		return errors.New("image src must be a data: URL (data:<mime>;base64,…); existing images reappear as bare <image/>")
	}
	a := Atom{Kind: AtomImage, AtomObject: &AtomObject{Src: src, RID: t.value("rid").V, Alt: t.value("alt")},
		Ins: s.ins, Del: s.del, Link: s.link, Field: s.field}
	if v := t.value("w"); v.OK {
		n, ok := parseU32(v.V)
		if !ok {
			return errors.New("invalid image width")
		}
		a.Width = Some(n)
	}
	if v := t.value("h"); v.OK {
		n, ok := parseU32(v.V)
		if !ok {
			return errors.New("invalid image height")
		}
		a.Height = Some(n)
	}
	s.atom(a)
	return nil
}

// equation reads <equation display="…">LaTeX</equation>; the lowering has
// validated it and converted it to Office Math.
func (s *dialectState) equation(c *cursor, t *tag) error {
	display := t.value("display") == Some("block")
	own := !s.inPara && s.blockEquations && display
	if !s.inPara && !own {
		return errors.New("<equation> outside a paragraph")
	}
	latex, _ := c.nextText() // the lowering rejects an empty equation
	closing := c.nextTag()
	if closing == nil || closing.name != "equation" || !closing.closing {
		return errors.New("<equation> must be immediately closed by </equation> — equations are atomic and cannot contain markup")
	}
	a := Atom{Kind: AtomEquation, Text: latex, AtomObject: &AtomObject{OMML: t.omml, Ref: t.ref, Display: display}, Ins: s.ins, Del: s.del}
	if own {
		// A block equation of a document's body standing alone is a
		// paragraph of its own, as Word holds a display equation.
		s.items = append(s.items, Item{Kind: ItemParaOpen, Para: &ParaAttrs{Tag: "p"}}, Item{Kind: ItemAtom, Atom: a}, Item{Kind: ItemParaClose})
		return nil
	}
	s.atom(a)
	return nil
}

// takeBalancedInner returns the canonical markup of the events inside an
// open <name> up to its matching close, respecting nesting.
func takeBalancedInner(c *cursor, name string) (string, error) {
	start := c.i
	depth := 1
	for ; c.i < len(c.evs); c.i++ {
		e := c.evs[c.i]
		if e.kind == evText || e.name != name {
			continue
		}
		switch {
		case e.kind == evClose:
			depth--
			if depth == 0 {
				inner := serialize(c.evs[start:c.i])
				c.i++
				return inner, nil
			}
		case !e.selfClosing:
			depth++
		}
	}
	return "", errorf("unclosed <%s>", name)
}
