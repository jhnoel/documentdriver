package tree

// AttrSpan returns the byte span of attribute qname's raw value (between
// its quotes) in the source n was parsed or scanned from; the quote is
// src[start-1]. ok is false when n has no such attribute, was built in
// code, or its attributes changed since it was read. It does not
// allocate. In a Visitor it is valid in Start and End.
//
// It derives the span from the eq1 offset scanAttr already records (the
// opening quote); no field is added to attr or Node.
func (n *Node) AttrSpan(qname string) (start, end int, ok bool) {
	if n.kind != ElementNode || n.src == nil || n.has(fOwnAttrs) {
		return 0, 0, false
	}
	a, i := n.findAttr(qname)
	if i < 0 || a.lead0 < 0 {
		return 0, 0, false
	}
	s := int(a.eq1) + 1
	return s, s + len(a.raw), true
}
