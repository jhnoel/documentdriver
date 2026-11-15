package plan

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// ErrUndeclared is returned by Verify when the working copy changed
// something no operation declared.
var ErrUndeclared = errors.New("plan: change outside the plan's write set")

// Units is a format's division of its parts into units, the elements the
// verifier compares on their own: for pptx the shape tree, each object,
// each object-level mc:AlternateContent and its branches.
type Units interface {
	// HasUnits reports whether a part, given as its original document,
	// is divided into units; a part that is not is declared as a whole.
	HasUnits(doc *tree.Document) bool
	// IsUnit reports whether element e is a unit.
	IsUnit(e *tree.Node) bool
	// Describe names unit e in an error ("object s17", or its element
	// name).
	Describe(e *tree.Node) string
}

// Verify checks the working copy pkg against the write set ws before
// commit:
//
//   - every changed part was declared;
//   - in every changed part that has units, every unit that existed when
//     the plan started and is not a declared target is byte for byte
//     unchanged (its own content: nested units are compared on their
//     own), and still in the same container, in the same order;
//   - every unit (the root element included) that is not declared keeps
//     its start tag, its own content and its list of child units; a
//     declared container may change only its list of child units.
//
// It compares bytes against the part as stored in the file, so it needs
// no snapshot and sees changes made by any means, not only through the
// format's edit layer.
func Verify(pkg *opc.Package, ws *WriteSet, u Units) error {
	changed, err := pkg.ChangedParts()
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range changed {
		if !ws.parts[PartKey(name)] {
			errs = append(errs, fmt.Errorf("%w: part %s changed but no operation declared it", ErrUndeclared, name))
			continue
		}
		if spans, ok := ws.spliced[PartKey(name)]; ok {
			if err := verifySpliced(pkg, name, spans); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if err := verifyPart(pkg, ws, u, name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// verifySpliced checks a part declared spliced: it changed only by its
// patch, and every replacement lies inside a declared span, so the bytes
// outside the spans are the original's. It needs no tree and no copy of
// the part: the patch's spans are offsets into the original.
func verifySpliced(pkg *opc.Package, name string, declared []splice.Span) error {
	if !pkg.Has(name) {
		return fmt.Errorf("%w: spliced part %s was deleted", ErrUndeclared, name)
	}
	pt, err := pkg.Part(name)
	if err != nil {
		return err
	}
	p := pt.Patch()
	if p == nil {
		return fmt.Errorf("%w: spliced part %s changed other than by a patch", ErrUndeclared, name)
	}
	// The union of the declared spans, as disjoint sorted spans
	// (touching spans merged).
	d := slices.Clone(declared)
	slices.SortFunc(d, func(a, b splice.Span) int { return cmp.Compare(a.Start, b.Start) })
	var union []splice.Span
	for _, s := range d {
		if n := len(union); n > 0 && s.Start <= union[n-1].End {
			union[n-1].End = max(union[n-1].End, s.End)
			continue
		}
		union = append(union, s)
	}
	var errs []error
	for _, e := range p.Spans() {
		i := sort.Search(len(union), func(i int) bool { return union[i].Start > e.Start }) - 1
		if i < 0 || union[i].End < e.End {
			errs = append(errs, fmt.Errorf("%w: %s: bytes [%d, %d) changed outside the declared spans", ErrUndeclared, name, e.Start, e.End))
		}
	}
	return errors.Join(errs...)
}

// unit modes: what of a unit's own bytes is compared.
const (
	full    = iota // content and the list of child units
	markers        // only the list of child units (a target)
	content        // only the content (a container)
)

type verifier struct {
	units      Units
	part       string
	cur        *tree.Document
	curBy      map[int]*tree.Node
	targets    map[int]bool
	containers map[int]bool
	// skip holds, by an element's original byte offset, the attributes
	// its start tag is compared without (declared repairs and root
	// namespaces).
	skip map[int]map[string]bool
	errs []error
}

// verifyPart checks that part changed only in ways the write set declared, returning an error otherwise.
func verifyPart(pkg *opc.Package, ws *WriteSet, u Units, name string) error {
	if !pkg.Has(name) || !pkg.IsXML(name) {
		return nil // deleted, or not XML: declared as a whole
	}
	pt, err := pkg.Part(name)
	if err != nil {
		return err
	}
	orig, ok, err := pt.OriginalString()
	if err != nil || !ok {
		return err // added parts have nothing to compare
	}
	od, err := tree.ParseString(orig)
	if err != nil {
		return fmt.Errorf("plan: %s: original is not well formed: %w", name, err)
	}
	if !u.HasUnits(od) {
		return nil // no units: declared as a whole
	}
	cur, err := pt.Tree()
	if err != nil {
		return err
	}
	if cur.OriginalString() != orig {
		return fmt.Errorf("%w: %s was replaced wholesale", ErrUndeclared, name)
	}
	v := &verifier{units: u, part: name, cur: cur, curBy: map[int]*tree.Node{}, targets: map[int]bool{}, containers: map[int]bool{}, skip: map[int]map[string]bool{}}
	cur.Node().Walk(func(n *tree.Node) bool {
		if s, _, ok := n.SpanIn(cur); ok && n.Kind() == tree.ElementNode {
			v.curBy[s] = n
		}
		return true
	})
	for n := range ws.targets[PartKey(name)] {
		if s, _, ok := n.SpanIn(cur); ok {
			v.targets[s] = true
		}
	}
	for n := range ws.containers[PartKey(name)] {
		if s, _, ok := n.SpanIn(cur); ok {
			v.containers[s] = true
		}
	}
	k := PartKey(name)
	for n, names := range ws.repairs[k] {
		if s, _, ok := n.SpanIn(cur); ok {
			for q := range names {
				v.skipAttr(s, q)
				if _, ok := n.Attr(q); !ok && v.curBy[s] == n {
					v.errs = append(v.errs, fmt.Errorf("%w: %s: repaired attribute %s of %s at byte %d was removed", ErrUndeclared, name, q, n.Name(), s))
				}
			}
		}
	}
	if decls := ws.roots[k]; len(decls) > 0 {
		v.checkRoot(od.Root(), cur.Root(), decls)
	}
	// The prolog and anything after the root element.
	if !bytes.Equal(v.ownBytes(od.Node(), od, full), v.ownBytes(cur.Node(), cur, full)) {
		v.errs = append(v.errs, fmt.Errorf("%w: %s: content outside the root element changed", ErrUndeclared, name))
	}
	v.visit(od.Root(), od)
	return errors.Join(v.errs...)
}

// visit compares the unit n of the original document od with its current
// counterpart, then the units below it. A removed target takes the units
// inside it with it; they are not visited.
func (v *verifier) visit(n *tree.Node, od *tree.Document) {
	if n.Kind() != tree.ElementNode {
		return
	}
	if n == od.Root() || v.units.IsUnit(n) {
		s, _, _ := n.Span()
		tgt, ctr := v.targets[s], v.containers[s]
		c := v.curBy[s]
		if c == nil {
			if !tgt {
				v.fail(n, "was removed but not targeted")
			}
			return
		}
		mode := full
		switch {
		case tgt && ctr:
			mode = -1
		case tgt:
			mode = markers
		case ctr:
			mode = content
		}
		if mode >= 0 && !bytes.Equal(v.ownBytes(n, od, mode), v.ownBytes(c, v.cur, mode)) {
			switch mode {
			case full:
				v.fail(n, "changed but was not targeted")
			case markers:
				v.fail(n, "changed its child objects but is not a declared container")
			case content:
				v.fail(n, "is a declared container but its own content changed")
			}
		}
	}
	for c := range n.ChildrenSeq() {
		v.visit(c, od)
	}
}

// fail records an undeclared change to node n, described by what.
func (v *verifier) fail(n *tree.Node, what string) {
	s, _, _ := n.Span()
	v.errs = append(v.errs, fmt.Errorf("%w: %s: %s at byte %d %s", ErrUndeclared, v.part, v.units.Describe(n), s, what))
}

// ownBytes serialises n's own bytes in doc: its content with each nested
// unit replaced by a marker naming it (its byte offset in the original,
// or "new"), or only the content, or only the markers.
func (v *verifier) ownBytes(n *tree.Node, doc *tree.Document, mode int) []byte {
	var b bytes.Buffer
	var walk func(n *tree.Node, top bool)
	walk = func(n *tree.Node, top bool) {
		if n.Kind() != tree.ElementNode && n.Kind() != tree.DocumentNode {
			if mode != markers {
				b.Write(n.Raw())
			}
			return
		}
		if !top && (v.units.IsUnit(n) || (n.Parent() != nil && n.Parent().Kind() == tree.DocumentNode)) {
			if mode != content {
				if s, _, ok := n.SpanIn(doc); ok {
					b.WriteString("\x00unit " + strconv.Itoa(s) + "\x00")
				} else {
					b.WriteString("\x00new\x00")
				}
			}
			return
		}
		if n.Kind() == tree.ElementNode && mode != markers {
			b.Write(v.startTag(n, doc))
		}
		for c := range n.ChildrenSeq() {
			walk(c, false)
		}
		if n.Kind() == tree.ElementNode && mode != markers {
			b.Write(n.EndTag())
		}
	}
	walk(n, true)
	return b.Bytes()
}
