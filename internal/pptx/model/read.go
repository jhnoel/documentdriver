package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// reader builds the model of one slide.
type reader struct {
	d     *Deck
	s     *Slide
	sld   *tree.Document
	rels  *opc.Rels
	lay   *tree.Node // layout spTree
	mst   *tree.Node // master spTree
	mroot *tree.Node // master root (p:sldMaster)

	cc    derive.ColorCtx // colour map, scheme and format scheme
	fonts map[string]string

	seen  map[string]int
	byRaw map[string][]*Object
	z     int
}

// link is one level of a placeholder's inheritance chain.
type link struct {
	sp     *tree.Node
	origin Origin
}

// ReadSlide builds the read model of the slide with the given part name.
// It only reads; nothing in the package is modified.
func (d *Deck) ReadSlide(part string) (*Slide, error) {
	pt, err := d.pkg.Part(part)
	if err != nil {
		return nil, err
	}
	doc, err := pt.Tree()
	if err != nil {
		return nil, err
	}
	r := &reader{d: d, sld: doc, seen: map[string]int{}, byRaw: map[string][]*Object{}}
	r.s = &Slide{Part: part, Name: ShortName(part), W: d.W, H: d.H}
	r.s.Omitted = append(r.s.Omitted, d.Omitted...)
	if r.rels, err = d.pkg.Rels(part); err != nil {
		return nil, err
	}
	if err := r.loadContext(part); err != nil {
		return nil, err
	}
	tree := derive.Path(doc.Root(), derive.PML, "cSld", "spTree")
	if tree == nil {
		r.s.Omitted = append(r.s.Omitted, Omission{"spTree", "slide has no p:cSld/p:spTree"})
		return r.s, nil
	}
	r.walk(tree, nil, derive.Root, nil, nil, nil)
	r.resolveGlue()
	r.hazards()
	return r.s, nil
}

// loadContext loads the layout, master and theme and resolves the colour
// map and theme fonts.
func (r *reader) loadContext(part string) error {
	// A layout, master or theme that cannot be read is a defect the
	// slide did not make: what it would supply is left out with a note,
	// and the slide's own objects are still read (docs/contract.md,
	// section 10).
	unreadable := func(what string, err error) {
		r.s.Omitted = append(r.s.Omitted, Omission{what, "unreadable: " + err.Error()})
	}
	layName, lay, err := r.d.related(part, ctypes.RelSlideLayout)
	if err != nil {
		unreadable("layout", err)
		layName, lay = "", nil
	}
	var mst *tree.Document
	mstNoted := false
	if lay != nil {
		r.lay = derive.Path(lay.Root(), derive.PML, "cSld", "spTree")
		if _, mst, err = r.d.related(layName, ctypes.RelSlideMaster); err != nil {
			unreadable("master", err)
			mst, mstNoted = nil, true
		}
	} else if err == nil {
		r.s.Omitted = append(r.s.Omitted, Omission{"layout", "slide has no layout relationship"})
	}
	var theme *tree.Document
	if mst != nil {
		r.mroot = mst.Root()
		r.mst = derive.Path(r.mroot, derive.PML, "cSld", "spTree")
		mName := ""
		if _, mName = r.masterName(layName); mName != "" {
			if _, theme, err = r.d.related(mName, ctypes.RelTheme); err != nil {
				unreadable("theme", err)
				theme = nil
			}
		}
	} else if lay != nil && !mstNoted {
		r.s.Omitted = append(r.s.Omitted, Omission{"master", "layout has no master relationship"})
	}
	// The colour map with the slide's or layout's override, the scheme
	// and the format scheme: the same context the property codecs use.
	r.cc = derive.SlideColorCtx(r.sld.Root(), rootOf(lay), rootOf(mst), rootOf(theme))
	r.fonts = map[string]string{}
	r.s.Fonts = r.fonts
	r.s.Colors = map[string]string{}
	if theme == nil {
		r.s.Omitted = append(r.s.Omitted, Omission{"theme", "no theme reachable from the slide's master"})
		return nil
	}
	te := derive.Child(theme.Root(), derive.DML, "themeElements")
	if fs := derive.Child(te, derive.DML, "fontScheme"); fs != nil {
		for _, f := range []struct{ el, ref string }{{"majorFont", "mj"}, {"minorFont", "mn"}} {
			fe := derive.Child(fs, derive.DML, f.el)
			for _, s := range []struct{ el, ref string }{{"latin", "lt"}, {"ea", "ea"}, {"cs", "cs"}} {
				if n := derive.Child(fe, derive.DML, s.el); n != nil {
					if tf, ok := n.Attr("typeface"); ok && tf != "" {
						r.fonts["+"+f.ref+"-"+s.ref] = tf
					}
				}
			}
		}
	}
	names := []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink"}
	for k := range r.cc.Map {
		names = append(names, k)
	}
	for _, n := range names {
		if res, err := r.cc.Scheme.Resolve(derive.Color{Kind: "scheme", Val: n}, r.cc.Map, nil); err == nil {
			r.s.Colors[n] = "#" + res.Hex
		}
	}
	return nil
}

// rootOf returns the document root, or nil for a nil document.
func rootOf(d *tree.Document) *tree.Node {
	if d == nil {
		return nil
	}
	return d.Root()
}

// masterName returns the slide master related to the layout.
func (r *reader) masterName(layName string) (*tree.Document, string) {
	name, doc, err := r.d.related(layName, ctypes.RelSlideMaster)
	if err != nil {
		return nil, ""
	}
	return doc, name
}

// objectKinds maps object element names to kinds.
var objectKinds = map[string]Kind{
	"sp": Shape, "cxnSp": Connector, "grpSp": Group, "pic": Picture,
	"graphicFrame": Other, "contentPart": Other,
}

// walk visits the children of a shape tree or group. pobj is the
// enclosing group object, pp its child-to-slide map (ppErr set when it
// could not be derived), gfill the group's fill for a:grpFill, source the
// enclosing mc:AlternateContent if any.
func (r *reader) walk(container *tree.Node, pobj *Object, pp derive.Parent, ppErr error, gfill *Fill, source *tree.Node) {
	for e := range container.ElementsSeq() {
		if derive.Is(e, derive.MC, "AlternateContent") {
			b := derive.SelectBranch(e)
			if b.Node == nil {
				r.s.Omitted = append(r.s.Omitted, Omission{"mc:AlternateContent", "no supported Choice and no Fallback; skipped"})
				continue
			}
			src := source
			if src == nil {
				src = e
			}
			before := len(r.s.Objects)
			r.walk(b.Node, pobj, pp, ppErr, gfill, src)
			branch := "Choice Requires=" + b.Requires
			if b.Fallback {
				branch = "Fallback"
			}
			for _, o := range r.s.Objects[before:] {
				o.Props = append(o.Props, Prop{"mc.branch", branch, FromSlide})
			}
			continue
		}
		kind, ok := objectKinds[e.Local()]
		if !ok || !(derive.In(e, derive.PML) || e.Local() == "contentPart") {
			continue
		}
		o := r.object(e, kind, pobj, pp, ppErr, gfill, source)
		if kind == Group {
			var gx derive.GroupXfrm
			err := ppErr
			if err == nil {
				if o.Stored == nil {
					err = errors.New("group has no transform")
				} else if sg, gerr := derive.ReadGroupXfrm(derive.XfrmElem(e)); gerr != nil {
					err = gerr
				} else {
					gx = sg.Float()
					o.Group = &gx
					var cp derive.Parent
					cp, err = pp.Child(gx)
					if err == nil {
						r.walk(e, o, cp, nil, o.Fill, source)
						continue
					}
				}
			}
			r.walk(e, o, derive.Parent{}, err, o.Fill, source)
		}
	}
}

// nvOf returns the non-visual properties element (p:nvSpPr and so on).
func nvOf(e *tree.Node) *tree.Node {
	for c := range e.ElementsSeq() {
		if strings.HasPrefix(c.Local(), "nv") {
			return c
		}
	}
	return nil
}

// childLocal returns the first child with the given local name, or nil.
func childLocal(n *tree.Node, local string) *tree.Node {
	if n == nil {
		return nil
	}
	for c := range n.ElementsSeq() {
		if c.Local() == local {
			return c
		}
	}
	return nil
}

// omit records an omitted property on the object.
func (o *Object) omit(prop, reason string) {
	o.Omitted = append(o.Omitted, Omission{prop, reason})
}

// setOrigin records the origin of a property.
func (o *Object) setOrigin(prop string, origin Origin) {
	if o.Origin == nil {
		o.Origin = map[string]Origin{}
	}
	o.Origin[prop] = origin
}

// object builds a read-model object from its element.
func (r *reader) object(e *tree.Node, kind Kind, pobj *Object, pp derive.Parent, ppErr error, gfill *Fill, source *tree.Node) *Object {
	o := &Object{Kind: kind, node: e, source: e, Z: r.z}
	if source != nil {
		o.source = source
	}
	r.z++
	if pobj != nil {
		o.Parent = pobj.ID
	}
	nv := nvOf(e)
	if c := childLocal(nv, "cNvPr"); c != nil {
		o.RawID, _ = c.Attr("id")
		o.Name, _ = c.Attr("name")
		h, err := derive.AttrBool(c, "hidden")
		if o.Hidden, _, err = derive.OrDefault(h, err, false); err != nil {
			o.omit("hidden", err.Error())
		}
		o.Descr, _ = c.Attr("descr")
		o.Title, _ = c.Attr("title")
	} else {
		o.omit("id", "no cNvPr")
	}
	if kind == Shape {
		if c := childLocal(nv, "cNvSpPr"); c != nil {
			tb, err := derive.AttrBool(c, "txBox")
			if o.TxBox, _, err = derive.OrDefault(tb, err, false); err != nil {
				o.omit("txBox", err.Error())
			}
		}
	}
	r.seen[o.RawID]++
	o.ID = "s" + o.RawID
	if n := r.seen[o.RawID]; n > 1 {
		o.ID += "~" + strconv.Itoa(n)
	}
	r.byRaw[o.RawID] = append(r.byRaw[o.RawID], o)
	r.s.Objects = append(r.s.Objects, o)

	if kind == Other {
		o.Type = otherType(e)
	}

	// Placeholder chain.
	links, last, isPh := derive.PlaceholderChain(e, r.lay, r.mst)
	chain := make([]link, 0, len(links))
	for _, l := range links {
		chain = append(chain, link{l.Elem, l.Origin})
	}
	var phType string
	if isPh {
		ref, _ := derive.ReadPh(e)
		o.Ph = &Placeholder{Type: ref.Type, Idx: ref.Idx}
		phType = last.EffType()
		if len(links) < 2 || links[1].Origin != FromLayout {
			o.omit("ph.layout", "no matching placeholder on the layout")
		}
	}

	// Transform: the same read half as the transform property codec.
	tr := derive.ReadTransform(links, pp, ppErr)
	switch {
	case tr.StoredErr != nil:
		o.omit("box", tr.StoredErr.Error())
	default:
		x := tr.Stored.Float()
		o.Stored = &x
		o.setOrigin("box", tr.Origin)
		o.M = tr.Placed.M
		if tr.PlaceErr != nil {
			o.omit("box", tr.PlaceErr.Error())
		} else {
			pl := tr.Placed
			o.Box = &pl.Box
			if pl.Skew > 0 {
				o.Props = append(o.Props, Prop{"box.skew", strconv.FormatFloat(pl.Skew, 'g', 3, 64) + " degrees, within tolerance; the box keeps the mapped x axis", FromSlide})
			}
		}
	}

	spPr := derive.Child(e, derive.PML, "spPr")
	style := derive.Child(e, derive.PML, "style")
	if style == nil {
		for _, l := range chain[1:] {
			if s := derive.Child(l.sp, derive.PML, "style"); s != nil {
				style = s
				break
			}
		}
	}

	switch kind {
	case Group:
		o.Fill = r.fill(o, []link{{derive.Child(e, derive.PML, "grpSpPr"), FromSlide}}, nil, gfill, true)
	case Shape, Connector, Picture:
		r.geometry(o, chain)
		switch {
		case kind == Shape:
			o.Fill = r.fill(o, spChain(chain), style, gfill, false)
		case kind == Connector && derive.FillElem(spPr) != nil:
			// A connector's own fill only: connector paths are never
			// filled, so its style's fill reference has nothing to show.
			o.Fill = r.fill(o, []link{{spPr, FromSlide}}, nil, nil, false)
		}
		o.Line = r.line(o, spChain(chain), style)
		if kind == Picture {
			r.picture(o, e)
		}
		if kind != Connector {
			r.text(o, e, chain, style, phType)
		}
		if kind == Connector || (o.Geom != nil && derive.IsConnectorPreset(o.Geom.Preset)) {
			r.connector(o, nv)
		}
		if kind != Connector {
			r.sites(o)
		}
	case Other:
		if spPr != nil {
			o.Line = r.line(o, spChain(chain), style)
		}
		if o.Type == "smartart" {
			o.omit("children", "SmartArt's stored drawing (dsp) shapes are not projected as children yet")
		}
	}
	return o
}

// spChain maps a chain to its spPr elements.
func spChain(chain []link) []link {
	out := make([]link, 0, len(chain))
	for _, l := range chain {
		out = append(out, link{derive.Child(l.sp, derive.PML, "spPr"), l.origin})
	}
	return out
}

// otherType classifies a graphic frame or content part.
func otherType(e *tree.Node) string {
	if e.Local() == "contentPart" {
		return "ink"
	}
	gd := derive.Path(e, derive.DML, "graphic", "graphicData")
	if gd == nil {
		return "graphicFrame"
	}
	uri, _ := gd.Attr("uri")
	switch {
	case strings.HasSuffix(uri, "/table"):
		return "table"
	case strings.HasSuffix(uri, "/chart"), strings.Contains(uri, "chartex"):
		return "chart"
	case strings.HasSuffix(uri, "/diagram"):
		return "smartart"
	case strings.HasSuffix(uri, "/ole"):
		return "ole"
	}
	return "graphicFrame"
}

// geometry resolves the object's geometry and path.
func (r *reader) geometry(o *Object, chain []link) {
	// The same read half as the geometry codec.
	dg, origin, ok := derive.ReadGeom(objLinks(chain))
	if !ok {
		if o.Kind != Group {
			o.omit("geom", "no prstGeom or custGeom on the object or its placeholders")
		}
		return
	}
	if !dg.Custom {
		o.Geom = &Geometry{Preset: dg.Preset, Adj: dg.Adj}
		o.setOrigin("geom", origin)
		return
	}
	cg := dg.Elem
	g := &Geometry{Custom: true}
	o.Geom = g
	o.setOrigin("geom", origin)
	if o.Stored == nil {
		o.omit("geom.path", "no transform: path extent unknown")
		return
	}
	cgm := derive.ReadCustomGeom(cg)
	kx, ky := 1.0, 1.0
	if o.Box != nil {
		kx, ky = derive.AxisScales(o.M)
	}
	paths, err := derive.CustomPaths(cgm, o.Stored.W, o.Stored.H, kx, ky, derive.FormatNum)
	if err != nil {
		o.omit("geom.path", err.Error())
	} else {
		g.Paths = paths
		var parts []string
		for _, p := range paths {
			if p.D != "" {
				parts = append(parts, p.D)
			}
		}
		g.Path = strings.Join(parts, " ")
		if g.Path == "" {
			// No commands at all: nothing to show or to write back.
			g.Paths = nil
			o.omit("geom.path", "custom geometry has no path commands")
		}
	}
	if sc, err := derive.ShowCustom(cgm, kx, ky); err != nil {
		o.omit("geom.custom", err.Error())
	} else {
		g.Parts = &sc
	}
}

// objLinks maps the reader's chain of object elements to derive links.
func objLinks(chain []link) []derive.Link { return links(chain) }

// sites resolves the object's connection sites.
func (r *reader) sites(o *Object) {
	if o.Geom == nil || o.Box == nil || o.Stored == nil {
		return
	}
	var err error
	switch {
	case o.Geom.Custom:
		spPr := derive.Child(o.node, derive.PML, "spPr")
		cgm := derive.ReadCustomGeom(derive.Child(spPr, derive.DML, "custGeom"))
		if len(cgm.Cxn) == 0 {
			return
		}
		o.Sites, err = derive.CustomSites(cgm, o.Stored.W, o.Stored.H, o.M)
	case derive.HasSiteTable(o.Geom.Preset):
		o.Sites, err = derive.PresetSites(o.Geom.Preset, o.Geom.Adj, o.Stored.W, o.Stored.H, o.M)
	default:
		o.omit("sites", "preset "+o.Geom.Preset+" is not in the connection-site table")
		return
	}
	if err != nil {
		o.Sites = nil
		o.omit("sites", err.Error())
	}
}

// picture reads a p:pic's image and SVG parts with their media facts,
// its blip fill (crop, fit, fill rectangle) and its locks, through the
// same derive calls as the picture codecs.
func (r *reader) picture(o *Object, e *tree.Node) {
	pic := &PicProps{}
	o.Pic = pic
	bf := derive.Child(e, derive.PML, "blipFill")
	if bf == nil {
		o.omit("crop", "no p:blipFill")
	} else if f, err := derive.ReadPicFill(bf); err != nil {
		o.omit("crop", err.Error())
	} else {
		pic.Fill = &f
	}
	if locks, err := derive.ReadPicLocks(derive.Path(e, derive.PML, "nvPicPr", "cNvPicPr")); err != nil {
		o.omit("locks", err.Error())
	} else {
		pic.Locks = derive.TrueLocks(locks)
	}
	blip := derive.Child(bf, derive.DML, "blip")
	if blip == nil {
		o.omit("image", "no a:blip")
		return
	}
	rid, ok := derive.AttrNS(blip, derive.REL, "embed")
	if !ok {
		if _, linked := derive.AttrNS(blip, derive.REL, "link"); linked {
			o.omit("image", "linked image (r:link), not embedded")
		}
	} else {
		o.Image, pic.Media = r.media(o, "image", rid)
	}
	if sb := derive.SVGBlip(blip); sb != nil {
		if rid, ok := derive.AttrNS(sb, derive.REL, "embed"); ok {
			pic.SVG, pic.SVGMedia = r.media(o, "svg", rid)
		} else {
			o.omit("svg", "asvg:svgBlip without r:embed")
		}
	}
}

// media resolves an embedding relationship id to its target and, for a
// part in the package, the part's media facts.
func (r *reader) media(o *Object, prop, rid string) (string, *MediaFacts) {
	rel, ok := r.rels.Get(rid)
	if !ok {
		o.omit(prop, "relationship "+rid+" not found")
		return "", nil
	}
	if rel.External() {
		return rel.Target, nil
	}
	t, err := r.rels.TargetPart(rel)
	if err != nil {
		o.omit(prop, err.Error())
		return "", nil
	}
	m, err := r.d.mediaFacts(t)
	switch {
	case err != nil:
		o.omit(prop+".media", err.Error())
	case prop == "image" && !m.HasPx:
		o.omit("media.px", m.PxReason)
	}
	return t, m
}

// connector resolves the connector's glue and endpoints.
func (r *reader) connector(o *Object, nv *tree.Node) {
	c := &Conn{}
	if cn := childLocal(nv, "cNvCxnSpPr"); cn != nil {
		for _, g := range []struct {
			el  string
			dst **Glue
		}{{"stCxn", &c.Start}, {"endCxn", &c.End}} {
			if n := derive.Child(cn, derive.DML, g.el); n != nil {
				raw, _ := n.Attr("id")
				idx, _ := n.Attr("idx")
				i, err := strconv.Atoi(idx)
				if err != nil {
					o.omit(g.el, "glue site index "+strconv.Quote(idx)+" is not an integer")
					continue
				}
				*g.dst = &Glue{RawID: raw, Site: i}
			}
		}
	}
	o.Conn = c
	switch {
	case o.Geom == nil:
		o.omit("from", "no geometry")
	case o.Geom.Custom:
		o.omit("from", "custom geometry connector: endpoints not derived")
	case !derive.IsConnectorPreset(o.Geom.Preset):
		o.omit("from", "preset "+o.Geom.Preset+" is not a connector preset")
	case o.Box == nil:
		o.omit("from", "no slide-space transform")
	default:
		g, err := derive.ConnectorGeom(o.Geom.Preset, o.Geom.Adj, o.Stored.W, o.Stored.H, o.M)
		if err != nil {
			o.omit("from", err.Error())
			break
		}
		c.From, c.To, c.First, c.Elbows, c.Curve = g.From, g.To, g.First, g.Elbows, g.Curve
		return
	}
	// Endpoints not derived: keep only glue.
	o.Conn = &Conn{Start: c.Start, End: c.End}
	o.Conn.First = "?"
}

// resolveGlue maps glue ids to object ids. An id shared by several
// objects is ambiguous and resolves to none of them.
func (r *reader) resolveGlue() {
	for _, o := range r.s.Objects {
		if o.Conn == nil {
			continue
		}
		for i, g := range []*Glue{o.Conn.Start, o.Conn.End} {
			if g == nil {
				continue
			}
			switch objs := r.byRaw[g.RawID]; len(objs) {
			case 0:
			case 1:
				g.ID = objs[0].ID
			default:
				for _, t := range objs {
					g.Candidates = append(g.Candidates, t.ID)
				}
				which := []string{"start", "end"}[i]
				o.omit(which, fmt.Sprintf("glue id %s is shared by %d objects (%s); the target is ambiguous", g.RawID, len(objs), strings.Join(g.Candidates, ", ")))
			}
		}
	}
}

// hazards detects stale, dangling, and group-frame hazards.
func (r *reader) hazards() {
	byID := map[string]*Object{}
	for _, o := range r.s.Objects {
		byID[o.ID] = o
	}
	for _, o := range r.s.Objects {
		if c := o.Conn; c != nil && o.Kind == Connector {
			for i, g := range []*Glue{c.Start, c.End} {
				if g == nil {
					continue
				}
				end, which := c.From, "start"
				if i == 1 {
					end, which = c.To, "end"
				}
				t := byID[g.ID]
				if len(g.Candidates) > 0 {
					o.omit(which+".stale", "glue target ambiguous; staleness unknown")
					continue
				}
				if g.ID == "" {
					r.s.Hazards = append(r.s.Hazards, Hazard{"dangling-glue", o.ID, fmt.Sprintf("%s glued to id %s, which is not on the slide", which, g.RawID)})
					continue
				}
				if c.First == "?" {
					o.omit(which+".stale", "endpoint not derived; staleness unknown")
					continue
				}
				if t.Sites == nil {
					o.omit(which+".stale", "target "+t.ID+" has no derived sites; staleness unknown")
					continue
				}
				if g.Site < 0 || g.Site >= len(t.Sites) {
					r.s.Hazards = append(r.s.Hazards, Hazard{"stale-connector", o.ID, fmt.Sprintf("%s glued to %s site %d, which has only %d sites", which, t.ID, g.Site, len(t.Sites))})
					continue
				}
				if d, stale := derive.StaleGlue(end, t.Sites[g.Site].Pt); stale {
					r.s.Hazards = append(r.s.Hazards, Hazard{"stale-connector", o.ID, fmt.Sprintf("%s is %s pt from %s:%d", which, derive.FormatNum(d/EMUPerPoint), t.ID, g.Site)})
				}
			}
		}
		if o.Parent != "" && o.Stored != nil {
			g := byID[o.Parent]
			if g == nil || g.Group == nil || g.Box == nil {
				continue
			}
			if d := derive.OutsideFrame(*o.Stored, *g.Group); d > derive.GroupFrameTolerance {
				r.s.Hazards = append(r.s.Hazards, Hazard{"group-frame", o.ID, fmt.Sprintf("extends %s pt outside the frame of group %s", derive.FormatNum(d/EMUPerPoint), g.ID)})
			}
		}
	}
}
