package model

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Placeholder is one piece of a paragraph's current content that the
// HTML projection cannot express as editable markup (docs/contract.md,
// section 7, placeholders): a drawing, an object, a symbol, a note
// reference, a field, a content control, a ruby, alternate content, an
// element the taxonomy does not know. The projection writes it with its
// address, <paragraph>/opaque:N (N its 1-based position among the
// paragraph's placeholders), and HTML input that carries the address
// stands for these exact nodes, moved where the HTML puts them.
type Placeholder struct {
	// Kind names what it is, as data-kind shows it: drawing, object,
	// symbol, footnote, endnote, note-mark, comment-mark, separator,
	// field, content-control, ruby, alternate-content or unknown.
	Kind string
	// Key is the element the projection meets first: the object itself,
	// the wrapper, or a complex field's begin w:fldChar.
	Key *tree.Node
	// Nodes are the placeholder's elements, consecutive siblings: one
	// run child when InRun, else one or more children of the paragraph
	// or of a wrapper inside it (a complex field is the runs from its
	// begin to its end).
	Nodes []*tree.Node
	// InRun reports that Nodes[0] is a child of a run (an object inside
	// w:r); the run's properties are its formatting.
	InRun bool
}

// objectKinds names the KindObject elements (equations aside) by the
// placeholder kind they are.
var objectKinds = map[string]string{
	"drawing": "drawing", "pict": "drawing", "contentPart": "drawing", "object": "object",
	"sym": "symbol", "footnoteReference": "footnote", "endnoteReference": "endnote",
	"footnoteRef": "note-mark", "endnoteRef": "note-mark", "annotationRef": "comment-mark",
	"separator": "separator", "continuationSeparator": "separator",
	"pgNum": "field", "dayShort": "field", "dayLong": "field", "monthShort": "field",
	"monthLong": "field", "yearShort": "field", "yearLong": "field",
}

// transparent are the wrappers whose content is walked for placeholders:
// a hyperlink, smart tag, custom XML and bidi wrappers, and current
// revision wrappers (insertions and move destinations).
var transparent = []string{"hyperlink", "smartTag", "customXml", "dir", "bdo", "ins", "moveTo"}

// Placeholders returns paragraph p's placeholders, in document order,
// over its current content (deleted and moved-away content is not
// current), and blocked, the label of content that is neither
// expressible nor one placeholder ("field": a complex field that does
// not begin and end within one container of the paragraph, or shares a
// run with other content at its ends); "" when there is none. Nothing
// inside a placeholder is counted, and equations are not placeholders
// (CurrentEquations counts them). The walk is depth-limited.
func Placeholders(p *tree.Node) (list []Placeholder, blocked string) {
	w := &placeholderWalk{}
	w.container(p, 0)
	return w.out, w.blocked
}

type placeholderWalk struct {
	out     []Placeholder
	blocked string
}

// add appends a placeholder of kind with key, nodes and inRun to the walk's output.
func (w *placeholderWalk) add(kind string, key *tree.Node, nodes []*tree.Node, inRun bool) {
	w.out = append(w.out, Placeholder{Kind: kind, Key: key, Nodes: nodes, InRun: inRun})
}

// container walks the children of a paragraph or of a transparent
// wrapper.
func (w *placeholderWalk) container(c *tree.Node, depth int) {
	if depth > tree.MaxDepth {
		return
	}
	kids := c.Children()
	for i := 0; i < len(kids); i++ {
		ch := kids[i]
		if !IsElement(ch) {
			continue
		}
		switch {
		case IsLocal(ch, "oMath"), IsLocal(ch, "oMathPara"):
		case IsLocal(ch, "AlternateContent"):
			w.add("alternate-content", ch, []*tree.Node{ch}, false)
		case IsRevDel(ch):
		case IsW(ch, "r"):
			if begin, ok := fieldStart(ch); ok {
				end, clean := fieldUnit(kids, i)
				if end < 0 {
					w.blocked = "field"
					w.run(ch, true)
					continue
				}
				if !clean || !onlyBefore(ch, begin) {
					w.blocked = "field"
				} else {
					w.add("field", begin, elements(kids[i:end+1]), false)
				}
				i = end
				continue
			}
			if hasFieldChar(ch) {
				// A field ends or separates here that began elsewhere.
				w.blocked = "field"
			}
			w.run(ch, false)
		case IsW(ch, "fldSimple"):
			w.add("field", ch, []*tree.Node{ch}, false)
		case IsW(ch, "sdt"):
			w.add("content-control", ch, []*tree.Node{ch}, false)
		case IsW(ch, "ruby"):
			w.add("ruby", ch, []*tree.Node{ch}, false)
		case isTransparent(ch):
			w.container(ch, depth+1)
		default:
			switch Classify(ch) {
			case KindUnknown:
				w.add("unknown", ch, []*tree.Node{ch}, false)
			case KindObject:
				// An object outside a run (not valid WML, but kept).
				w.add(objectKinds[ch.Local()], ch, []*tree.Node{ch}, false)
			}
		}
	}
}

// isTransparent reports whether n is a wrapper whose content is walked for placeholders.
func isTransparent(n *tree.Node) bool {
	for _, l := range transparent {
		if IsW(n, l) {
			return true
		}
	}
	return false
}

// run adds the objects of run r. inField: r begins a field that does
// not close, whose objects are not counted.
func (w *placeholderWalk) run(r *tree.Node, inField bool) {
	if inField {
		return
	}
	for ch := range r.ChildrenSeq() {
		if !IsElement(ch) {
			continue
		}
		switch {
		case IsLocal(ch, "AlternateContent"):
			w.add("drawing", ch, []*tree.Node{ch}, true)
		case IsLocal(ch, "oMath"), IsLocal(ch, "oMathPara"):
		default:
			switch Classify(ch) {
			case KindObject:
				w.add(objectKinds[ch.Local()], ch, []*tree.Node{ch}, true)
			case KindUnknown:
				w.add("unknown", ch, []*tree.Node{ch}, true)
			}
		}
	}
}

// fieldStart returns the first begin w:fldChar directly in run r.
func fieldStart(r *tree.Node) (*tree.Node, bool) {
	for ch := range r.ChildrenSeq() {
		if IsW(ch, "fldChar") && AttrOr(ch, "fldCharType") == "begin" {
			return ch, true
		}
	}
	return nil, false
}

// hasFieldChar reports whether run r holds a w:fldChar.
func hasFieldChar(r *tree.Node) bool {
	return ChildW(r, "fldChar") != nil
}

// onlyBefore reports whether nothing but properties and markers stands
// before child c of run r.
func onlyBefore(r, c *tree.Node) bool {
	for ch := range r.ChildrenSeq() {
		if ch == c {
			return true
		}
		if !IsElement(ch) {
			continue
		}
		switch Classify(ch) {
		case KindProps, KindMarker:
		default:
			return false
		}
	}
	return true
}

// fieldUnit finds the sibling index that closes the complex field begun
// in kids[i] (a run): the run whose w:fldChar end brings the nesting
// back to zero, counting the field characters of every sibling and their
// descendants in order. It returns -1 when the field does not close among
// the siblings, and clean false when the closing run holds content after
// its end or a sibling between is not a run, a field-result wrapper or a
// marker.
func fieldUnit(kids []*tree.Node, i int) (end int, clean bool) {
	depth := 0
	clean = true
	for j := i; j < len(kids); j++ {
		k := kids[j]
		if !IsElement(k) {
			continue
		}
		if j > i && !IsW(k, "r") && !isTransparent(k) && Classify(k) != KindMarker && !IsW(k, "fldSimple") &&
			!IsLocal(k, "oMath") && !IsLocal(k, "oMathPara") && !IsRevDel(k) {
			clean = false
		}
		closed := false
		after := false
		k.Walk(func(n *tree.Node) bool {
			if closed {
				if IsElement(n) && n.Parent() == k {
					switch Classify(n) {
					case KindProps, KindMarker:
					default:
						after = true
					}
				}
				return false
			}
			if IsW(n, "fldChar") {
				switch AttrOr(n, "fldCharType") {
				case "begin":
					depth++
				case "end":
					depth--
					if depth == 0 {
						closed = true
					}
				}
			}
			return true
		})
		if closed {
			if after || !IsW(k, "r") {
				clean = false
			}
			return j, clean
		}
	}
	return -1, false
}

// elements returns the element nodes of nodes.
func elements(nodes []*tree.Node) []*tree.Node {
	var out []*tree.Node
	for _, n := range nodes {
		if IsElement(n) {
			out = append(out, n)
		}
	}
	return out
}

// PlaceholderText is the text the projection writes inside placeholder
// ph (a field's result, a content control's or ruby's text): the
// characters of its w:t leaves and its tabs, in order. HTML input that
// sends the placeholder back must carry the same text: its content is
// not editable there.
func PlaceholderText(ph Placeholder) string {
	var b []byte
	for _, n := range ph.Nodes {
		n.Walk(func(c *tree.Node) bool {
			switch {
			case IsW(c, "t"):
				b = append(b, TextContent(c)...)
				return false
			case IsW(c, "tab"):
				b = append(b, '\t')
			case IsW(c, "instrText"), IsW(c, "delText"), IsW(c, "delInstrText"), IsW(c, "p"):
				return false
			case IsRevDel(c):
				return false
			}
			return true
		})
	}
	return string(b)
}

// ObjectKind is the placeholder kind of a KindObject element (equations
// aside): drawing, object, symbol, footnote, ... ("unknown" for none).
func ObjectKind(n *tree.Node) string {
	if k, ok := objectKinds[n.Local()]; ok {
		return k
	}
	return "unknown"
}
