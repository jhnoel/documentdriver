package edit

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// Workspace is a plan's editing view of a deck: the edit core's
// workspace (which records the write set Verify checks at commit, hands
// out handles and places units) with slide addresses on top. It resolves
// addresses, hands out Handles, allocates ids and binds aliases. It never
// saves: internal/pptx/ops does.
type Workspace struct {
	pkg     *opc.Package
	core    *core.Workspace
	slides  []string // slide parts in presentation order, once listed
	listed  bool
	aliases map[Alias]Ref
	nextID  map[string]int64 // by part key: the next id to allocate
}

// pptx is what the edit core needs from PresentationML: its namespaces,
// the slide units, the complex type of an object element (where Ensure
// and the placed inserts start their walk) and the objects a path must
// not step into.
var pptx = core.Format{
	Vocab: vocab,
	Units: units{},
	Type:  objectType,
	Bound: func(e *tree.Node) bool { return isObject(e) || isAC(e) },
}

// New returns a workspace over pkg.
func New(pkg *opc.Package) *Workspace {
	return &Workspace{pkg: pkg, core: core.New(pkg, pptx), aliases: map[Alias]Ref{}, nextID: map[string]int64{}}
}

// partKey returns the canonical key for a part name.
func partKey(name string) string { return plan.PartKey(name) }

// Slides returns the slide part names in presentation order.
func (w *Workspace) Slides() ([]string, error) {
	if w.listed {
		return w.slides, nil
	}
	rels, err := w.pkg.Rels("/")
	if err != nil {
		return nil, err
	}
	pres := ""
	for _, r := range rels.All() {
		if ctypes.Is(r.Type, ctypes.RelOfficeDocument) && !r.External() {
			if pres, err = rels.TargetPart(r); err != nil {
				return nil, err
			}
			break
		}
	}
	if pres == "" {
		return nil, fmt.Errorf("edit: no presentation part")
	}
	doc, err := w.tree(pres)
	if err != nil {
		return nil, err
	}
	prels, err := w.pkg.Rels(pres)
	if err != nil {
		return nil, err
	}
	lst := derive.Child(doc.Root(), derive.PML, "sldIdLst")
	if lst == nil {
		w.listed = true
		return nil, nil
	}
	for _, id := range lst.Elements() {
		rid, ok := derive.AttrNS(id, derive.REL, "id")
		if !ok {
			continue
		}
		r, ok := prels.Get(rid)
		if !ok {
			continue
		}
		name, err := prels.TargetPart(r)
		if err != nil || !w.pkg.Has(name) {
			continue
		}
		w.slides = append(w.slides, name)
	}
	w.listed = true
	return w.slides, nil
}

// SlidePart maps a slide argument (slide3, slide3.xml, or the part name
// with or without its leading slash) to a slide part in the slide list.
func (w *Workspace) SlidePart(arg string) (string, error) {
	slides, err := w.Slides()
	if err != nil {
		return "", err
	}
	for _, s := range slides {
		short := strings.TrimSuffix(path.Base(s), ".xml")
		if strings.EqualFold(arg, short) || strings.EqualFold(arg, short+".xml") || partKey(arg) == partKey(s) {
			return s, nil
		}
	}
	return "", fmt.Errorf("%w: no slide %q in the presentation's slide list", ErrBadAddress, arg)
}

// tree returns the parsed document for a package part.
func (w *Workspace) tree(part string) (*tree.Document, error) { return w.core.Tree(part) }

// spTree returns a slide's shape tree.
func (w *Workspace) spTree(part string) (*tree.Document, *tree.Node, error) {
	doc, err := w.tree(part)
	if err != nil {
		return nil, nil, err
	}
	st := derive.Path(doc.Root(), derive.PML, "cSld", "spTree")
	if st == nil {
		return nil, nil, fmt.Errorf("edit: %s has no p:cSld/p:spTree", part)
	}
	return doc, st, nil
}

// Resolve resolves a full address (slide3#17, slide3#17~2) against the
// current working state.
func (w *Workspace) Resolve(addr string) (Ref, error) {
	slide, id, ok := strings.Cut(addr, "#")
	if !ok {
		return Ref{}, fmt.Errorf("%w: %q has no #", ErrBadAddress, addr)
	}
	part, err := w.SlidePart(slide)
	if err != nil {
		return Ref{}, err
	}
	return w.ResolveIn(part, id)
}

// ResolveIn resolves an object id as the views show it (s17, s17~2; the
// s is optional) on the slide part, or an alias ($cache) bound earlier.
func (w *Workspace) ResolveIn(part, id string) (Ref, error) {
	if a, ok := ParseAlias(id); ok {
		return w.Lookup(a)
	}
	raw, ord, err := parseID(id)
	if err != nil {
		return Ref{}, err
	}
	if _, err := w.find(part, raw, ord); err != nil {
		return Ref{}, err
	}
	return Ref{part: part, id: raw, ord: ord}, nil
}

// Kind returns the local name of the projected element ref addresses
// (sp, cxnSp, grpSp, pic, graphicFrame, contentPart), without declaring
// anything.
func (w *Workspace) Kind(ref Ref) (string, error) {
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return "", err
	}
	return e.Local(), nil
}

// find returns the projected element of the ord-th object with cNvPr id
// raw on the slide.
func (w *Workspace) find(part, raw string, ord int) (*tree.Node, error) {
	_, st, err := w.spTree(part)
	if err != nil {
		return nil, err
	}
	n := 0
	var found *tree.Node
	walkProjected(st, func(e *tree.Node) bool {
		if objectID(e) == raw {
			n++
			if n == ord {
				found = e
				return false
			}
		}
		return true
	})
	if found == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoObject, Ref{part: part, id: raw, ord: ord})
	}
	return found, nil
}

// ErrAmbiguous is returned when a glue id names more than one object.
var ErrAmbiguous = errors.New("edit: ambiguous glue id")

// ResolveGlue resolves a connector glue id as written (stCxn/endCxn id) on
// the slide. Glue to an id shared by several objects is ambiguous and is
// never resolved to the first: it fails with ErrAmbiguous naming every
// candidate, so the caller must choose an explicit address.
func (w *Workspace) ResolveGlue(part, raw string) (Ref, error) {
	_, st, err := w.spTree(part)
	if err != nil {
		return Ref{}, err
	}
	n := 0
	walkProjected(st, func(e *tree.Node) bool {
		if objectID(e) == raw {
			n++
		}
		return true
	})
	switch n {
	case 0:
		return Ref{}, fmt.Errorf("%w: no object with id %s on %s (dangling glue)", ErrNoObject, raw, part)
	case 1:
		return Ref{part: part, id: raw, ord: 1}, nil
	}
	var c []string
	for k := 1; k <= n; k++ {
		c = append(c, Ref{part: part, id: raw, ord: k}.Short())
	}
	return Ref{}, fmt.Errorf("%w: id %s is shared by %s", ErrAmbiguous, raw, strings.Join(c, ", "))
}

// Bind names ref with alias a for the rest of the plan. An alias is bound
// once.
func (w *Workspace) Bind(a Alias, ref Ref) error {
	if _, dup := w.aliases[a]; dup {
		return fmt.Errorf("edit: alias $%s is already bound", a)
	}
	if ref.IsZero() {
		return fmt.Errorf("edit: alias $%s bound to nothing", a)
	}
	w.aliases[a] = ref
	return nil
}

// Aliases returns a copy of the aliases bound so far.
func (w *Workspace) Aliases() map[Alias]Ref {
	out := make(map[Alias]Ref, len(w.aliases))
	maps.Copy(out, w.aliases)
	return out
}

// Lookup returns the Ref bound to alias a.
func (w *Workspace) Lookup(a Alias) (Ref, error) {
	r, ok := w.aliases[a]
	if !ok {
		return Ref{}, fmt.Errorf("%w: alias $%s is not bound (yet)", ErrNoObject, a)
	}
	return r, nil
}

// NextID allocates a new object id on the slide: one more than the
// largest id of any cNvPr in the part (every mc:AlternateContent branch,
// every group, any namespace), of any connector glue reference (so a
// dangling stCxn or endCxn never becomes glued to a new object), or
// allocated earlier in the plan. Existing ids are never reused or
// renumbered.
func (w *Workspace) NextID(part string) (string, error) {
	k := partKey(part)
	next, ok := w.nextID[k]
	if !ok {
		doc, err := w.tree(part)
		if err != nil {
			return "", err
		}
		var maxID int64
		doc.Root().Walk(func(n *tree.Node) bool {
			if n.Kind() == tree.ElementNode && (n.Local() == "cNvPr" || n.Local() == "stCxn" || n.Local() == "endCxn") {
				if s, ok := n.Attr("id"); ok {
					if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && v > maxID {
						maxID = v
					}
				}
			}
			return true
		})
		next = maxID + 1
	}
	if next > math.MaxUint32 { // ST_DrawingElementId is xsd:unsignedInt
		return "", fmt.Errorf("edit: %s: no free object id", part)
	}
	w.nextID[k] = next + 1
	return strconv.FormatInt(next, 10), nil
}

// objectKinds are the local names of slide objects; all but contentPart
// must be in the PresentationML namespace.
var objectKinds = map[string]bool{"sp": true, "cxnSp": true, "grpSp": true, "pic": true, "graphicFrame": true, "contentPart": true}

// isObject reports whether e is a slide object element.
func isObject(e *tree.Node) bool {
	return e.Kind() == tree.ElementNode && objectKinds[e.Local()] && (derive.In(e, derive.PML) || e.Local() == "contentPart")
}

// isGroup reports whether e is a PresentationML group element.
func isGroup(e *tree.Node) bool { return derive.Is(e, derive.PML, "grpSp") }

// isAC reports whether e is an mc:AlternateContent element.
func isAC(e *tree.Node) bool { return derive.Is(e, derive.MC, "AlternateContent") }

// objectID returns the cNvPr id of an object element as written.
func objectID(e *tree.Node) string {
	for _, c := range e.Elements() {
		if strings.HasPrefix(c.Local(), "nv") {
			for _, cc := range c.Elements() {
				if cc.Local() == "cNvPr" {
					id, _ := cc.Attr("id")
					return id
				}
			}
			return ""
		}
	}
	return ""
}

// walkProjected visits the objects under container in document order,
// descending into groups and into the projected branch of each
// mc:AlternateContent, as the read model does. fn returns false to stop.
func walkProjected(container *tree.Node, fn func(*tree.Node) bool) bool {
	for _, e := range container.Elements() {
		if isAC(e) {
			b := derive.SelectBranch(e)
			if b.Node != nil && !walkProjected(b.Node, fn) {
				return false
			}
			continue
		}
		if !isObject(e) {
			continue
		}
		if !fn(e) {
			return false
		}
		if isGroup(e) && !walkProjected(e, fn) {
			return false
		}
	}
	return true
}

// Peek returns a Handle on the object at ref for reading only: unlike
// Target it declares nothing, so any change made through it fails
// Verify. Operations use it to read objects they do not write (the
// children whose boxes a new group frame encloses).
func (w *Workspace) Peek(ref Ref) (*Handle, error) {
	e, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return nil, err
	}
	br, err := counterparts(e)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}
	return &Handle{Handle: w.core.Peek(ref.part, ref.String(), br), w: w, ref: ref}, nil
}

// Children returns the addresses of the group's direct child objects in
// paint order (the projected branch of a child mc:AlternateContent), as
// the read model lists them.
func (w *Workspace) Children(ref Ref) ([]Ref, error) {
	g, err := w.find(ref.part, ref.id, ref.ord)
	if err != nil {
		return nil, err
	}
	if !isGroup(g) {
		return nil, fmt.Errorf("edit: %s is not a group", ref)
	}
	direct := map[*tree.Node]bool{}
	for _, e := range g.Elements() {
		switch {
		case isAC(e):
			if b := derive.SelectBranch(e); b.Node != nil {
				for _, c := range b.Node.Elements() {
					if isObject(c) {
						direct[c] = true
					}
				}
			}
		case isObject(e):
			direct[e] = true
		}
	}
	_, st, err := w.spTree(ref.part)
	if err != nil {
		return nil, err
	}
	var out []Ref
	ords := map[string]int{}
	walkProjected(st, func(e *tree.Node) bool {
		id := objectID(e)
		ords[id]++
		if direct[e] {
			out = append(out, Ref{part: ref.part, id: id, ord: ords[id]})
		}
		return true
	})
	return out, nil
}
