package opc

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// PackageRelsName is the part name of the package-level relationships.
const PackageRelsName = "/_rels/.rels"

// Relationship is one <Relationship> entry.
type Relationship struct {
	ID         string
	Type       string
	Target     string
	TargetMode string // "External", or "" for internal targets
}

// External reports whether the relationship targets something outside the
// package.
func (r Relationship) External() bool { return r.TargetMode == "External" }

// Rels is the relationships part of one source part, edited through its
// tree. A source without a relationships part has an empty Rels; the part is
// created on the first Add.
type Rels struct {
	pkg    *Package
	source string // source part name, "/" for the package
	name   string // relationships part name
	doc    *tree.Document
	// next caches NextID after Add: the id it returns while the
	// relationship Add added is still the root's last element (the
	// highest id is then still that one's).
	next nextID
}

// nextID is a cached NextID and the element it holds for.
type nextID struct {
	id   int
	last *tree.Node // the relationship Add added with id-1
}

// lastElement reports whether no element follows e among its siblings.
func lastElement(e *tree.Node) bool {
	for s := e.NextSibling(); s != nil; s = s.NextSibling() {
		if s.Kind() == tree.ElementNode {
			return false
		}
	}
	return true
}

// RelsName returns the relationships part name for a source part ("/" for
// the package itself).
func RelsName(source string) string {
	if source == "/" || source == "" {
		return PackageRelsName
	}
	dir, file := path.Split(source)
	return dir + "_rels/" + file + ".rels"
}

// Rels returns the relationships of source ("/" for the package).
func (p *Package) Rels(source string) (*Rels, error) {
	if source != "/" && !strings.HasPrefix(source, "/") {
		source = "/" + source
	}
	key := PartKey(source)
	if r, ok := p.rels[key]; ok {
		return r, nil
	}
	r := &Rels{pkg: p, source: source, name: RelsName(source)}
	if p.Has(r.name) {
		pt, err := p.Part(r.name)
		if err != nil {
			return nil, err
		}
		if r.doc, err = pt.Tree(); err != nil {
			return nil, err
		}
	}
	p.rels[key] = r
	return r, nil
}

// Name returns the relationships part name.
func (r *Rels) Name() string { return r.name }

// elements returns the Relationship elements of the relationships document, or nil without one.
func (r *Rels) elements() []*tree.Node {
	if r.doc == nil {
		return nil
	}
	root := r.doc.Root()
	var out []*tree.Node
	for _, e := range root.Elements() {
		if e.Is(root.Space(), "Relationship") {
			out = append(out, e)
		}
	}
	return out
}

// relOf converts a Relationship element into its Relationship value.
func relOf(e *tree.Node) Relationship {
	var r Relationship
	r.ID, _ = e.Attr("Id")
	r.Type, _ = e.Attr("Type")
	r.Target, _ = e.Attr("Target")
	r.TargetMode, _ = e.Attr("TargetMode")
	return r
}

// All returns the relationships in document order.
func (r *Rels) All() []Relationship {
	var out []Relationship
	for _, e := range r.elements() {
		out = append(out, relOf(e))
	}
	return out
}

// Get returns the relationship with the given id.
func (r *Rels) Get(id string) (Relationship, bool) {
	for _, e := range r.elements() {
		if v, _ := e.Attr("Id"); v == id {
			return relOf(e), true
		}
	}
	return Relationship{}, false
}

// ByType returns the relationships of the given type.
func (r *Rels) ByType(relType string) []Relationship {
	var out []Relationship
	for _, rel := range r.All() {
		if rel.Type == relType {
			out = append(out, rel)
		}
	}
	return out
}

// TargetPart resolves an internal relationship's target to a part name.
func (r *Rels) TargetPart(rel Relationship) (string, error) {
	if rel.External() {
		return "", fmt.Errorf("opc: relationship %s is external", rel.ID)
	}
	t := rel.Target
	if i := strings.IndexAny(t, "#?"); i >= 0 {
		t = t[:i]
	}
	if strings.HasPrefix(t, "/") {
		return path.Clean(t), nil
	}
	base := "/"
	if r.source != "/" {
		base = path.Dir(r.source)
	}
	return path.Join(base, t), nil
}

// NextID returns the next free id of the form rIdN: one more than the
// largest N in use, so an id is never reused within a part.
func (r *Rels) NextID() string {
	if r.doc != nil {
		if c := r.next; c.id > 0 && c.last != nil && c.last.Parent() == r.doc.Root() && lastElement(c.last) {
			return "rId" + strconv.Itoa(c.id)
		}
	}
	highest := 0
	for _, e := range r.elements() {
		id, _ := e.Attr("Id")
		if n, err := strconv.Atoi(strings.TrimPrefix(id, "rId")); err == nil && strings.HasPrefix(id, "rId") && n > highest {
			highest = n
		}
	}
	return "rId" + strconv.Itoa(highest+1)
}

// Add appends a relationship with the next free id and returns the id. An
// external target gets TargetMode="External".
func (r *Rels) Add(relType, target string, external bool) (string, error) {
	if r.doc == nil {
		if err := r.create(); err != nil {
			return "", err
		}
	}
	root := r.doc.Root()
	id := r.NextID()
	el, err := tree.NewElement(root.Space(), root.Prefix(), "Relationship")
	if err != nil {
		return "", err
	}
	attrs := [][2]string{{"Id", id}, {"Type", relType}, {"Target", target}}
	if external {
		attrs = append(attrs, [2]string{"TargetMode", "External"})
	}
	for _, a := range attrs {
		if err := el.SetAttr(a[0], a[1]); err != nil {
			return "", err
		}
	}
	if err := insertAfterLast(root, el, func(e *tree.Node) bool { return e.Is(root.Space(), "Relationship") }); err != nil {
		return "", err
	}
	// The next id is one past this one when this one was the highest (it
	// was: NextID gave it), so Add after Add need not rescan.
	if n, err := strconv.Atoi(strings.TrimPrefix(id, "rId")); err == nil {
		r.next = nextID{id: n + 1, last: el}
	}
	return id, nil
}

// Remove deletes the relationship with the given id. It reports whether it
// existed.
func (r *Rels) Remove(id string) bool {
	for _, e := range r.elements() {
		if v, _ := e.Attr("Id"); v == id {
			return e.Remove() == nil
		}
	}
	return false
}

// create makes an empty relationships part modelled on the package's own
// /_rels/.rels: the same prolog and root element with its namespace
// declarations, and no children.
func (r *Rels) create() error {
	if r.source == "/" {
		return errors.New("opc: package has no /_rels/.rels")
	}
	if err := r.pkg.RegistersTypes(); err != nil {
		return err // refused before the part is added
	}
	pkgRels, err := r.pkg.Rels("/")
	if err != nil {
		return err
	}
	if pkgRels.doc == nil {
		return errors.New("opc: cannot create a relationships part: package has no /_rels/.rels to model it on")
	}
	var b bytes.Buffer
	root := pkgRels.doc.Root()
	for _, c := range pkgRels.doc.Node().Children() {
		if c == root {
			break
		}
		b.Write(c.Raw())
	}
	b.WriteString("<" + root.Name())
	for _, a := range root.Attrs() {
		if a.Name == "xmlns" || strings.HasPrefix(a.Name, "xmlns:") {
			b.WriteString(" " + a.Name + `="`)
			b.Write(tree.EscapeText(a.Value))
			b.WriteString(`"`)
		}
	}
	b.WriteString("></" + root.Name() + ">")
	pt, err := r.pkg.AddPart(r.name, b.Bytes(), "")
	if err != nil {
		return err
	}
	ct, err := r.pkg.ContentTypes()
	if err != nil {
		return err
	}
	if _, ok := ct.Lookup(r.name); !ok {
		pkgType, ok := ct.Lookup(PackageRelsName)
		if !ok {
			return errors.New("opc: no content type for relationships parts")
		}
		if err := ct.SetOverride(r.name, pkgType); err != nil {
			return err
		}
	}
	r.doc, err = pt.Tree()
	return err
}
