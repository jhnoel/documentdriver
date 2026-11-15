package edit

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// ErrUndeclared is returned by Verify when the working copy changed
// something no operation declared (plan.ErrUndeclared).
var ErrUndeclared = plan.ErrUndeclared

// Verify checks the working copy against the write set before commit,
// with plan.Verify over the slide's units (see units):
//
//   - every changed part was declared;
//   - in every changed part with a shape tree, every object that existed
//     when the plan started and is not a declared target is byte for byte
//     unchanged (its own content: nested objects are compared on their
//     own), and still in the same container, in the same order;
//   - every container (the slide root, the shape tree, groups,
//     mc:AlternateContent and its branches) that is not declared keeps its
//     start tag, its own content and its list of child objects; a declared
//     container may change only its list of child objects.
//
// It compares bytes against the part as stored in the file, so it needs
// no snapshot and sees changes made by any means, not only through
// Handles.
func (w *Workspace) Verify() error { return w.core.Verify() }

// units divides PresentationML parts for the verifier: a part with a
// shape tree has units (isUnit); objects are named by their id.
type units struct{}

// HasUnits implements plan.Units: parts with a p:cSld/p:spTree.
func (units) HasUnits(doc *tree.Document) bool {
	return derive.Path(doc.Root(), derive.PML, "cSld", "spTree") != nil
}

// IsUnit implements plan.Units.
func (units) IsUnit(e *tree.Node) bool { return isUnit(e) }

// Describe implements plan.Units.
func (units) Describe(n *tree.Node) string {
	if isObject(n) {
		return "object s" + objectID(n)
	}
	return n.Name()
}

// isContainer reports whether e holds objects: a shape tree, a group, or
// a branch of an object-level mc:AlternateContent.
func isContainer(e *tree.Node) bool {
	return derive.Is(e, derive.PML, "spTree") || isGroup(e) || (isBranch(e) && isUnit(e))
}

// isUnit reports whether e is compared on its own by the verifier: a
// shape tree, an object or mc:AlternateContent in a container, or a
// branch of such an mc:AlternateContent.
func isUnit(e *tree.Node) bool {
	if e.Kind() != tree.ElementNode {
		return false
	}
	if derive.Is(e, derive.PML, "spTree") {
		return true
	}
	p := e.Parent()
	if p == nil {
		return false
	}
	if isObject(e) || isAC(e) {
		return isContainer(p)
	}
	if isBranch(e) {
		return isAC(p) && isUnit(p)
	}
	return false
}

// Units returns the slide units the verifier compares (plan.Units), for
// law harnesses that hold plans to the same division (internal/laws).
func Units() plan.Units { return units{} }
