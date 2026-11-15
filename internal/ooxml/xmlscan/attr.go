package xmlscan

import (
	"iter"
	"strings"
)

// Attr is one attribute of a tag as written, as offsets into the source.
// src[Start:NameStart] is the whitespace before it (so src[Start:End()]
// is the whole attribute, to drop it), src[NameStart:NameEnd] its
// qualified name, src[ValueStart:ValueEnd] its raw value between the
// quotes, and src[ValueStart-1] the quote.
type Attr struct {
	Start, NameStart, NameEnd, ValueStart, ValueEnd int
}

// Name returns the attribute's qualified name as written. It does not
// allocate.
func (a Attr) Name(src string) string { return src[a.NameStart:a.NameEnd] }

// Raw returns the attribute's value as written between its quotes,
// references undecoded. It does not allocate.
func (a Attr) Raw(src string) string { return src[a.ValueStart:a.ValueEnd] }

// Value returns the attribute's decoded value (DecodeAttr of Raw). It
// returns Raw itself when there is nothing to decode.
func (a Attr) Value(src string) string { return DecodeAttr(a.Raw(src)) }

// Quote returns the attribute's quote character: a double or a single quote.
func (a Attr) Quote(src string) byte { return src[a.ValueStart-1] }

// End returns the offset just past the attribute's closing quote.
func (a Attr) End() int { return a.ValueEnd + 1 }

// attrAt reads the attribute whose name starts at k, with its leading
// whitespace starting at start: name, S? '=' S?, a quoted value holding
// no '<'.
func attrAt(src string, start, k int) (Attr, bool) {
	ns := k
	for k < len(src) {
		c := src[k]
		if c == '=' || IsSpace(c) {
			break
		}
		if c == '<' || c == '>' || c == '/' || c == '"' || c == '\'' {
			return Attr{}, false
		}
		k++
	}
	if k == ns {
		return Attr{}, false
	}
	ne := k
	for k < len(src) && IsSpace(src[k]) {
		k++
	}
	if k >= len(src) || src[k] != '=' {
		return Attr{}, false
	}
	k++
	for k < len(src) && IsSpace(src[k]) {
		k++
	}
	if k >= len(src) || (src[k] != '"' && src[k] != '\'') {
		return Attr{}, false
	}
	q := src[k]
	vs := k + 1
	e := strings.IndexByte(src[vs:], q)
	if e < 0 || strings.IndexByte(src[vs:vs+e], '<') >= 0 {
		return Attr{}, false
	}
	return Attr{Start: start, NameStart: ns, NameEnd: ne, ValueStart: vs, ValueEnd: vs + e}, true
}

// nextAttr returns the attribute after offset k of tag t (k is NameEnd
// or an attribute's End), or false at the tag's close.
func nextAttr(src string, t Token, k int) (Attr, bool) {
	end := min(t.End, len(src))
	ws := k
	for k < end && IsSpace(src[k]) {
		k++
	}
	if k >= end || k == ws || src[k] == '>' || src[k] == '/' {
		return Attr{}, false
	}
	a, ok := attrAt(src, ws, k)
	if !ok || a.End() > end {
		return Attr{}, false
	}
	return a, true
}

// Attrs yields the attributes of tag t of src in document order,
// namespace declarations included. t must come from Next (a start or
// empty tag; any other token yields nothing). It does not allocate.
func Attrs(src string, t Token) iter.Seq[Attr] {
	return func(yield func(Attr) bool) {
		if !t.IsElement() {
			return
		}
		for k := t.NameEnd; ; {
			a, ok := nextAttr(src, t, k)
			if !ok || !yield(a) {
				return
			}
			k = a.End()
		}
	}
}

// FindAttr returns the attribute of tag t of src whose qualified name as
// written is qname (the first, if a malformed tag repeats it). It does
// not allocate.
func FindAttr(src string, t Token, qname string) (Attr, bool) {
	if !t.IsElement() {
		return Attr{}, false
	}
	for k := t.NameEnd; ; {
		a, ok := nextAttr(src, t, k)
		if !ok {
			return Attr{}, false
		}
		if src[a.NameStart:a.NameEnd] == qname {
			return a, true
		}
		k = a.End()
	}
}
