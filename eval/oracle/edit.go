// Package oracle makes reference outputs for the evaluation tasks. Each
// reference edits the slide XML directly through internal/opc and
// internal/ooxml/tree, and reads positions and connection sites from the
// read model. It deliberately does not use pptxdriver's write operations,
// so the graders are tested against edits made independently of them.
package oracle

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
)

// Namespaces used in fragments: transitional PresentationML and DrawingML,
// the namespaces of every task deck's slides.
var ns = map[string]string{
	"p": "http://schemas.openxmlformats.org/presentationml/2006/main",
	"a": "http://schemas.openxmlformats.org/drawingml/2006/main",
}

const emu = model.EMUPerPoint

// Ed edits one slide of an open package.
type Ed struct {
	Pkg   *opc.Package
	deck  *model.Deck
	part  string
	Slide *model.Slide
	doc   *tree.Document
	// added records raw ids of objects the editor created.
	added map[string]bool
}

// Open opens a deck for editing one slide.
func Open(path, slide string) (*Ed, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	e := &Ed{Pkg: p, added: map[string]bool{}}
	if e.deck, err = model.OpenDeck(p); err != nil {
		p.Close()
		return nil, err
	}
	if e.part, err = e.deck.ResolveSlide(slide); err != nil {
		p.Close()
		return nil, err
	}
	pt, err := p.Part(e.part)
	if err != nil {
		p.Close()
		return nil, err
	}
	if e.doc, err = pt.Tree(); err != nil {
		p.Close()
		return nil, err
	}
	return e, e.Refresh()
}

// Refresh re-reads the slide model from the edited tree.
func (e *Ed) Refresh() error {
	s, err := e.deck.ReadSlide(e.part)
	if err != nil {
		return err
	}
	e.Slide = s
	return nil
}

// SaveAs writes the edited package.
func (e *Ed) SaveAs(path string) error { return e.Pkg.SaveAs(path) }

// Close releases the package.
func (e *Ed) Close() error { return e.Pkg.Close() }

// Obj finds an object by short id.
func (e *Ed) Obj(id string) (*model.Object, error) {
	for _, o := range e.Slide.Objects {
		if o.ID == id {
			return o, nil
		}
	}
	return nil, fmt.Errorf("oracle: no object %s on %s", id, e.part)
}

// ObjByText finds the first object whose text contains s.
func (e *Ed) ObjByText(s string) (*model.Object, error) {
	for _, o := range e.Slide.Objects {
		if o.Text != nil && strings.Contains(stripTags(o.Text.HTML), s) {
			return o, nil
		}
	}
	return nil, fmt.Errorf("oracle: no object with text %q", s)
}

// stripTags returns h with HTML tags removed.
func stripTags(h string) string {
	var b strings.Builder
	in := false
	for _, r := range h {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// child returns the first element child of n with the given local name.
func child(n *tree.Node, local string) *tree.Node {
	for _, c := range n.Elements() {
		if c.Local() == local {
			return c
		}
	}
	return nil
}

// elem finds an object's element in the editor's own parsed slide tree,
// walking the shape tree the way PowerPoint reads it: within an
// mc:AlternateContent only the branch derive.SelectBranch picks. It
// returns the element and its source (the enclosing top-level
// mc:AlternateContent when the object sits in one, else the element). The
// oracle looks nodes up itself, rather than through model.Object, so its
// edits stay independent of the read model and of pptxdriver's write path.
func (e *Ed) elem(id string) (node, source *tree.Node, err error) {
	seen := map[string]int{}
	var walk func(container, src *tree.Node) bool
	walk = func(container, src *tree.Node) bool {
		for _, c := range container.Elements() {
			if derive.Is(c, derive.MC, "AlternateContent") {
				b := derive.SelectBranch(c)
				if b.Node == nil {
					continue
				}
				s := src
				if s == nil {
					s = c
				}
				if walk(b.Node, s) {
					return true
				}
				continue
			}
			if !(derive.In(c, derive.PML)) {
				continue
			}
			switch c.Local() {
			case "sp", "cxnSp", "grpSp", "pic", "graphicFrame", "contentPart":
			default:
				continue
			}
			raw := ""
			for _, nv := range c.Elements() {
				if strings.HasPrefix(nv.Local(), "nv") {
					if p := child(nv, "cNvPr"); p != nil {
						raw, _ = p.Attr("id")
					}
					break
				}
			}
			seen[raw]++
			sid := "s" + raw
			if n := seen[raw]; n > 1 {
				sid += "~" + strconv.Itoa(n)
			}
			if sid == id {
				node, source = c, src
				if source == nil {
					source = c
				}
				return true
			}
			if c.Local() == "grpSp" && walk(c, src) {
				return true
			}
		}
		return false
	}
	if st := e.spTree(); st != nil && walk(st, nil) {
		return node, source, nil
	}
	return nil, nil, fmt.Errorf("oracle: no element for object %s on %s", id, e.part)
}

// spPr is an object's shape properties element.
func (e *Ed) spPr(o *model.Object) (*tree.Node, error) {
	node, _, err := e.elem(o.ID)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"spPr", "grpSpPr"} {
		if n := child(node, name); n != nil {
			return n, nil
		}
	}
	return nil, fmt.Errorf("oracle: %s has no spPr", o.ID)
}

var fillNames = map[string]bool{"noFill": true, "solidFill": true, "gradFill": true, "blipFill": true, "pattFill": true, "grpFill": true}

// SetFill replaces an object's fill with a solid fill of colour, a colour
// element fragment such as <a:schemeClr val="accent2"/>.
func (e *Ed) SetFill(id, colour string) error {
	o, err := e.Obj(id)
	if err != nil {
		return err
	}
	sp, err := e.spPr(o)
	if err != nil {
		return err
	}
	frag := []byte("<a:solidFill>" + colour + "</a:solidFill>")
	for _, c := range sp.Elements() {
		if fillNames[c.Local()] {
			_, err := sp.ReplaceWithFragment(c, frag, ns)
			return err
		}
	}
	// No fill: it goes after the geometry (xfrm, custGeom/prstGeom).
	at := 0
	for i, c := range sp.Children() {
		if l := c.Local(); l == "xfrm" || l == "custGeom" || l == "prstGeom" {
			at = i + 1
		}
	}
	_, err = sp.InsertFragment(at, frag, ns)
	return err
}

// SetLine sets an object's line width (EMU) and solid colour.
func (e *Ed) SetLine(id string, w int, colour string) error {
	o, err := e.Obj(id)
	if err != nil {
		return err
	}
	sp, err := e.spPr(o)
	if err != nil {
		return err
	}
	ln := child(sp, "ln")
	if ln == nil {
		return fmt.Errorf("oracle: %s has no a:ln", id)
	}
	if err := ln.SetAttr("w", strconv.Itoa(w)); err != nil {
		return err
	}
	frag := []byte("<a:solidFill>" + colour + "</a:solidFill>")
	for _, c := range ln.Elements() {
		if fillNames[c.Local()] {
			_, err := ln.ReplaceWithFragment(c, frag, ns)
			return err
		}
	}
	_, err = ln.InsertFragment(0, frag, ns)
	return err
}

// xfrm is an object's own a:xfrm.
func (e *Ed) xfrm(o *model.Object) (*tree.Node, error) {
	sp, err := e.spPr(o)
	if err != nil {
		return nil, err
	}
	x := child(sp, "xfrm")
	if x == nil {
		return nil, fmt.Errorf("oracle: %s has no xfrm", o.ID)
	}
	return x, nil
}

// MoveTo sets a top-level object's top-left corner in points.
func (e *Ed) MoveTo(id string, xPt, yPt float64) error {
	o, err := e.Obj(id)
	if err != nil {
		return err
	}
	if o.Parent != "" {
		return fmt.Errorf("oracle: MoveTo on grouped %s", id)
	}
	x, err := e.xfrm(o)
	if err != nil {
		return err
	}
	off := child(x, "off")
	if err := off.SetAttr("x", strconv.Itoa(int(math.Round(xPt*emu)))); err != nil {
		return err
	}
	return off.SetAttr("y", strconv.Itoa(int(math.Round(yPt*emu))))
}

// MoveBy shifts a top-level object by EMU.
func (e *Ed) MoveBy(id string, dx, dy int64) error {
	o, err := e.Obj(id)
	if err != nil {
		return err
	}
	x, err := e.xfrm(o)
	if err != nil {
		return err
	}
	off := child(x, "off")
	for _, a := range []struct {
		name string
		d    int64
	}{{"x", dx}, {"y", dy}} {
		s, _ := off.Attr(a.name)
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		if err := off.SetAttr(a.name, strconv.FormatInt(v+a.d, 10)); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes an object (its mc:AlternateContent wrapper, if any).
func (e *Ed) Delete(id string) error {
	o, err := e.Obj(id)
	if err != nil {
		return err
	}
	_, src, err := e.elem(o.ID)
	if err != nil {
		return err
	}
	return src.Remove()
}

// nextID is one more than the largest cNvPr id on the slide.
func (e *Ed) nextID() int {
	maxID := 0
	e.doc.Root().Walk(func(n *tree.Node) bool {
		if n.Local() == "cNvPr" {
			if s, ok := n.Attr("id"); ok {
				if v, err := strconv.Atoi(s); err == nil && v > maxID {
					maxID = v
				}
			}
		}
		return true
	})
	return maxID + 1
}

// spTree is the slide's shape tree.
func (e *Ed) spTree() *tree.Node {
	return derive.Path(e.doc.Root(), derive.PML, "cSld", "spTree")
}

// appendObject adds an object fragment at the front of the paint order
// (before any extLst) and returns its short id after a refresh.
func (e *Ed) appendObject(frag string) error {
	st := e.spTree()
	at := len(st.Children())
	for i, c := range st.Children() {
		if c.Local() == "extLst" {
			at = i
		}
	}
	if _, err := st.InsertFragment(at, []byte(frag), ns); err != nil {
		return err
	}
	return e.Refresh()
}

// Shape describes a new shape.
type Shape struct {
	Name       string
	Geom       string     // preset name; "" with Path for custGeom
	Path       [][2]int64 // custGeom polygon in EMU relative to the box
	Box        [4]float64 // points
	Fill, Line string     // colour fragments; "" for none
	LineW      int        // EMU
	Text       string
	SizePt     float64
	TextColour string
	Anchor     string // bodyPr anchor; default ctr
}

// esc escapes s for embedding as XML text.
func esc(s string) string {
	return string(tree.EscapeText(s))
}

// AddShape appends a shape and returns its short id.
func (e *Ed) AddShape(s Shape) (string, error) {
	id := e.nextID()
	var g string
	if s.Geom != "" {
		g = `<a:prstGeom prst="` + s.Geom + `"><a:avLst/></a:prstGeom>`
	} else {
		w, h := int64(math.Round(s.Box[2]*emu)), int64(math.Round(s.Box[3]*emu))
		var p strings.Builder
		for i, q := range s.Path {
			tag := "lnTo"
			if i == 0 {
				tag = "moveTo"
			}
			fmt.Fprintf(&p, `<a:%s><a:pt x="%d" y="%d"/></a:%s>`, tag, q[0], q[1], tag)
		}
		g = fmt.Sprintf(`<a:custGeom><a:avLst/><a:gdLst/><a:ahLst/><a:cxnLst/><a:rect l="0" t="0" r="r" b="b"/><a:pathLst><a:path w="%d" h="%d">%s<a:close/></a:path></a:pathLst></a:custGeom>`, w, h, p.String())
	}
	fill := "<a:noFill/>"
	if s.Fill != "" {
		fill = "<a:solidFill>" + s.Fill + "</a:solidFill>"
	}
	line := "<a:ln><a:noFill/></a:ln>"
	if s.Line != "" {
		line = fmt.Sprintf(`<a:ln w="%d"><a:solidFill>%s</a:solidFill></a:ln>`, s.LineW, s.Line)
	}
	var text string
	{
		var paras strings.Builder
		for _, para := range strings.Split(s.Text, "\n") {
			col := ""
			if s.TextColour != "" {
				col = "<a:solidFill>" + s.TextColour + "</a:solidFill>"
			}
			if para == "" {
				paras.WriteString(`<a:p><a:pPr algn="ctr"/><a:endParaRPr lang="en-US"/></a:p>`)
				continue
			}
			fmt.Fprintf(&paras, `<a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="en-US" sz="%d" dirty="0">%s</a:rPr><a:t>%s</a:t></a:r></a:p>`, int(s.SizePt*100), col, esc(para))
		}
		anchor := s.Anchor
		if anchor == "" {
			anchor = "ctr"
		}
		text = `<p:txBody><a:bodyPr lIns="45720" tIns="45720" rIns="45720" bIns="45720" rtlCol="0" anchor="` + anchor + `"/><a:lstStyle/>` + paras.String() + `</p:txBody>`
	}
	frag := fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>%s%s%s</p:spPr>%s</p:sp>`,
		id, esc(fmt.Sprintf("%s %d", s.Name, id-1)), r(s.Box[0]), r(s.Box[1]), r(s.Box[2]), r(s.Box[3]), g, fill, line, text)
	if err := e.appendObject(frag); err != nil {
		return "", err
	}
	e.added[strconv.Itoa(id)] = true
	return "s" + strconv.Itoa(id), nil
}

// r converts points to EMU, rounded to the nearest EMU.
func r(p float64) int64 { return int64(math.Round(p * emu)) }

// Conn describes a new connector between two glued sites, or from one
// glued site to a free point.
type Conn struct {
	Geom       string // straightConnector1, bentConnector3, line
	From       string // short id
	FromSite   int
	To         string // short id; "" for a free end at ToPt
	ToSite     int
	ToPt       [2]float64 // points
	VertFirst  bool       // bent connectors: first segment vertical
	LineW      int
	Line       string // colour fragment
	ArrowAtEnd bool
}

// site finds a site's slide coordinates (EMU).
func (e *Ed) site(id string, idx int) (derive.Pt, string, error) {
	o, err := e.Obj(id)
	if err != nil {
		return derive.Pt{}, "", err
	}
	for _, s := range o.Sites {
		if s.Index == idx {
			return s.Pt, o.RawID, nil
		}
	}
	return derive.Pt{}, "", fmt.Errorf("oracle: %s has no site %d", id, idx)
}

// connXfrm computes a connector's xfrm for endpoints p1 -> p2 (EMU).
func connXfrm(p1, p2 derive.Pt, vert bool) string {
	if !vert {
		x, y := math.Min(p1.X, p2.X), math.Min(p1.Y, p2.Y)
		w, h := math.Abs(p2.X-p1.X), math.Abs(p2.Y-p1.Y)
		attrs := ""
		if p2.X < p1.X {
			attrs += ` flipH="1"`
		}
		if p2.Y < p1.Y {
			attrs += ` flipV="1"`
		}
		return fmt.Sprintf(`<a:xfrm%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, attrs, ri(x), ri(y), ri(w), ri(h))
	}
	// Rotated 90 degrees: local x runs down the slide. The start, local
	// (0,0) after flips, maps to centre + (fy*h/2, -fx*w/2).
	cx, cy := (p1.X+p2.X)/2, (p1.Y+p2.Y)/2
	h, w := math.Abs(p1.X-p2.X), math.Abs(p1.Y-p2.Y)
	attrs := ` rot="5400000"`
	if p1.Y > p2.Y {
		attrs += ` flipH="1"`
	}
	if p1.X < p2.X {
		attrs += ` flipV="1"`
	}
	return fmt.Sprintf(`<a:xfrm%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, attrs, ri(cx-w/2), ri(cy-h/2), ri(w), ri(h))
}

// ri rounds v to the nearest integer.
func ri(v float64) int64 { return int64(math.Round(v)) }

// AddConn appends a connector and returns its short id.
func (e *Ed) AddConn(c Conn) (string, error) {
	p1, raw1, err := e.site(c.From, c.FromSite)
	if err != nil {
		return "", err
	}
	glue := fmt.Sprintf(`<a:stCxn id="%s" idx="%d"/>`, raw1, c.FromSite)
	var p2 derive.Pt
	if c.To != "" {
		var raw2 string
		if p2, raw2, err = e.site(c.To, c.ToSite); err != nil {
			return "", err
		}
		glue += fmt.Sprintf(`<a:endCxn id="%s" idx="%d"/>`, raw2, c.ToSite)
	} else {
		p2 = derive.Pt{X: c.ToPt[0] * emu, Y: c.ToPt[1] * emu}
	}
	id := e.nextID()
	tail := ""
	if c.ArrowAtEnd {
		tail = `<a:tailEnd type="triangle"/>`
	}
	frag := fmt.Sprintf(`<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="%d" name="Connector %d"/><p:cNvCxnSpPr>%s</p:cNvCxnSpPr><p:nvPr/></p:nvCxnSpPr><p:spPr>%s<a:prstGeom prst="%s"><a:avLst/></a:prstGeom><a:ln w="%d"><a:solidFill>%s</a:solidFill>%s</a:ln></p:spPr></p:cxnSp>`,
		id, id-1, glue, connXfrm(p1, p2, c.VertFirst), c.Geom, c.LineW, c.Line, tail)
	if err := e.appendObject(frag); err != nil {
		return "", err
	}
	e.added[strconv.Itoa(id)] = true
	return "s" + strconv.Itoa(id), nil
}

// Reglue rewrites an existing connector's glue and box so that it runs
// between the given sites (To "" leaves the end free at ToPt). Rotation
// is dropped; flips encode direction.
func (e *Ed) Reglue(id string, c Conn) error {
	o, err := e.Obj(id)
	if err != nil {
		return err
	}
	p1, raw1, err := e.site(c.From, c.FromSite)
	if err != nil {
		return err
	}
	glue := fmt.Sprintf(`<a:stCxn id="%s" idx="%d"/>`, raw1, c.FromSite)
	var p2 derive.Pt
	if c.To != "" {
		var raw2 string
		if p2, raw2, err = e.site(c.To, c.ToSite); err != nil {
			return err
		}
		glue += fmt.Sprintf(`<a:endCxn id="%s" idx="%d"/>`, raw2, c.ToSite)
	} else {
		p2 = derive.Pt{X: c.ToPt[0] * emu, Y: c.ToPt[1] * emu}
	}
	node, _, err := e.elem(o.ID)
	if err != nil {
		return err
	}
	nv := child(node, "nvCxnSpPr")
	cnv := child(nv, "cNvCxnSpPr")
	for _, g := range cnv.Elements() {
		if g.Local() == "stCxn" || g.Local() == "endCxn" {
			if err := g.Remove(); err != nil {
				return err
			}
		}
	}
	if _, err := cnv.InsertFragment(len(cnv.Children()), []byte(glue), ns); err != nil {
		return err
	}
	x, err := e.xfrm(o)
	if err != nil {
		return err
	}
	if _, err := x.Parent().ReplaceWithFragment(x, []byte(connXfrm(p1, p2, c.VertFirst)), ns); err != nil {
		return err
	}
	return e.Refresh()
}

// Added reports whether the editor created the object with this raw id.
func (e *Ed) Added(rawID string) bool { return e.added[rawID] }
