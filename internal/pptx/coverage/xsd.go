package coverage

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// SchemaZipSHA256 is the SHA-256 of OfficeOpenXML-XMLSchema-Transitional.zip
// in Apache POI at commit 1243387d (the commit pinned by
// eval/corpus/fetch.sh), the same file internal/ooxml/schema's tables are
// generated from (schema.ZipSHA256).
const SchemaZipSHA256 = schema.ZipSHA256

// SchemaURL is where the pinned schema zip is fetched from (schema.ZipURL).
const SchemaURL = schema.ZipURL

const xsdNS = "http://www.w3.org/2001/XMLSchema"

// Canonical prefixes of the namespaces the universe spans.
var nsPrefix = map[string]string{
	"http://schemas.openxmlformats.org/presentationml/2006/main":             "p",
	"http://schemas.openxmlformats.org/drawingml/2006/main":                  "a",
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships":    "r",
	"http://schemas.openxmlformats.org/officeDocument/2006/sharedTypes":      "s",
	"http://schemas.openxmlformats.org/drawingml/2006/picture":               "pic",
	"http://schemas.openxmlformats.org/drawingml/2006/diagram":               "dgm",
	"http://schemas.openxmlformats.org/drawingml/2006/chart":                 "c",
	"http://schemas.openxmlformats.org/drawingml/2006/lockedCanvas":          "lc",
	"http://schemas.openxmlformats.org/officeDocument/2006/math":             "m",
	"http://schemas.openxmlformats.org/drawingml/2006/chartDrawing":          "cdr",
	"http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing":    "xdr",
	"http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing": "wp",
}

// schemaFiles are the schemas loaded: PresentationML, DrawingML main and
// the shared types they import. Graphic frame content (charts, SmartArt,
// tables' outer frame aside) is xsd:any in a:graphicData and is not
// followed.
var schemaFiles = []string{"pml.xsd", "dml-main.xsd", "shared-commonSimpleTypes.xsd", "shared-relationshipReference.xsd"}

// Root is one starting point of the universe: an element and its complex
// type, with the subtrees not followed below it.
type Root struct {
	// Path is the element path of the root, such as "p:sp" or
	// "p:presentation/p:sldSz".
	Path string `json:"path"`
	Type string `json:"type"` // qualified complex type, such as "p:CT_Shape"
	// Kind is the object kind for object roots (shape, connector, group,
	// picture, other), "slide" for slide-level context, "factored" for
	// the pseudo-root of a factored type.
	Kind string `json:"kind"`
}

// Roots are the object kinds of the design's primitive model and the
// slide-level context the read model reads.
var Roots = []Root{
	{"p:sp", "p:CT_Shape", "shape"},
	{"p:cxnSp", "p:CT_Connector", "connector"},
	{"p:grpSp", "p:CT_GroupShape", "group"},
	{"p:pic", "p:CT_Picture", "picture"},
	{"p:graphicFrame", "p:CT_GraphicalObjectFrame", "other"},
	{"p:contentPart", "p:CT_Rel", "other"},
	{"p:sld", "p:CT_Slide", "slide"},
	{"p:sldLayout/p:clrMapOvr", "a:CT_ColorMappingOverride", "slide"},
	{"p:sldMaster/p:clrMap", "a:CT_ColorMapping", "slide"},
	{"p:sldMaster/p:txStyles", "p:CT_SlideMasterTextStyles", "slide"},
	{"p:presentation/p:sldSz", "p:CT_SlideSize", "slide"},
	{"p:presentation/p:defaultTextStyle", "a:CT_TextListStyle", "slide"},
	{"a:theme/a:themeElements/a:clrScheme", "a:CT_ColorScheme", "slide"},
	{"a:theme/a:themeElements/a:fontScheme", "a:CT_FontScheme", "slide"},
	{"a:theme/a:themeElements/a:fmtScheme", "a:CT_StyleMatrix", "slide"},
}

// objectElems are the elements that are objects of their own: below a
// group or shape tree they are recorded but not expanded, since each is
// a root.
var objectElems = map[string]bool{
	"p:sp": true, "p:cxnSp": true, "p:grpSp": true, "p:pic": true, "p:graphicFrame": true, "p:contentPart": true,
}

// stopElems are slide-level subtrees recorded as one item and not
// expanded: animations and transitions are non-goals, and the slide
// background is not read.
var stopElems = map[string]string{
	"p:sld/p:transition": "transitions are a non-goal",
	"p:sld/p:timing":     "animations are a non-goal",
	"p:sld/p:cSld/p:bg":  "the slide background is not part of the diagram model",
}

type xschema struct {
	uri    string
	prefix string
	types  map[string]*tree.Node // complexType by name
	simple map[string]*tree.Node // simpleType by name
	groups map[string]*tree.Node
	agroup map[string]*tree.Node
	attrs  map[string]*tree.Node // top-level attributes
	elems  map[string]*tree.Node // top-level elements
}

type xsdSet struct {
	byURI map[string]*xschema
}

// GenerateUniverse reads the pinned schema zip and enumerates every
// element and attribute path reachable from Roots.
//
// Recursion rule: a complex type is not expanded below an element whose
// path already contains an element of the same type (the item is
// recorded with Recursive set). This cuts true recursion
// (a:effectDag/a:cont/a:cont..., a:grpSp inside a:grpSp...) after one
// level and keeps every distinct schema position once. Object elements
// below a group or shape tree, and the slide-level subtrees in
// stopElems, are recorded without expansion. xsd:any (extension bodies,
// graphic data) is recorded as an item named "*".
func GenerateUniverse(zipBytes []byte) (*Universe, error) {
	if sum := sha256.Sum256(zipBytes); hex.EncodeToString(sum[:]) != SchemaZipSHA256 {
		return nil, fmt.Errorf("schema zip SHA-256 %x, want %s", sum, SchemaZipSHA256)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	xs := &xsdSet{byURI: map[string]*xschema{}}
	for _, name := range schemaFiles {
		f, err := zr.Open(name)
		if err != nil {
			return nil, err
		}
		src, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		doc, err := tree.Parse(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		root := doc.Root()
		tns, _ := root.Attr("targetNamespace")
		s := &xschema{uri: tns, prefix: nsPrefix[tns], types: map[string]*tree.Node{}, simple: map[string]*tree.Node{},
			groups: map[string]*tree.Node{}, agroup: map[string]*tree.Node{}, attrs: map[string]*tree.Node{}, elems: map[string]*tree.Node{}}
		for e := range root.ElementsSeq() {
			n, _ := e.Attr("name")
			switch e.Local() {
			case "complexType":
				s.types[n] = e
			case "simpleType":
				s.simple[n] = e
			case "group":
				s.groups[n] = e
			case "attributeGroup":
				s.agroup[n] = e
			case "attribute":
				s.attrs[n] = e
			case "element":
				s.elems[n] = e
			}
		}
		xs.byURI[tns] = s
	}
	g := &gen{xs: xs, u: &Universe{Schema: SchemaURL, SHA256: SchemaZipSHA256, Rule: RecursionRule, Enums: map[string][]string{}, Simple: map[string]SimpleType{}}, seen: map[string]bool{}, queued: map[string]bool{}}
	roots := append([]Root(nil), Roots...)
	for i := 0; i < len(roots); i++ {
		r := roots[i]
		g.u.Roots = append(g.u.Roots, r)
		s, local, err := xs.resolve(nil, r.Type)
		if err != nil {
			return nil, err
		}
		ct := s.types[local]
		if ct == nil {
			return nil, fmt.Errorf("no complex type %s", r.Type)
		}
		if r.Kind != "factored" && Factored[r.Type] {
			// A root of a factored type (the presentation's default text
			// style) is an item whose content is the pseudo-root's.
			it := Item{Path: r.Path, Node: "element", Type: r.Type, Min: 1, Max: "1", Root: r.Path, Factored: PseudoRoot(r.Type)}
			g.add(it)
			if !g.queued[r.Type] {
				g.queued[r.Type] = true
				roots = append(roots, Root{Path: it.Factored, Type: r.Type, Kind: "factored"})
			}
			continue
		}
		g.add(Item{Path: r.Path, Node: "element", Type: r.Type, Min: 1, Max: "1", Root: r.Path})
		if err := g.complex(r.Path, r.Path, s, ct, []string{r.Type}); err != nil {
			return nil, err
		}
		// Factored types reached so far are expanded once each, as roots
		// of their own, after the roots that reach them.
		roots = append(roots, g.pending...)
		g.pending = nil
	}
	return g.u, nil
}

// resolve splits a QName written in schema node n's scope (or a canonical
// "p:CT_x" when n is nil) into its schema and local name.
func (xs *xsdSet) resolve(n *tree.Node, qname string) (*xschema, string, error) {
	prefix, local, ok := strings.Cut(qname, ":")
	if !ok {
		prefix, local = "", qname
	}
	var uri string
	if n == nil {
		for u, p := range nsPrefix {
			if p == prefix {
				uri = u
			}
		}
	} else {
		u, ok := n.LookupPrefix(prefix)
		if !ok {
			return nil, "", fmt.Errorf("prefix %q not declared", prefix)
		}
		uri = u
	}
	if uri == xsdNS {
		return nil, "xsd:" + local, nil
	}
	s := xs.byURI[uri]
	if s == nil {
		return nil, "", fmt.Errorf("namespace %s not loaded (for %s)", uri, qname)
	}
	return s, local, nil
}

type gen struct {
	xs      *xsdSet
	u       *Universe
	seen    map[string]bool
	queued  map[string]bool
	pending []Root
}

// Factored are value types whose content is enumerated once, under a
// pseudo-root named "(type)", instead of at every position: at each
// position only the element itself is an item (with Factored naming the
// pseudo-root). Each is read by one function wherever it is read, or not
// read anywhere, so its content means the same at every position:
// colours (derive.ParseColor; the modifiers apply in order everywhere),
// gradient and pattern fill bodies (shown as raw XML wherever shown),
// effect containers and 3D (non-goals, preserved), list-style levels
// (one resolver, levelProps, for every list style) and blips. Without
// this the universe has three million paths, almost all repeats.
var Factored = map[string]bool{
	"a:CT_SchemeColor": true, "a:CT_SRgbColor": true, "a:CT_ScRgbColor": true,
	"a:CT_HslColor": true, "a:CT_SystemColor": true, "a:CT_PresetColor": true,
	"a:CT_GradientFillProperties": true, "a:CT_PatternFillProperties": true,
	"a:CT_EffectList": true, "a:CT_EffectContainer": true,
	"a:CT_Scene3D": true, "a:CT_Shape3D": true,
	"a:CT_TextListStyle": true, "a:CT_Blip": true,
}

// PseudoRoot names the root a factored type is enumerated under.
func PseudoRoot(typ string) string { return "(" + typ + ")" }

// add appends an item to the universe unless already seen.
func (g *gen) add(it Item) {
	if g.seen[it.Path] {
		// The same path reached twice (a name in two choice branches):
		// keep the first.
		return
	}
	g.seen[it.Path] = true
	g.u.Items = append(g.u.Items, it)
}

// particleCtx carries what an enclosing model group says about a
// particle: the choice it belongs to and whether the group makes it
// optional or repeating.
type particleCtx struct {
	choice   string
	optional bool
	repeated bool
	ordinal  *int // unnamed choices seen in the current complex type
}

// complex enumerates attributes and content of a complex type.
func (g *gen) complex(path, root string, s *xschema, ct *tree.Node, types []string) error {
	// Attributes first (they are listed under the element), then the
	// content model in schema order.
	if err := g.attributes(path, root, s, ct); err != nil {
		return err
	}
	for c := range ct.ElementsSeq() {
		switch c.Local() {
		case "sequence", "choice", "group", "all":
			if err := g.particle(path, root, s, c, particleCtx{ordinal: new(int)}, types); err != nil {
				return err
			}
		case "attribute", "attributeGroup", "anyAttribute", "annotation":
		case "complexContent", "simpleContent":
			return fmt.Errorf("%s: %s not supported", path, c.Local())
		default:
			return fmt.Errorf("%s: unsupported %s", path, c.Local())
		}
	}
	return nil
}

// attributes enumerates the attributes of a complex type node.
func (g *gen) attributes(path, root string, s *xschema, n *tree.Node) error {
	for a := range n.ElementsSeq() {
		switch a.Local() {
		case "attribute":
			it := Item{Node: "attribute", Root: root, Max: "1"}
			var decl *tree.Node
			if ref, ok := a.Attr("ref"); ok {
				rs, local, err := g.xs.resolve(a, ref)
				if err != nil {
					return err
				}
				decl = rs.attrs[local]
				if decl == nil {
					return fmt.Errorf("%s: no attribute %s", path, ref)
				}
				it.Path = path + "/@" + rs.prefix + ":" + local
				s2 := rs
				it.Type, it.Enum = g.simpleType(decl, s2)
				if d, ok := decl.Attr("default"); ok {
					it.Default = &d
				}
			} else {
				name, _ := a.Attr("name")
				it.Path = path + "/@" + name
				decl = a
				it.Type, it.Enum = g.simpleType(a, s)
			}
			if d, ok := a.Attr("default"); ok {
				it.Default = &d
			}
			if u, _ := a.Attr("use"); u == "required" {
				it.Min = 1
			}
			g.add(it)
		case "attributeGroup":
			ref, _ := a.Attr("ref")
			rs, local, err := g.xs.resolve(a, ref)
			if err != nil {
				return err
			}
			ag := rs.agroup[local]
			if ag == nil {
				return fmt.Errorf("%s: no attribute group %s", path, ref)
			}
			if err := g.attributes(path, root, rs, ag); err != nil {
				return err
			}
		}
	}
	return nil
}

// simpleType returns the qualified type name of an attribute or element
// declaration and, when it is an enumeration, the enumeration's name
// (its values are recorded in the universe's Enums).
func (g *gen) simpleType(decl *tree.Node, _ *xschema) (string, string) {
	t, ok := decl.Attr("type")
	if !ok {
		// An anonymous simple type.
		for c := range decl.ElementsSeq() {
			if c.Local() == "simpleType" {
				return "(anonymous)", ""
			}
		}
		return "xsd:anySimpleType", ""
	}
	ts, local, err := g.xs.resolve(decl, t)
	if err != nil {
		return t, ""
	}
	if ts == nil {
		return local, "" // xsd:...
	}
	q := ts.prefix + ":" + local
	if vals := g.enumValues(ts, local, 0); len(vals) > 0 {
		if _, ok := g.u.Enums[q]; !ok {
			g.u.Enums[q] = vals
		}
		return q, q
	}
	if _, ok := g.u.Simple[q]; !ok {
		st := g.simpleInfo(ts, local, 0)
		g.u.Simple[q] = st
	}
	return q, ""
}

// simpleInfo resolves a simple type to its built-in base and its
// inclusive bounds, following restriction bases; a union takes its first
// member.
func (g *gen) simpleInfo(s *xschema, local string, depth int) SimpleType {
	st := s.simple[local]
	if st == nil || depth > 8 {
		return SimpleType{Base: "xsd:string"}
	}
	for c := range st.ElementsSeq() {
		switch c.Local() {
		case "restriction":
			var out SimpleType
			base, _ := c.Attr("base")
			bs, bl, err := g.xs.resolve(c, base)
			switch {
			case err != nil:
				out.Base = "xsd:string"
			case bs == nil:
				out.Base = bl
			default:
				out = g.simpleInfo(bs, bl, depth+1)
			}
			for f := range c.ElementsSeq() {
				v, _ := f.Attr("value")
				switch f.Local() {
				case "minInclusive", "minExclusive":
					out.Min = v
				case "maxInclusive", "maxExclusive":
					out.Max = v
				case "pattern":
					out.Pattern = v
				}
			}
			return out
		case "union":
			mt, _ := c.Attr("memberTypes")
			f := strings.Fields(mt)
			if len(f) == 0 {
				return SimpleType{Base: "xsd:string"}
			}
			ms, ml, err := g.xs.resolve(c, f[0])
			if err != nil {
				return SimpleType{Base: "xsd:string"}
			}
			if ms == nil {
				return SimpleType{Base: ml}
			}
			return g.simpleInfo(ms, ml, depth+1)
		case "list":
			return SimpleType{Base: "list"}
		}
	}
	return SimpleType{Base: "xsd:string"}
}

// enumValues returns the enumeration values of a simple type, following
// restriction bases and unions whose members are all enumerations.
func (g *gen) enumValues(s *xschema, local string, depth int) []string {
	st := s.simple[local]
	if st == nil || depth > 8 {
		return nil
	}
	for c := range st.ElementsSeq() {
		switch c.Local() {
		case "restriction":
			var vals []string
			for e := range c.ElementsSeq() {
				if e.Local() == "enumeration" {
					v, _ := e.Attr("value")
					vals = append(vals, v)
				}
			}
			if len(vals) > 0 {
				return vals
			}
			if base, ok := c.Attr("base"); ok {
				bs, bl, err := g.xs.resolve(c, base)
				if err == nil && bs != nil {
					return g.enumValues(bs, bl, depth+1)
				}
			}
			return nil
		case "union":
			mt, _ := c.Attr("memberTypes")
			var vals []string
			for m := range strings.FieldsSeq(mt) {
				ms, ml, err := g.xs.resolve(c, m)
				if err != nil || ms == nil {
					return nil
				}
				v := g.enumValues(ms, ml, depth+1)
				if len(v) == 0 {
					return nil
				}
				vals = append(vals, v...)
			}
			return vals
		}
	}
	return nil
}

// occurs returns the min and max occurrence of element n.
func occurs(n *tree.Node) (int, string) {
	minOcc := 1
	if v, ok := n.Attr("minOccurs"); ok {
		minOcc, _ = strconv.Atoi(v)
	}
	maxOcc := "1"
	if v, ok := n.Attr("maxOccurs"); ok {
		maxOcc = v
	}
	return minOcc, maxOcc
}

// particle enumerates one content-model particle under path.
func (g *gen) particle(path, root string, s *xschema, p *tree.Node, ctx particleCtx, types []string) error {
	minOcc, maxOcc := occurs(p)
	inner := ctx
	if minOcc == 0 {
		inner.optional = true
	}
	if maxOcc != "1" {
		inner.repeated = true
	}
	switch p.Local() {
	case "sequence", "all":
		for c := range p.ElementsSeq() {
			if err := g.particle(path, root, s, c, inner, types); err != nil {
				return err
			}
		}
	case "choice":
		// An unnamed choice is named by its type and its ordinal among
		// the type's unnamed choices.
		*ctx.ordinal++
		inner.choice = fmt.Sprintf("%s#choice%d", types[len(types)-1], *ctx.ordinal)
		for c := range p.ElementsSeq() {
			if err := g.particle(path, root, s, c, inner, types); err != nil {
				return err
			}
		}
	case "group":
		ref, _ := p.Attr("ref")
		gs, local, err := g.xs.resolve(p, ref)
		if err != nil {
			return err
		}
		grp := gs.groups[local]
		if grp == nil {
			return fmt.Errorf("%s: no group %s", path, ref)
		}
		for c := range grp.ElementsSeq() {
			if c.Local() == "choice" {
				// A named choice group (EG_FillProperties...) names the
				// choice.
				cctx := inner
				cctx.choice = gs.prefix + ":" + local
				cmin, cmax := occurs(c)
				if cmin == 0 {
					cctx.optional = true
				}
				if cmax != "1" {
					cctx.repeated = true
				}
				for a := range c.ElementsSeq() {
					if err := g.particleNamed(path, root, gs, a, cctx, types); err != nil {
						return err
					}
				}
				continue
			}
			if err := g.particle(path, root, gs, c, inner, types); err != nil {
				return err
			}
		}
	case "element":
		return g.element(path, root, s, p, ctx, types)
	case "any":
		it := Item{Path: path + "/*", Node: "any", Root: root, Min: 0, Max: maxOcc, Choice: ctx.choice}
		g.add(it)
	case "annotation":
	default:
		return fmt.Errorf("%s: unsupported particle %s", path, p.Local())
	}
	return nil
}

// particleNamed handles the alternatives of a named choice group, where
// a nested choice or group keeps the outer group's name.
func (g *gen) particleNamed(path, root string, s *xschema, p *tree.Node, ctx particleCtx, types []string) error {
	switch p.Local() {
	case "choice":
		for c := range p.ElementsSeq() {
			if err := g.particleNamed(path, root, s, c, ctx, types); err != nil {
				return err
			}
		}
		return nil
	case "group":
		ref, _ := p.Attr("ref")
		gs, local, err := g.xs.resolve(p, ref)
		if err != nil {
			return err
		}
		grp := gs.groups[local]
		if grp == nil {
			return fmt.Errorf("%s: no group %s", path, ref)
		}
		for c := range grp.ElementsSeq() {
			if err := g.particleNamed(path, root, gs, c, ctx, types); err != nil {
				return err
			}
		}
		return nil
	}
	return g.particle(path, root, s, p, ctx, types)
}

// element enumerates one element declaration under path, recursing into complex types.
func (g *gen) element(path, root string, s *xschema, e *tree.Node, ctx particleCtx, types []string) error {
	minOcc, maxOcc := occurs(e)
	name, ok := e.Attr("name")
	if !ok {
		// A reference to a top-level element (a:graphic): its
		// declaration gives the name and type, the reference the bounds.
		ref, ok := e.Attr("ref")
		if !ok {
			return fmt.Errorf("%s: element without name or ref", path)
		}
		rs, local, err := g.xs.resolve(e, ref)
		if err != nil {
			return err
		}
		decl := rs.elems[local]
		if decl == nil {
			return fmt.Errorf("%s: no element %s", path, ref)
		}
		s, e, name = rs, decl, local
	}
	qn := s.prefix + ":" + name
	child := path + "/" + qn
	if ctx.optional || ctx.choice != "" {
		minOcc = 0
	}
	if ctx.repeated && maxOcc == "1" {
		maxOcc = "unbounded"
	}
	it := Item{Path: child, Node: "element", Root: root, Min: minOcc, Max: maxOcc, Choice: ctx.choice}
	t, hasType := e.Attr("type")
	if !hasType {
		it.Type = "(anonymous)"
		g.add(it)
		return nil
	}
	ts, local, err := g.xs.resolve(e, t)
	if err != nil {
		return err
	}
	if ts == nil {
		it.Type = local
		g.add(it)
		return nil
	}
	it.Type = ts.prefix + ":" + local
	if ts.types[local] == nil {
		// A simple-typed element (a:t and the like).
		it.Type, it.Enum = g.simpleType(e, s)
		g.add(it)
		return nil
	}
	if objectElems[qn] && path != "" {
		it.Object = true
		g.add(it)
		return nil
	}
	if why, ok := stopElems[child]; ok {
		it.Stop = why
		g.add(it)
		return nil
	}
	if Factored[it.Type] {
		it.Factored = PseudoRoot(it.Type)
		g.add(it)
		if !g.queued[it.Type] {
			g.queued[it.Type] = true
			g.pending = append(g.pending, Root{Path: it.Factored, Type: it.Type, Kind: "factored"})
		}
		return nil
	}
	if slices.Contains(types, it.Type) {
		it.Recursive = true
		g.add(it)
		return nil
	}
	g.add(it)
	return g.complex(child, root, ts, ts.types[local], append(types[:len(types):len(types)], it.Type))
}
