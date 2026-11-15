package ops

// DebugSetAttr sets an attribute on every element a selector matches in
// one part. It backs `pptx debug set-attr` and is explicitly unsafe, like
// xml.set: it bypasses the property codecs, but it is still atomic,
// declares exactly the objects it changes, and is checked like any other
// operation.
type DebugSetAttr struct {
	Part     string // part name, such as /ppt/slides/slide1.xml
	Selector string // see tree.Document.Select
	Attr     string // qualified attribute name
	Value    string

	// Matched is the number of elements changed, set by a successful run.
	Matched int
}

// Name implements Op.
func (*DebugSetAttr) Name() string { return "debug.set-attr" }

// apply sets the attribute on every matched element and records the match count.
func (o *DebugSetAttr) apply(t *Tx) error {
	n, err := t.ws.RawSetAttr(o.Part, o.Selector, o.Attr, o.Value)
	if err != nil {
		return err
	}
	o.Matched = n
	return nil
}
