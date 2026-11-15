package edit

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// Format is what the core needs from a format's edit layer.
type Format struct {
	// Vocab lists the namespace families the format writes.
	Vocab Vocab
	// Units divides the format's parts into the units the commit-time
	// verifier compares (plan.Verify) and RawSetAttr declares.
	Units plan.Units
	// Type returns the schema complex type of a handle's branch element
	// (p:sp is p:CT_Shape), and false when the schema does not tabulate
	// it. Ensure and the placed inserts walk paths from it.
	Type func(e *tree.Node) (string, bool)
	// Bound reports whether a path step onto element e would leave the
	// handle's own content (into another object); such paths fail.
	Bound func(e *tree.Node) bool
	// PropertyChoices are the complex types whose repeated choice holds
	// properties that each appear once (w:CT_RPr): Ensure places a new
	// one by declaration order instead of refusing a repeated slot.
	PropertyChoices map[string]bool
}

// Workspace is a plan's editing view of a package, shared by the
// formats: it records the write set (parts, target units and container
// units, keyed by part) that Verify checks at commit, hands out Handles
// and places units. A format's edit layer wraps it with its own
// addresses; it never saves.
type Workspace struct {
	pkg *opc.Package
	ws  *plan.WriteSet
	f   *Format
	// created holds every element the workspace inserted new, with its
	// part key, and order the same elements in creation order (see
	// Created).
	created map[*tree.Node]string
	order   []*tree.Node
	// seq is each created element's position in order.
	seq map[*tree.Node]int
	// source maps a copy to the element it was made from (SourceOf).
	source map[*tree.Node]*tree.Node
}

// New returns a workspace over pkg for format f.
func New(pkg *opc.Package, f Format) *Workspace {
	return &Workspace{pkg: pkg, ws: plan.NewWriteSet(), f: &f, created: map[*tree.Node]string{}}
}

// Tree returns the parsed tree of a part of the working copy.
func (w *Workspace) Tree(part string) (*tree.Document, error) {
	pt, err := w.pkg.Part(part)
	if err != nil {
		return nil, err
	}
	return pt.Tree()
}

// DeclarePart adds a part the plan may change as a whole (relationships,
// content types, media). Parts with units are declared through Target
// and DeclareContainer instead, which also say which units may change.
func (w *Workspace) DeclarePart(name string) { w.ws.DeclarePart(name) }

// DeclareSpliced declares that part may change by a splice patch within
// spans of its original bytes (plan.WriteSet.DeclareSpliced): the xlsx
// edit layer's worksheets, which are never parsed into a tree.
func (w *Workspace) DeclareSpliced(part string, spans ...splice.Span) {
	w.ws.DeclareSpliced(part, spans...)
}

// DeclareContainer declares unit n of part as a container of the plan:
// its list of child units may change.
func (w *Workspace) DeclareContainer(part string, n *tree.Node) { w.ws.DeclareContainer(part, n) }

// Target returns a Handle over the branch elements br of one addressed
// thing in part (br[0] is the one the views show) and declares each as a
// target of the plan: its own content may change. label names it in
// errors.
func (w *Workspace) Target(part, label string, br []*tree.Node) *Handle {
	for _, b := range br {
		w.ws.DeclareTarget(part, b)
	}
	return w.Peek(part, label, br)
}

// Peek returns a Handle over br for reading only: it declares nothing,
// so any change made through it fails Verify.
func (w *Workspace) Peek(part, label string, br []*tree.Node) *Handle {
	return &Handle{w: w, part: part, label: label, br: br}
}

// Verify checks the working copy against the write set before commit
// (plan.Verify over the format's units).
func (w *Workspace) Verify() error { return plan.Verify(w.pkg, w.ws, w.f.Units) }

// RawSetAttr sets an attribute on every element a selector matches in one
// part (see tree.Document.Select). It backs debug commands and is
// explicitly unsafe: it bypasses the property codecs and mc branches. It
// still declares its write set exactly: the part, and in a part with
// units the nearest enclosing unit of each matched element (or the root
// element), so the verifier holds it to the elements it matched. It
// returns the number of elements changed.
func (w *Workspace) RawSetAttr(part, selector, attr, value string) (int, error) {
	doc, err := w.Tree(part)
	if err != nil {
		return 0, err
	}
	nodes, err := doc.Select(selector)
	if err != nil {
		return 0, err
	}
	if len(nodes) == 0 {
		return 0, fmt.Errorf("selector %q matched nothing in %s", selector, part)
	}
	w.DeclarePart(part)
	for _, el := range nodes {
		w.ws.DeclareTarget(part, w.enclosingUnit(doc, el))
	}
	for _, el := range nodes {
		if err := el.SetAttr(attr, value); err != nil {
			return 0, err
		}
	}
	return len(nodes), nil
}

// enclosingUnit returns the nearest ancestor-or-self of e that the
// verifier compares on its own, or the root element.
func (w *Workspace) enclosingUnit(doc *tree.Document, e *tree.Node) *tree.Node {
	for p := e; p != nil; p = p.Parent() {
		if p == doc.Root() || w.f.Units.IsUnit(p) {
			return p
		}
	}
	return doc.Root()
}

// Declared returns the plan's write set for reading (integrity
// attribution: which parts, units and attributes the plan declared).
// Declarations go through the workspace, its handles and regions only.
func (w *Workspace) Declared() Declared { return Declared{w.ws} }

// Declared is a read-only view of a plan's write set.
type Declared struct{ ws *plan.WriteSet }

// Parts returns the declared part keys, sorted.
func (d Declared) Parts() []string { return d.ws.Parts() }

// Targets returns the declared target units of part.
func (d Declared) Targets(part string) []*tree.Node { return d.ws.Targets(part) }

// Containers returns the declared container units of part.
func (d Declared) Containers(part string) []*tree.Node { return d.ws.Containers(part) }

// Repairs returns the declared repaired attributes of part.
func (d Declared) Repairs(part string) []plan.RepairedAttr { return d.ws.Repairs(part) }

// RootNamespaces returns the declared root namespaces of part.
func (d Declared) RootNamespaces(part string) map[string]plan.RootNamespace {
	return d.ws.RootNamespaces(part)
}
