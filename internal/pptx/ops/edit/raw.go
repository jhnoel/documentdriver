package edit

// RawSetAttr sets an attribute on every element a selector matches in one
// part (see tree.Document.Select). It backs the debug command and is
// explicitly unsafe: it bypasses the property codecs and mc branches. It
// still declares its write set exactly: the part, and in a part with a
// shape tree the nearest enclosing unit of each matched element (the
// object, container or slide root whose own bytes change), so the
// verifier holds it to the elements it matched (core.Workspace.RawSetAttr
// over the slide units). It returns the number of elements changed.
func (w *Workspace) RawSetAttr(part, selector, attr, value string) (int, error) {
	return w.core.RawSetAttr(part, selector, attr, value)
}
