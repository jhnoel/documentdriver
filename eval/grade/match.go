package grade

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
)

// Matching pairs input objects with output objects.
//
// Identity rules, in order:
//
//  1. Same short id (cNvPr id plus duplicate suffix) and same kind, and
//     either the same name or an input object the task targets. Tools
//     that keep ids (pptxdriver, python-pptx, hand edits) match here.
//  2. Otherwise, the same signature: kind, name and plain text. This
//     recovers objects a tool renumbered, and never pairs two different
//     objects that merely share an id.
//
// Input objects left over were deleted; output objects left over are new.
type Matching struct {
	In, Out     *model.Slide
	inNS, outNS Scope
	isIn        map[*model.Object]bool
	InToOut     map[string]*model.Object // input short id -> output object
	OutToIn     map[*model.Object]*model.Object
	ByID        int
	BySignature int
	Deleted     []*model.Object // input objects with no output match
	New         []*model.Object // output objects with no input match
}

// signature returns the matching signature of kind, name and text.
func (m *Matching) signature(o *model.Object) string {
	return string(o.Kind) + "\x00" + o.Name + "\x00" + m.Text(o)
}

// Scope maps the namespace prefixes declared on a slide part's root to
// their URIs. Object XML comes back as bytes, without its ancestors'
// declarations, so grading re-supplies them from the slide it came from.
type Scope map[string]string

// ScopeOf is the scope of a parsed slide part.
func ScopeOf(d *tree.Document) Scope {
	s := Scope{}
	if d == nil || d.Root() == nil {
		return s
	}
	for _, a := range d.Root().Attrs() {
		switch {
		case a.Name == "xmlns":
			s[""] = a.Value
		case strings.HasPrefix(a.Name, "xmlns:"):
			s[a.Name[len("xmlns:"):]] = a.Value
		}
	}
	return s
}

// parseXML parses element bytes under the scope's declarations and returns
// the element, or nil if there are no bytes or they do not parse.
func parseXML(b []byte, ns Scope) *tree.Node {
	if len(b) == 0 {
		return nil
	}
	var w bytes.Buffer
	w.WriteString("<w")
	keys := slices.Sorted(maps.Keys(ns))
	for _, k := range keys {
		name := "xmlns"
		if k != "" {
			name += ":" + k
		}
		fmt.Fprintf(&w, " %s=%q", name, ns[k])
	}
	w.WriteString(">")
	w.Write(b)
	w.WriteString("</w>")
	doc, err := tree.Parse(w.Bytes())
	if err != nil {
		return nil
	}
	els := doc.Root().Elements()
	if len(els) != 1 {
		return nil
	}
	return els[0]
}

// Text is an object's plain text; see objText.
func (m *Matching) Text(o *model.Object) string {
	ns := m.outNS
	if m.isIn[o] {
		ns = m.inNS
	}
	return objText(o, ns)
}

// Match pairs the objects of in and out. targets are input short ids.
// inNS and outNS are the scopes of the two slide parts.
func Match(in, out *model.Slide, inNS, outNS Scope, targets map[string]bool) *Matching {
	m := &Matching{In: in, Out: out, inNS: inNS, outNS: outNS, isIn: map[*model.Object]bool{}, InToOut: map[string]*model.Object{}, OutToIn: map[*model.Object]*model.Object{}}
	for _, o := range in.Objects {
		m.isIn[o] = true
	}
	outByID := map[string]*model.Object{}
	for _, o := range out.Objects {
		outByID[o.ID] = o
	}
	var rest []*model.Object
	for _, o := range in.Objects {
		c := outByID[o.ID]
		if c != nil && c.Kind == o.Kind && (c.Name == o.Name || targets[o.ID]) && m.OutToIn[c] == nil {
			m.pair(o, c)
			m.ByID++
			continue
		}
		rest = append(rest, o)
	}
	sigs := map[string][]*model.Object{}
	for _, o := range out.Objects {
		if m.OutToIn[o] == nil {
			sigs[m.signature(o)] = append(sigs[m.signature(o)], o)
		}
	}
	for _, o := range rest {
		s := m.signature(o)
		if len(sigs[s]) > 0 {
			m.pair(o, sigs[s][0])
			sigs[s] = sigs[s][1:]
			m.BySignature++
			continue
		}
		m.Deleted = append(m.Deleted, o)
	}
	for _, o := range out.Objects {
		if m.OutToIn[o] == nil {
			m.New = append(m.New, o)
		}
	}
	return m
}

// pair records in and out as a matched input-output pair.
func (m *Matching) pair(in, out *model.Object) {
	m.InToOut[in.ID] = out
	m.OutToIn[out] = in
}

// InID is the input short id of an output object, or "" for a new one.
func (m *Matching) InID(o *model.Object) string {
	if in := m.OutToIn[o]; in != nil {
		return in.ID
	}
	return ""
}

// OutByID finds an output object by its output short id.
func (m *Matching) OutByID(id string) *model.Object {
	for _, o := range m.Out.Objects {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// Label names an output object for reports: its input id, or new:<id>.
func (m *Matching) Label(o *model.Object) string {
	if id := m.InID(o); id != "" {
		return id
	}
	return "new:" + o.ID
}

// objText is an object's plain text: its own a:t runs, paragraphs joined
// by newlines, whitespace collapsed within each paragraph. Group text is
// not collected from children.
func objText(o *model.Object, ns Scope) string {
	node := parseXML(o.ElementXML(), ns)
	if node == nil {
		return ""
	}
	var body *tree.Node
	for c := range node.ElementsSeq() {
		if c.Local() == "txBody" {
			body = c
		}
	}
	if body == nil {
		return ""
	}
	var paras []string
	for p := range body.ElementsSeq() {
		if !derive.Is(p, derive.DML, "p") {
			continue
		}
		var b strings.Builder
		p.Walk(func(e *tree.Node) bool {
			if derive.Is(e, derive.DML, "t") {
				b.WriteString(e.Text())
			}
			return true
		})
		paras = append(paras, strings.Join(strings.Fields(b.String()), " "))
	}
	return strings.TrimSpace(strings.Join(paras, "\n"))
}

// objectElem reports whether n is an element that holds an object in an
// spTree or grpSp: shapes, connectors, groups, pictures, frames, content
// parts and the mc:AlternateContent wrapping one.
func objectElem(n *tree.Node) bool {
	if n.Kind() != tree.ElementNode {
		return false
	}
	switch n.Local() {
	case "sp", "cxnSp", "grpSp", "pic", "graphicFrame", "contentPart":
		return derive.In(n, derive.PML)
	case "AlternateContent":
		return derive.In(n, derive.MC)
	}
	return false
}

// Canonical writes a namespace-resolved, order-preserving form of n:
// element and attribute names as {uri}local, attributes sorted, namespace
// declarations dropped, whitespace-only text dropped and other text
// trimmed. With stripObjects, object elements inside an spTree or grpSp
// are left out, so a group compares by its own properties and a slide by
// its skeleton. Two XML serialisations of the same content compare equal.
func Canonical(n *tree.Node, stripObjects bool) []byte {
	var b bytes.Buffer
	canon(&b, n, stripObjects)
	return b.Bytes()
}

// canon writes n's canonical form into b, stripping objects when strip is set.
func canon(b *bytes.Buffer, n *tree.Node, strip bool) {
	switch n.Kind() {
	case tree.DocumentNode:
		for c := range n.ChildrenSeq() {
			if c.Kind() == tree.ElementNode {
				canon(b, c, strip)
			}
		}
	case tree.ElementNode:
		b.WriteString("<{" + n.Space() + "}" + n.Local())
		var attrs []string
		for _, a := range n.Attrs() {
			if a.Name == "xmlns" || strings.HasPrefix(a.Name, "xmlns:") {
				continue
			}
			name := a.Name
			if i := strings.IndexByte(name, ':'); i >= 0 {
				uri, _ := n.LookupPrefix(name[:i])
				name = "{" + uri + "}" + name[i+1:]
			}
			attrs = append(attrs, name+"="+fmt.Sprintf("%q", a.Value))
		}
		sort.Strings(attrs)
		for _, a := range attrs {
			b.WriteString(" " + a)
		}
		b.WriteString(">")
		// Text is kept exactly in a:t and similar leaf runs, and trimmed
		// elsewhere, where whitespace between elements is formatting.
		if t := n.Text(); n.Local() == "t" {
			fmt.Fprintf(b, "%q", t)
		} else if t = strings.TrimSpace(t); t != "" {
			fmt.Fprintf(b, "%q", t)
		}
		container := n.Local() == "spTree" || n.Local() == "grpSp"
		for c := range n.ElementsSeq() {
			if strip && container && objectElem(c) {
				continue
			}
			canon(b, c, strip)
		}
		b.WriteString("</>")
	}
}

// ownSame compares an input object with its output counterpart: bytes
// first, then canonical XML; for a group, the group without its children.
// ains and bns are the scopes of the slides a and b came from.
func ownSame(a, b *model.Object, ains, bns Scope) (same, reserialised bool) {
	xa, xb := a.XML(), b.XML()
	if xa == nil || xb == nil {
		return xa == nil && xb == nil, false
	}
	if a.Kind != model.Group && bytes.Equal(xa, xb) {
		return true, false
	}
	na, nb := parseXML(xa, ains), parseXML(xb, bns)
	if na == nil || nb == nil {
		return false, false
	}
	ca, cb := Canonical(na, true), Canonical(nb, true)
	if !bytes.Equal(ca, cb) {
		return false, false
	}
	if a.Kind == model.Group {
		// Compare the group's own bytes with children removed only
		// canonically; a byte difference there is a children change.
		return true, false
	}
	return true, true
}

// reordered counts matched objects whose relative paint order changed:
// the objects outside a longest increasing run of output positions, taken
// in input order. Only objects in skip=false are counted.
func reordered(m *Matching, skip map[string]bool) []string {
	pos := map[*model.Object]int{}
	for i, o := range m.Out.Objects {
		pos[o] = i
	}
	var ids []string
	var seq []int
	for _, o := range m.In.Objects {
		if c := m.InToOut[o.ID]; c != nil {
			ids = append(ids, o.ID)
			seq = append(seq, pos[c])
		}
	}
	keep := lis(seq)
	var out []string
	for i, id := range ids {
		if !keep[i] && !skip[id] {
			out = append(out, id)
		}
	}
	return out
}

// lis marks one longest strictly increasing subsequence of s.
func lis(s []int) []bool {
	n := len(s)
	tails := []int{} // indices into s
	prev := make([]int, n)
	for i := range s {
		j := sort.Search(len(tails), func(k int) bool { return s[tails[k]] >= s[i] })
		if j > 0 {
			prev[i] = tails[j-1]
		} else {
			prev[i] = -1
		}
		if j == len(tails) {
			tails = append(tails, i)
		} else {
			tails[j] = i
		}
	}
	keep := make([]bool, n)
	if len(tails) == 0 {
		return keep
	}
	for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
		keep[i] = true
	}
	return keep
}
