package xmlscan

import (
	"cmp"
	"errors"
	"slices"
	"strings"
)

// ErrOverlap is returned by Apply when two edits overlap or an edit is
// not a range of the source.
var ErrOverlap = errors.New("xmlscan: edits overlap or fall outside the source")

// ErrValue is returned by EditTag when an attribute name is not an XML
// qualified name or a value holds a character XML cannot carry.
var ErrValue = errors.New("xmlscan: not an XML name or value")

// Edit replaces src[Start:End] with Text (Start == End inserts).
type Edit struct {
	Start, End int
	Text       string
}

// Apply returns src with edits applied. It sorts edits in place by
// Start, then End, keeping the given order among equal ones (so several
// insertions at one offset are written in order, before a replacement
// that starts there). Overlapping edits, or one outside src, are
// ErrOverlap. With no edits it returns src itself; otherwise it
// allocates the result once. Bytes outside the edits are copied.
//
// Apply is for sub-node edits of a small element (an attribute value, a
// formula text); a large part's whole-node edits are a splice.Patch.
func Apply(src string, edits []Edit) (string, error) {
	if len(edits) == 0 {
		return src, nil
	}
	slices.SortStableFunc(edits, func(a, b Edit) int {
		if c := cmp.Compare(a.Start, b.Start); c != 0 {
			return c
		}
		return cmp.Compare(a.End, b.End)
	})
	at, n := 0, len(src)
	for _, e := range edits {
		if e.Start < at || e.End < e.Start || e.End > len(src) {
			return "", ErrOverlap
		}
		at = e.End
		n += len(e.Text) - (e.End - e.Start)
	}
	var b strings.Builder
	b.Grow(n)
	at = 0
	for _, e := range edits {
		b.WriteString(src[at:e.Start])
		b.WriteString(e.Text)
		at = e.End
	}
	b.WriteString(src[at:])
	return b.String(), nil
}

// AttrEdit sets attribute Name to Value, or removes it when Remove.
type AttrEdit struct {
	Name, Value string
	Remove      bool
}

// EditTag returns the text of tag t of src (a start or empty tag from
// Next) with edits applied. When several edits name one attribute, the
// last one counts. The exact byte rules:
//
//   - An existing attribute keeps its position, leading whitespace, '='
//     spelling and quote; only src[ValueStart:ValueEnd] is replaced, by
//     EscapeAttr(value, its quote).
//   - A value equal to the attribute's decoded current value changes
//     nothing.
//   - A removed attribute loses src[a.Start:a.End()], its leading
//     whitespace included; removing an absent one changes nothing.
//   - New attributes are inserted, in edits order, each as
//     ` name="value"` (always a double quote, value escaped by
//     EscapeAttr), right after the last attribute's closing quote, or
//     right after the tag name when the tag has none; whitespace before
//     the tag's '>' or "/>" stays after them.
//   - Every name must pass ValidQName and every value set ValidChars;
//     otherwise EditTag returns the tag unchanged and ErrValue.
//
// Bytes outside the edited attributes and the insertion point are
// copied. With nothing to change it returns the tag as a substring of src
// (no allocation). A token that is not a start or empty tag of src is
// ErrSyntax.
func EditTag(src string, t Token, edits ...AttrEdit) (string, error) {
	if !t.IsElement() || t.Start < 0 || t.End > len(src) || t.NameEnd > t.End {
		return "", ErrSyntax
	}
	tag := src[t.Start:t.End]
	for _, e := range edits {
		if !ValidQName(e.Name) || !e.Remove && !ValidChars(e.Value) {
			return tag, ErrValue
		}
	}
	ins := t.NameEnd
	for a := range Attrs(src, t) {
		ins = a.End()
	}
	var out []Edit
	for i, e := range edits {
		if slices.ContainsFunc(edits[i+1:], func(l AttrEdit) bool { return l.Name == e.Name }) {
			continue
		}
		a, ok := FindAttr(src, t, e.Name)
		switch {
		case ok && e.Remove:
			out = append(out, Edit{a.Start - t.Start, a.End() - t.Start, ""})
		case ok:
			if a.Value(src) != e.Value {
				out = append(out, Edit{a.ValueStart - t.Start, a.ValueEnd - t.Start, EscapeAttr(e.Value, a.Quote(src))})
			}
		case !e.Remove:
			out = append(out, Edit{ins - t.Start, ins - t.Start, " " + e.Name + `="` + EscapeAttr(e.Value, '"') + `"`})
		}
	}
	return Apply(tag, out)
}
