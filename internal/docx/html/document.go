package html

import (
	"strings"
	"unicode"
)

// Document HTML (docx create): docxdriver lowers it into the dialect and
// splits that string into sections and leading header/footer slots
// (build.rs parse_create_sections) before parsing each piece. The same
// split is made here over the event stream. Compiling the pieces into
// WordprocessingML stays with the build port; it raises Err fields in
// docxdriver's order: each slot's as the split meets it, then each body's.

// Chrome is a header or footer slot authored at the start of a section.
type Chrome struct {
	Scope string // "hdr" or "ftr"
	Kind  string // "default", "first" or "even"
	Items []Item // empty for an empty slot
	Err   error  // the slot's dialect error, if any
}

// Section is one section of document HTML.
type Section struct {
	TitlePage bool // <section first-page>
	Slots     []Chrome
	Body      []Item
	BodyErr   error // the body's dialect error, if any
	// BodyBlank reports a body with no content at all (whitespace only).
	BodyBlank bool
}

// ParseDocument lowers document HTML and splits it into sections. A
// lowering error fails the whole document; the dialect errors of each
// piece are returned with the piece.
func ParseDocument(input string, eq LatexToOMML) ([]Section, error) {
	evs, err := lowerHTML(input, true, eq)
	if err != nil {
		return nil, err
	}
	var sections []Section
	var cur Section
	var body []ev
	sawMarker := false
	push := func() {
		cur.Body, cur.BodyErr = parseEventsIn(body, true)
		cur.BodyBlank = blank(body)
		sections = append(sections, cur)
		cur, body = Section{}, nil
	}
	rest := trimEvents(evs, unicode.IsSpace, false)
	for {
		rest = trimEvents(rest, unicode.IsSpace, false)
		if len(rest) == 0 {
			break
		}
		e := rest[0]
		if e.kind == evOpen && e.name == "section" {
			title := hasTagAttr(e.attrs, "first-page")
			rest = rest[1:]
			if len(body) > 0 || len(cur.Slots) > 0 || sawMarker || cur.TitlePage {
				push()
			}
			cur.TitlePage = title
			sawMarker = true
			continue
		}
		if e.kind == evOpen && (e.name == "header" || e.name == "footer") {
			end := len(rest)
			for i := 1; i < len(rest); i++ {
				if rest[i].kind == evClose && rest[i].name == e.name {
					end = i
					break
				}
			}
			if end == len(rest) {
				return nil, errorf("unclosed <%s>", e.name)
			}
			slot := Chrome{Scope: "hdr", Kind: "default"}
			if e.name == "footer" {
				slot.Scope = "ftr"
			}
			if hasTagAttr(e.attrs, "first") {
				slot.Kind = "first"
			} else if hasTagAttr(e.attrs, "even") {
				slot.Kind = "even"
			}
			inner := trimEvents(trimEvents(rest[1:end], isNewline, false), isNewline, true)
			slot.Items, slot.Err = parseEvents(inner)
			cur.Slots = append(cur.Slots, slot)
			rest = rest[end+1:]
			continue
		}
		next := len(rest)
		for i, x := range rest {
			if x.kind == evOpen && x.name == "section" {
				next = i
				break
			}
		}
		chunk := trimEvents(trimEvents(rest[:next], unicode.IsSpace, false), unicode.IsSpace, true)
		if len(chunk) > 0 {
			if len(body) > 0 {
				body = append(body, textEv("\n"))
			}
			body = append(body, chunk...)
		}
		rest = rest[next:]
	}
	push()
	if len(sections) == 1 && sections[0].BodyBlank && len(sections[0].Slots) > 0 {
		sections[0].Body = []Item{{Kind: ItemParaOpen, Para: &ParaAttrs{Tag: "p"}}, {Kind: ItemParaClose}}
		sections[0].BodyErr = nil
		sections[0].BodyBlank = false
	}
	return sections, nil
}

// isNewline reports whether r is a newline.
func isNewline(r rune) bool { return r == '\n' }

// hasTagAttr reports whether attrs contains an attribute named name.
func hasTagAttr(attrs []tagAttr, name string) bool {
	for _, a := range attrs {
		if a.name == name {
			return true
		}
	}
	return false
}

// trimEvents trims runes matching cut from the start (or, with end, the
// end) of the text at that edge of the stream, as str::trim_start_matches
// would on the serialised form.
func trimEvents(evs []ev, cut func(rune) bool, end bool) []ev {
	for len(evs) > 0 {
		i := 0
		if end {
			i = len(evs) - 1
		}
		if evs[i].kind != evText {
			return evs
		}
		var t string
		if end {
			t = strings.TrimRightFunc(evs[i].text, cut)
		} else {
			t = strings.TrimLeftFunc(evs[i].text, cut)
		}
		if t != "" {
			out := append([]ev(nil), evs...)
			out[i].text = t
			return out
		}
		if end {
			evs = evs[:i]
		} else {
			evs = evs[1:]
		}
	}
	return evs
}
