package edit

import (
	"fmt"
	"path"
	"strings"

	"github.com/jhnoel/documentdriver/internal/opc"
)

// AddPart adds a new part name holding empty, with content type ct and a
// relationship of type rel from the main document part (a numbering part,
// a header or footer, the comment parts), and returns the relationship's
// id (the one RelID gives). It declares what it changes: the part, the
// content types stream and the main document's relationships part. It
// refuses up front, before any change, when the content types stream
// cannot register a part (opc.ErrContentTypes) or the name is taken. A
// name for a part that may occur more than once comes from PartName.
func (w *Workspace) AddPart(rel, ct, name, empty string) (string, error) {
	return w.AddPartFrom(w.doc, rel, ct, name, []byte(empty))
}

// AddPartFrom is AddPart with the part's bytes and the relationship from
// part source (a header's picture is related from the header).
func (w *Workspace) AddPartFrom(source, rel, ct, name string, data []byte) (string, error) {
	if err := w.pkg.RegistersTypes(); err != nil {
		return "", err
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	if w.pkg.Has(name) {
		return "", fmt.Errorf("edit: %s already exists", name)
	}
	rels, err := w.pkg.Rels(source)
	if err != nil {
		return "", err
	}
	w.core.DeclarePart(name)
	w.core.DeclarePart(opc.ContentTypesName)
	if _, err := w.pkg.AddPart(name, data, ct); err != nil {
		return "", err
	}
	w.core.DeclarePart(rels.Name())
	return rels.Add(rel, relTarget(source, name), false)
}

// RelateFrom adds a relationship of type rel from part source to the
// existing part name and returns its id, declaring source's
// relationships part.
func (w *Workspace) RelateFrom(source, rel, name string) (string, error) {
	rels, err := w.pkg.Rels(source)
	if err != nil {
		return "", err
	}
	w.core.DeclarePart(rels.Name())
	return rels.Add(rel, relTarget(source, name), false)
}

// relTarget is the relationship target of part name from part source:
// relative when name is in source's directory or below it, else the
// absolute part name.
func relTarget(source, name string) string {
	dir := path.Dir(source)
	if dir == "/" {
		return strings.TrimPrefix(name, "/")
	}
	if rest, ok := strings.CutPrefix(name, dir+"/"); ok {
		return rest
	}
	return name
}
