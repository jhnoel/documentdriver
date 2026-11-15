//go:build ignore

// gen_schema extracts child-order, attribute and simple-type tables from
// the ECMA-376 transitional schemas and writes one Go table per
// vocabulary: PresentationML (pml.xsd, prefix p), DrawingML main
// (dml-main.xsd, prefix a), WordprocessingML (wml.xsd, prefix w),
// SpreadsheetML (sml.xsd, prefix x) and the shared simple types
// (shared-commonSimpleTypes.xsd, prefix s).
//
// Which complex types are tabulated differs by vocabulary. For pml and
// dml it is the fixed list of types the pptx edit layer inserts into
// (wantedPD below), where every type must be expressible. For wml and sml
// (wholePrefixes) it is every complex type of the schema, so that the
// docx and xlsx sides start from the whole vocabulary; a type the slot
// model cannot express exactly (xsd:all, a sequence inside a single
// choice, a repeated sequence of elements, one element in two positions)
// is not tabulated and is listed with the reason instead (wmlUntabulated,
// smlUntabulated; the latter is empty at the pinned schema). Each table
// also holds the attributes of its tabulated types and of their
// children's types, and the facets of every simple type those attributes
// use, resolved through restriction bases, union members (an anonymous
// member is named after its union and position) and list item types; a
// simple type goes in the table of its own prefix.
//
// The input is not committed. It is the OfficeOpenXML-XMLSchema-
// Transitional.zip in Apache POI at a pinned commit (the commit pinned by
// eval/corpus/fetch.sh), checked here by its SHA-256 (schema.ZipSHA256;
// a test keeps the two equal):
//
//	https://raw.githubusercontent.com/apache/poi/1243387d7ffbac84d361323673f1b75a5b3c26f3/poi-ooxml-full/src/main/xmlschema/org/apache/poi/schemas/OfficeOpenXML-XMLSchema-Transitional.zip
//
// Usage (from this directory; writes pml_table.go, dml_table.go,
// wml_table.go, sml_table.go, shared_table.go, rel_table.go, xml_table.go,
// wml_model_table.go and sml_model_table.go to outdir, default here;
// TestGeneratedTables compares a run into a temporary directory with the
// committed files):
//
//	go run gen_schema.go OfficeOpenXML-XMLSchema-Transitional.zip [outdir]
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/format"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

const zipSHA256 = "d34187520749998af306faf1b730e568b0ca6d88ad24638a407c0a9bb4ca04fc"

// wantedPD are the PresentationML and DrawingML complex types whose child
// order is tabulated: the ones the pptx operations insert into.
var wantedPD = []string{
	"p:CT_Shape", "p:CT_Connector", "p:CT_Picture", "p:CT_GroupShape",
	"p:CT_ShapeNonVisual", "p:CT_ConnectorNonVisual", "p:CT_PictureNonVisual", "p:CT_GroupShapeNonVisual",
	"a:CT_NonVisualDrawingShapeProps", "a:CT_NonVisualConnectorProperties",
	"a:CT_ShapeProperties", "a:CT_GroupShapeProperties",
	"a:CT_Transform2D", "a:CT_GroupTransform2D",
	"a:CT_PresetGeometry2D", "a:CT_CustomGeometry2D",
	"a:CT_LineProperties", "a:CT_SolidColorFillProperties",
	"a:CT_TextBody", "a:CT_TextBodyProperties", "a:CT_TextParagraph",
	"a:CT_TextParagraphProperties", "a:CT_RegularTextRun", "a:CT_TextCharacterProperties",
	"a:CT_GeomGuideList", "a:CT_NonVisualDrawingProps",
	// The six colour types, whose modifiers AppendEach places.
	"a:CT_SRgbColor", "a:CT_SchemeColor", "a:CT_SystemColor", "a:CT_PresetColor", "a:CT_HslColor", "a:CT_ScRgbColor",
	// The lists of a custom geometry, whose children ReplaceChildren
	// replaces (M4 paths), and the elements inside them.
	"a:CT_Path2DList", "a:CT_Path2D", "a:CT_AdjustHandleList", "a:CT_ConnectionSiteList",
	"a:CT_ConnectionSite", "a:CT_XYAdjustHandle", "a:CT_PolarAdjustHandle",
	// Line spacing, space before and after, and tab stops in a paragraph's
	// properties: their children (percent, point, tab stop) carry the
	// attributes the text grammar writes.
	"a:CT_TextSpacing", "a:CT_TextTabStopList",
	// Pictures: the blip fill (crop, fill mode), the blip and its
	// extension list (the SVG blip), the picture locks.
	"a:CT_BlipFillProperties", "a:CT_Blip", "a:CT_StretchInfoProperties", "a:CT_OfficeArtExtensionList",
	"a:CT_NonVisualPictureProperties",
}

// vocab is one generated table.
type vocab struct {
	file   string // schema file in the zip
	prefix string // table prefix of its names
	name   string // Go identifier prefix and output file name stem
	title  string // what the vocabulary is, for the generated comment
}

var vocabs = []vocab{
	{"pml.xsd", "p", "pml", "PresentationML"},
	{"dml-main.xsd", "a", "dml", "DrawingML main"},
	{"wml.xsd", "w", "wml", "WordprocessingML"},
	{"sml.xsd", "x", "sml", "SpreadsheetML"},
	{"shared-commonSimpleTypes.xsd", "s", "shared", "the shared common simple types"},
	{"shared-relationshipReference.xsd", "r", "rel", "the relationship reference"},
}

// wholePrefixes are the vocabularies whose every complex type is
// tabulated (and given a content model), with the inexpressible ones
// listed as untabulated: WordprocessingML and SpreadsheetML.
var wholePrefixes = []string{"w", "x"}

// xmlVocab is the XML namespace (xml:space), which the zip lacks: its one
// table holds the type of xml:space, read from xml.xsd.
var xmlVocab = vocab{"xml.xsd", "xml", "xml", "the XML namespace"}

type schema struct {
	prefix string
	// foreign marks a schema loaded only for its groups: its elements
	// are named, never typed (its types are not tabulated).
	foreign    bool
	types      map[string]*tree.Node
	order      []string // complex type names in document order
	groups     map[string]*tree.Node
	simples    map[string]*tree.Node
	attrGroups map[string]*tree.Node
}

var schemas = map[string]*schema{}

// foreignGroups are schemas whose groups a tabulated type references but
// whose types are not tabulated: OMML (shared-math.xsd), whose
// EG_OMathMathElements is part of w:CT_RunTrackChange (the content of
// w:ins, w:del, w:moveFrom and w:moveTo). Their elements enter a slot by
// name, untyped, as an element reference into another schema does
// (m:oMath).
var foreignGroups = map[string]*schema{}

type child struct{ prefix, local, typ string }

type slot struct {
	names []child
	multi bool
}

// errInexpressible marks content the slot model cannot express exactly.
var errInexpressible = errors.New("not expressible as slots")

// inexpressible returns an error marking content the slot model cannot express.
func inexpressible(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInexpressible, fmt.Sprintf(format, args...))
}

// main generates the schema tables from the input ZIP file.
func main() {
	if len(os.Args) != 2 && len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run gen_schema.go OfficeOpenXML-XMLSchema-Transitional.zip [outdir]")
		os.Exit(2)
	}
	outDir := "."
	if len(os.Args) == 3 {
		outDir = os.Args[2]
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fatal(err)
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != zipSHA256 {
		fatal(fmt.Errorf("%s: SHA-256 %x, want %s", os.Args[1], sum, zipSHA256))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		fatal(err)
	}
	for _, v := range vocabs {
		schemas[v.prefix] = load(zr, v.file, v.prefix)
	}
	math := load(zr, "shared-math.xsd", "m")
	math.foreign = true
	foreignGroups["m"] = math

	// The tabulated complex types and the reasons for the wml and sml types
	// left out.
	types := map[string][]slot{}
	skipped := map[string]string{}
	for _, t := range wantedPD {
		slots, err := typeSlots(t)
		if err != nil {
			fatal(err)
		}
		types[t] = slots
	}
	for _, p := range wholePrefixes {
		for _, local := range schemas[p].order {
			t := p + ":" + local
			slots, err := typeSlots(t)
			switch {
			case errors.Is(err, errInexpressible):
				skipped[t] = strings.TrimPrefix(err.Error(), errInexpressible.Error()+": ")
			case err != nil:
				fatal(err)
			default:
				types[t] = slots
			}
		}
	}

	// The attribute tables cover the tabulated types and their children's
	// types; the simple tables every simple type those attributes use.
	attrTypes := map[string]bool{}
	for t, slots := range types {
		attrTypes[t] = true
		for _, s := range slots {
			for _, c := range s.names {
				if c.typ != "" {
					attrTypes[c.typ] = true
				}
			}
		}
	}
	attrs := map[string]map[string]string{}
	simples := map[string]*genSimple{}
	for _, t := range slices.Sorted(maps.Keys(attrTypes)) {
		a := map[string]string{}
		p, local, _ := strings.Cut(t, ":")
		s := schemas[p]
		if s.types[local] == nil {
			continue // a simple-content child type, or one from another schema
		}
		collectAttrs(s, s.types[local], a)
		if len(a) == 0 {
			continue
		}
		attrs[t] = a
		for _, st := range a {
			resolveSimple(st, simples)
		}
	}

	// The content model, and the simple types its attributes use that the
	// attribute tables do not already hold.
	models, refs := modelTables(zr)
	schemas["xml"] = &schema{prefix: "xml"}
	for _, r := range slices.Sorted(maps.Keys(refs)) {
		switch {
		case r == "xml:space":
			simples[r] = xmlSpaceSimple()
		case !strings.HasPrefix(r, "xsd:"):
			resolveSimple(r, simples)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(models)) {
		if err := os.WriteFile(filepath.Join(outDir, name), models[name], 0o644); err != nil {
			fatal(err)
		}
	}

	for _, v := range append(vocabs, xmlVocab) {
		var b bytes.Buffer
		fmt.Fprintf(&b, "// Code generated by gen_schema.go from the ECMA-376 transitional schemas (%s); DO NOT EDIT.\n\npackage schema\n", v.file)
		b.WriteString(importsFor(v.prefix))
		writeTypes(&b, v, types, skipped)
		writeAttrs(&b, v, attrs)
		writeSimples(&b, v, simples)
		out, err := format.Source(b.Bytes())
		if err != nil {
			fatal(fmt.Errorf("%s: %w", v.name, err))
		}
		if err := os.WriteFile(filepath.Join(outDir, v.name+"_table.go"), out, 0o644); err != nil {
			fatal(err)
		}
	}
}

// inVocab reports whether qname belongs to vocabulary v.
func inVocab(v vocab, qname string) bool { return strings.HasPrefix(qname, v.prefix+":") }

// writeTypes writes the Types table for vocabulary v.
func writeTypes(b *bytes.Buffer, v vocab, types map[string][]slot, skipped map[string]string) {
	fmt.Fprintf(b, "\n// %sTypes holds each tabulated %s complex type's children in schema\n// order.%s\n", v.name, v.title, lazyNote(v.prefix))
	openTable(b, v.prefix, v.name+"Types", "map[string][]Slot")
	for _, t := range slices.Sorted(maps.Keys(types)) {
		if !inVocab(v, t) {
			continue
		}
		fmt.Fprintf(b, "\t%q: {\n", t)
		for _, s := range types[t] {
			b.WriteString("\t\t{Names: []Child{")
			for i, c := range s.names {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(b, "{%q, %q, %q}", c.prefix, c.local, c.typ)
			}
			fmt.Fprintf(b, "}, Multi: %v},\n", s.multi)
		}
		b.WriteString("\t},\n")
	}
	closeTable(b, v.prefix)
	var sk []string
	for t := range skipped {
		if inVocab(v, t) {
			sk = append(sk, t)
		}
	}
	if len(sk) == 0 && !slices.Contains(wholePrefixes, v.prefix) {
		return
	}
	sort.Strings(sk)
	fmt.Fprintf(b, "\n// %sUntabulated lists the %s complex types whose content the\n// slot model cannot express exactly, with the reason.%s\n", v.name, v.title, lazyNote(v.prefix))
	openTable(b, v.prefix, v.name+"Untabulated", "map[string]string")
	for _, t := range sk {
		fmt.Fprintf(b, "\t%q: %q,\n", t, skipped[t])
	}
	closeTable(b, v.prefix)
}

// writeAttrs writes the Attrs table for vocabulary v.
func writeAttrs(b *bytes.Buffer, v vocab, attrs map[string]map[string]string) {
	fmt.Fprintf(b, "\n// %sAttrs maps the tabulated %s complex types, and the types of\n// tabulated types' children, to their attributes' simple types (xsd: for\n// built-ins).%s\n", v.name, v.title, lazyNote(v.prefix))
	openTable(b, v.prefix, v.name+"Attrs", "map[string]map[string]string")
	for _, t := range slices.Sorted(maps.Keys(attrs)) {
		if !inVocab(v, t) {
			continue
		}
		fmt.Fprintf(b, "\t%q: {", t)
		for i, a := range slices.Sorted(maps.Keys(attrs[t])) {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%q: %q", a, attrs[t][a])
		}
		b.WriteString("},\n")
	}
	closeTable(b, v.prefix)
}

// writeSimples writes the Simple table for vocabulary v.
func writeSimples(b *bytes.Buffer, v vocab, simples map[string]*genSimple) {
	fmt.Fprintf(b, "\n// %sSimple holds the %s simple types the attribute tables use,\n// each resolved through its restriction chain: the built-in it ends in,\n// union members, enumeration, inclusive integer bounds, patterns (each\n// must match) and lengths.%s\n", v.name, v.title, lazyNote(v.prefix))
	openTable(b, v.prefix, v.name+"Simple", "map[string]Simple")
	for _, n := range slices.Sorted(maps.Keys(simples)) {
		if !inVocab(v, n) {
			continue
		}
		st := simples[n]
		fmt.Fprintf(b, "\t%q: {", n)
		var parts []string
		if st.base != "" {
			parts = append(parts, fmt.Sprintf("Base: %q", st.base))
		}
		if len(st.union) > 0 {
			parts = append(parts, fmt.Sprintf("Union: %#v", st.union))
		}
		if st.list != "" {
			parts = append(parts, fmt.Sprintf("List: %q", st.list))
		}
		if len(st.enum) > 0 {
			parts = append(parts, fmt.Sprintf("Enum: %#v", st.enum))
		}
		if st.hasMin {
			parts = append(parts, fmt.Sprintf("Min: %d, HasMin: true", st.min))
		}
		if st.hasMax {
			parts = append(parts, fmt.Sprintf("Max: %d, HasMax: true", st.max))
		}
		if len(st.patterns) > 0 {
			parts = append(parts, fmt.Sprintf("Patterns: %#v", st.patterns))
		}
		for _, l := range []struct {
			name string
			v    int
		}{{"Length", st.length}, {"MinLen", st.minLen}, {"MaxLen", st.maxLen}} {
			if l.v != 0 {
				parts = append(parts, fmt.Sprintf("%s: %d", l.name, l.v))
			}
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("},\n")
	}
	closeTable(b, v.prefix)
}

// load reads one schema file from the zip into a schema.
func load(zr *zip.Reader, name, prefix string) *schema {
	f, err := zr.Open(name)
	if err != nil {
		fatal(err)
	}
	src, err := io.ReadAll(f)
	if err != nil {
		fatal(err)
	}
	doc, err := tree.Parse(src)
	if err != nil {
		fatal(fmt.Errorf("%s: %w", name, err))
	}
	s := &schema{prefix: prefix, types: map[string]*tree.Node{}, groups: map[string]*tree.Node{},
		simples: map[string]*tree.Node{}, attrGroups: map[string]*tree.Node{}}
	for _, e := range doc.Root().Elements() {
		n, _ := e.Attr("name")
		switch e.Local() {
		case "complexType":
			s.types[n] = e
			s.order = append(s.order, n)
		case "group":
			s.groups[n] = e
		case "simpleType":
			s.simples[n] = e
		case "attributeGroup":
			s.attrGroups[n] = e
		}
	}
	return s
}

// qualify resolves a type or group reference written in schema s: a
// prefix names the loaded schema of that table prefix (the four schema
// files declare the same prefixes the tables use), no prefix names s
// itself. A schema that is not loaded gives nil.
func qualify(s *schema, ref string) (*schema, string) {
	if p, local, ok := strings.Cut(ref, ":"); ok {
		if o, ok := schemas[p]; ok {
			return o, local
		}
		return nil, local // another schema: not tabulated
	}
	return s, ref
}

// typeSlots returns the child slots of complex type qname.
func typeSlots(qname string) ([]slot, error) {
	p, local, _ := strings.Cut(qname, ":")
	s := schemas[p]
	ct := s.types[local]
	if ct == nil {
		return nil, fmt.Errorf("no complex type %s", qname)
	}
	out, err := content(s, ct, qname)
	if err != nil {
		return nil, err
	}
	// Placing a child by its name needs one position per name.
	seen := map[string]bool{}
	for _, sl := range out {
		names := map[string]bool{}
		for _, c := range sl.names {
			k := c.prefix + ":" + c.local
			if seen[k] && !names[k] {
				return nil, inexpressible("%s appears in two positions", k)
			}
			names[k] = true
		}
		for k := range names {
			seen[k] = true
		}
	}
	return out, nil
}

// content returns the slots of a complex type's content model; an
// extension's content follows its base type's.
func content(s *schema, ct *tree.Node, qname string) ([]slot, error) {
	var out []slot
	for _, c := range ct.Elements() {
		switch c.Local() {
		case "sequence":
			sl, err := sequence(s, c)
			if err != nil {
				return nil, err
			}
			out = append(out, sl...)
		case "choice", "group":
			sl, err := sequence(s, &wrapper{c})
			if err != nil {
				return nil, err
			}
			out = append(out, sl...)
		case "complexContent":
			for _, d := range c.Elements() {
				switch d.Local() {
				case "extension":
					base, _ := d.Attr("base")
					bs, local := qualify(s, base)
					if bs == nil || bs.types[local] == nil {
						return nil, fmt.Errorf("%s: no base type %s", qname, base)
					}
					sl, err := content(bs, bs.types[local], base)
					if err != nil {
						return nil, err
					}
					out = append(out, sl...)
					fallthrough
				case "restriction":
					sl, err := content(s, d, qname)
					if err != nil {
						return nil, err
					}
					out = append(out, sl...)
				default:
					return nil, fmt.Errorf("%s: unsupported derivation %s", qname, d.Local())
				}
			}
		case "all":
			return nil, inexpressible("xsd:all leaves the order free")
		case "attribute", "attributeGroup", "anyAttribute", "simpleContent":
		default:
			return nil, fmt.Errorf("%s: unsupported content %s", qname, c.Local())
		}
	}
	return out, nil
}

// wrapper lets a lone particle be read as a one-particle sequence.
type wrapper struct{ n *tree.Node }

type particles interface{ Elements() []*tree.Node }

// Elements returns the wrapper's single wrapped node.
func (w *wrapper) Elements() []*tree.Node { return []*tree.Node{w.n} }

// many reports whether the particle repeats (maxOccurs not 1).
func many(n *tree.Node) bool {
	m, ok := n.Attr("maxOccurs")
	return ok && m != "1"
}

// sequence flattens a sequence into slots: an element is a slot, a choice
// (or a group whose content is a choice) is one slot of alternatives, a
// nested sequence (or sequence group) contributes its own slots.
func sequence(s *schema, seq particles) ([]slot, error) {
	var out []slot
	for _, p := range seq.Elements() {
		switch p.Local() {
		case "element":
			c, err := elem(s, p)
			if err != nil {
				return nil, err
			}
			out = append(out, slot{names: []child{c}, multi: many(p)})
		case "choice":
			multi := many(p)
			if els := p.Elements(); len(els) == 1 && els[0].Local() == "element" {
				// A choice of one element is that element, repeated if
				// either repeats.
				c, err := elem(s, els[0])
				if err != nil {
					return nil, err
				}
				out = append(out, slot{names: []child{c}, multi: multi || many(els[0])})
				continue
			}
			alts, err := alternatives(s, p, multi)
			if err != nil {
				return nil, err
			}
			out = append(out, slot{names: alts, multi: multi})
		case "sequence":
			sl, err := sequence(s, p)
			if err != nil {
				return nil, err
			}
			if many(p) && len(sl) > 0 {
				return nil, inexpressible("repeated nested sequence")
			}
			out = append(out, sl...)
		case "group":
			gs, body, err := group(s, p)
			if err != nil {
				return nil, err
			}
			switch body.Local() {
			case "choice":
				multi := many(p) || many(body)
				alts, err := alternatives(gs, body, multi)
				if err != nil {
					return nil, err
				}
				out = append(out, slot{names: alts, multi: multi})
			case "sequence":
				sl, err := sequence(gs, body)
				if err != nil {
					return nil, err
				}
				if many(p) && len(sl) > 0 {
					return nil, inexpressible("repeated sequence group")
				}
				out = append(out, sl...)
			}
		case "any":
			// Extension points (CT_OfficeArtExtension bodies) and foreign
			// content (VML): nothing to place.
		default:
			return nil, fmt.Errorf("unsupported particle %s", p.Local())
		}
	}
	return out, nil
}

// group resolves a group reference to its schema and body.
func group(s *schema, ref *tree.Node) (*schema, *tree.Node, error) {
	r, _ := ref.Attr("ref")
	gs, local := qualify(s, r)
	if p, _, ok := strings.Cut(r, ":"); gs == nil && ok {
		gs = foreignGroups[p]
	}
	if gs == nil {
		return nil, nil, inexpressible("group %s is from a schema that is not tabulated", r)
	}
	if gs.groups[local] == nil {
		return nil, nil, fmt.Errorf("no group %s", r)
	}
	els := gs.groups[local].Elements()
	if len(els) != 1 {
		return nil, nil, fmt.Errorf("group %s: %d particles", r, len(els))
	}
	return gs, els[0], nil
}

// alternatives lists a choice's element alternatives, flattening nested
// choices and choice groups. In a repeating choice (multi) the order of
// its elements is free anyway, so a nested sequence or repetition
// contributes its elements as alternatives; in a single choice either
// would change what the slot means, and is inexpressible.
func alternatives(s *schema, ch *tree.Node, multi bool) ([]child, error) {
	var out []child
	add := func(cs ...child) {
		for _, c := range cs {
			if !containsChild(out, c) {
				out = append(out, c)
			}
		}
	}
	for _, p := range ch.Elements() {
		if many(p) && !multi {
			return nil, inexpressible("repeated %s inside a single choice", p.Local())
		}
		switch p.Local() {
		case "element":
			c, err := elem(s, p)
			if err != nil {
				return nil, err
			}
			add(c)
		case "choice":
			cs, err := alternatives(s, p, multi)
			if err != nil {
				return nil, err
			}
			add(cs...)
		case "sequence":
			if !multi {
				return nil, inexpressible("sequence inside a single choice")
			}
			cs, err := alternatives(s, p, multi)
			if err != nil {
				return nil, err
			}
			add(cs...)
		case "group":
			gs, body, err := group(s, p)
			if err != nil {
				return nil, err
			}
			if many(body) && !multi {
				return nil, inexpressible("repeated group inside a single choice")
			}
			if body.Local() != "choice" && !multi {
				return nil, inexpressible("sequence group inside a single choice")
			}
			cs, err := alternatives(gs, body, multi)
			if err != nil {
				return nil, err
			}
			add(cs...)
		case "any":
		default:
			return nil, fmt.Errorf("unsupported alternative %s", p.Local())
		}
	}
	return out, nil
}

// containsChild reports whether cs already holds child c.
func containsChild(cs []child, c child) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

// elem returns the child an element particle declares. A reference to a
// global element of another schema (m:oMath, wp:inline) names that
// element, with its type untabulated.
func elem(s *schema, e *tree.Node) (child, error) {
	if ref, ok := e.Attr("ref"); ok {
		p, local, ok := strings.Cut(ref, ":")
		if !ok || schemas[p] != nil {
			return child{}, fmt.Errorf("element ref %s into a tabulated schema", ref)
		}
		return child{p, local, ""}, nil
	}
	name, ok := e.Attr("name")
	if !ok {
		return child{}, fmt.Errorf("element without a name")
	}
	if s.foreign {
		return child{s.prefix, name, ""}, nil
	}
	typ, _ := e.Attr("type")
	ts, local := qualify(s, typ)
	qt := ""
	if ts != nil && ts.types[local] != nil {
		qt = ts.prefix + ":" + local
	} else if ts != nil && ts.simples[local] == nil && typ != "" && !strings.HasPrefix(typ, "xsd:") {
		return child{}, fmt.Errorf("element %s: no type %s", name, typ)
	}
	return child{s.prefix, name, qt}, nil
}

// genSimple is a simple type resolved through its restriction chain.
type genSimple struct {
	base                   string // the XSD built-in the chain ends in; "" for a union or a list
	union                  []string
	list                   string // the item type of a list type
	enum                   []string
	min, max               int64
	hasMin, hasMax         bool
	patterns               []string
	length, minLen, maxLen int
}

// collectAttrs adds the attributes of complex type ct (in schema s),
// with attribute groups and complex-content bases expanded, to out as
// name to qualified simple type. Attributes by reference (r:id, r:embed)
// belong to other schemas and are not tabulated.
func collectAttrs(s *schema, ct *tree.Node, out map[string]string) {
	for _, c := range ct.Elements() {
		switch c.Local() {
		case "attribute":
			name, ok := c.Attr("name")
			if !ok {
				continue // ref="r:id"
			}
			typ, ok := c.Attr("type")
			if !ok {
				fatal(fmt.Errorf("attribute %s has an inline type", name))
			}
			out[name] = qualifySimple(s, typ)
		case "attributeGroup":
			ref, _ := c.Attr("ref")
			gs, local := qualify(s, ref)
			if gs == nil || gs.attrGroups[local] == nil {
				fatal(fmt.Errorf("no attribute group %s", ref))
			}
			collectAttrs(gs, gs.attrGroups[local], out)
		case "complexContent", "simpleContent":
			for _, d := range c.Elements() {
				if base, ok := d.Attr("base"); ok {
					if bs, local := qualify(s, base); bs != nil && bs.types[local] != nil {
						collectAttrs(bs, bs.types[local], out)
					}
				}
				collectAttrs(s, d, out)
			}
		}
	}
}

// qualifySimple returns the qualified name of a simple type reference
// written in schema s: xsd:x for built-ins, else prefix:local.
func qualifySimple(s *schema, ref string) string {
	if strings.HasPrefix(ref, "xsd:") {
		return ref
	}
	ts, local := qualify(s, ref)
	if ts == nil || ts.simples[local] == nil {
		fatal(fmt.Errorf("simple type %s not found", ref))
	}
	return ts.prefix + ":" + local
}

// resolveSimple resolves the simple type qn and every type it depends on
// into out.
func resolveSimple(qn string, out map[string]*genSimple) *genSimple {
	if strings.HasPrefix(qn, "xsd:") {
		return &genSimple{base: qn}
	}
	if st, ok := out[qn]; ok {
		return st
	}
	p, local, _ := strings.Cut(qn, ":")
	s := schemas[p]
	return resolveSimpleNode(qn, s, s.simples[local], out)
}

// resolveSimpleNode resolves simple type node, written in schema s, under
// the name qn. An anonymous member of a union (sml's x:ST_TextRotation)
// is tabulated as the union's name and its 1-based position,
// "x:ST_TextRotation/1".
func resolveSimpleNode(qn string, s *schema, node *tree.Node, out map[string]*genSimple) *genSimple {
	els := node.Elements()
	if len(els) != 1 {
		fatal(fmt.Errorf("%s: %d derivations", qn, len(els)))
	}
	d := els[0]
	st := &genSimple{}
	switch d.Local() {
	case "list":
		item, ok := d.Attr("itemType")
		if !ok || len(d.Elements()) > 0 {
			fatal(fmt.Errorf("%s: list without a named item type", qn))
		}
		st.list = qualifySimple(s, item)
		resolveSimple(st.list, out)
	case "union":
		members, _ := d.Attr("memberTypes")
		for _, m := range strings.Fields(members) {
			mq := qualifySimple(s, m)
			resolveSimple(mq, out)
			st.union = append(st.union, mq)
		}
		for i, m := range d.Elements() {
			if m.Local() != "simpleType" {
				fatal(fmt.Errorf("%s: union member %s", qn, m.Local()))
			}
			mq := fmt.Sprintf("%s/%d", qn, i+1)
			resolveSimpleNode(mq, s, m, out)
			st.union = append(st.union, mq)
		}
	case "restriction":
		base, _ := d.Attr("base")
		bq := qualifySimple(s, base)
		*st = *resolveSimple(bq, out)
		st.enum = nil
		st.patterns = append([]string(nil), st.patterns...)
		var enum []string
		for _, f := range d.Elements() {
			v, _ := f.Attr("value")
			switch f.Local() {
			case "enumeration":
				enum = append(enum, v)
			case "pattern":
				if _, err := regexp.Compile("^(?:" + v + ")$"); err != nil {
					fatal(fmt.Errorf("%s: pattern %q: %w", qn, v, err))
				}
				st.patterns = append(st.patterns, v)
			case "minInclusive", "minExclusive", "maxInclusive", "maxExclusive":
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					fatal(fmt.Errorf("%s: %s %q: %w", qn, f.Local(), v, err))
				}
				switch f.Local() {
				case "minExclusive":
					n++
					fallthrough
				case "minInclusive":
					if !st.hasMin || n > st.min {
						st.min, st.hasMin = n, true
					}
				case "maxExclusive":
					n--
					fallthrough
				case "maxInclusive":
					if !st.hasMax || n < st.max {
						st.max, st.hasMax = n, true
					}
				}
			case "length", "minLength", "maxLength":
				n, err := strconv.Atoi(v)
				if err != nil {
					fatal(fmt.Errorf("%s: %s %q: %w", qn, f.Local(), v, err))
				}
				switch f.Local() {
				case "length":
					st.length = n
				case "minLength":
					st.minLen = n
				default:
					st.maxLen = n
				}
			case "whiteSpace":
			default:
				fatal(fmt.Errorf("%s: unsupported facet %s", qn, f.Local()))
			}
		}
		if enum != nil {
			st.enum = enum
		} else if bs := out[bq]; bs != nil {
			st.enum = bs.enum
		}
	default:
		fatal(fmt.Errorf("%s: unsupported derivation %s", qn, d.Local()))
	}
	out[qn] = st
	return st
}

// fatal prints err to stderr and exits 1.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen_schema:", err)
	os.Exit(1)
}

// The content models (wml_model_table.go, sml_model_table.go; one per
// modelVocabs entry): for every complex type of wml.xsd or sml.xsd, each
// child the type allows with a conservative
// order rank, a maximum count, its complex type and whether it is
// required; the namespaces its wildcards admit; and its attributes with
// their simple types and whether they are required. It is what an
// integrity check needs that the slot tables leave out. Ranks are
// conservative: a sequence gives its particles increasing ranks;
// everything inside a choice, or inside a repeating sequence, shares one
// rank; an element that appears at two ranks gets none. So a document is
// out of order only when a child's rank is below one already seen, and an
// order the schema allows is never reported. This is the algorithm of
// docxdriver's scripts/generate-wml-model.py, over these schemas.
//
// Every schema of the zip is read for the groups, elements and attributes
// wml.xsd and sml.xsd refer to. The zip has no xml.xsd; xml:space's type is read from
// the W3C xml.xsd in tools/conformance/ooxml-schemas, which is not in git:
// copy the schemas there before running the generator.

const (
	nsXSD = "http://www.w3.org/2001/XMLSchema"
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsX   = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsXML = "http://www.w3.org/XML/1998/namespace"
)

// modelPrefixes are the table prefixes of the namespaces the models name.
var modelPrefixes = map[string]string{
	nsW: "w", nsX: "x", nsXSD: "xsd", nsXML: "xml",
	"http://schemas.openxmlformats.org/officeDocument/2006/math":             "m",
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships":    "r",
	"http://schemas.openxmlformats.org/officeDocument/2006/sharedTypes":      "s",
	"http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing": "wp",
	"http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing":    "xdr",
	"http://schemas.openxmlformats.org/schemaLibrary/2006/main":              "sl",
	"": "",
}

// modelVocab is one generated content model: the complex types of one
// namespace, written to file as <name>Models and <name>Roots.
type modelVocab struct {
	ns, prefix, name, title, file string
}

// modelVocabs are the content models, one per vocabulary of
// wholePrefixes.
var modelVocabs = []modelVocab{
	{nsW, "w", "wml", "WordprocessingML", "wml.xsd"},
	{nsX, "x", "sml", "SpreadsheetML", "sml.xsd"},
}

// modelPrefix returns the table prefix of a namespace with a content
// model, or "".
func modelPrefix(ns string) string {
	for _, v := range modelVocabs {
		if v.ns == ns {
			return v.prefix
		}
	}
	return ""
}

const xmlXSD = "../../../tools/conformance/ooxml-schemas/xml.xsd"

type xsdFile struct {
	prefixes  map[string]string // prefix -> namespace, first declaration wins
	ns        string
	qualified bool // attributeFormDefault="qualified"
}

type xsdComp struct {
	f *xsdFile
	n *tree.Node
}

type modelGen struct {
	complex, simple, groups, attrGroups, elements, attributes map[string]xsdComp // by {ns}name
	types                                                     map[string]*modelType
	simpleRefs                                                map[string]bool // table names of the attributes' simple types
}

type modelChild struct {
	rank, max int // -1: no rank, unbounded
	kind      string
	required  bool
}

type modelType struct {
	children map[string]*modelChild // by {ns}local
	any      map[string]bool
	attrs    map[string]modelAttr // by {ns}local; "*" for anyAttribute
}

type modelAttr struct {
	simple   string // table name, "" for none
	required bool
}

// clark returns the {namespace}local form of a name.
func clark(ns, local string) string { return "{" + ns + "}" + local }

// splitClark splits a Clark name into namespace and local name.
func splitClark(c string) (string, string) {
	ns, local, _ := strings.Cut(strings.TrimPrefix(c, "{"), "}")
	return ns, local
}

// qn resolves a prefixed name written in f.
func (f *xsdFile) qn(v string) string {
	p, local, ok := strings.Cut(v, ":")
	if !ok {
		return clark(f.prefixes[""], v)
	}
	if p == "xml" {
		return clark(nsXML, local)
	}
	return clark(f.prefixes[p], local)
}

// readXSD parses one XSD source and indexes its declarations.
func readXSD(src []byte, name string, g *modelGen) {
	doc, err := tree.Parse(src)
	if err != nil {
		fatal(fmt.Errorf("%s: %w", name, err))
	}
	root := doc.Root()
	f := &xsdFile{prefixes: map[string]string{}}
	f.ns, _ = root.Attr("targetNamespace")
	if v, _ := root.Attr("attributeFormDefault"); v == "qualified" {
		f.qualified = true
	}
	var walk func(n *tree.Node)
	walk = func(n *tree.Node) {
		for _, a := range n.Attrs() {
			p := ""
			switch {
			case a.Name == "xmlns":
			case strings.HasPrefix(a.Name, "xmlns:"):
				p = a.Name[len("xmlns:"):]
			default:
				continue
			}
			if _, ok := f.prefixes[p]; !ok {
				f.prefixes[p] = a.Value
			}
		}
		for _, c := range n.Elements() {
			walk(c)
		}
	}
	walk(root)
	for _, e := range root.Elements() {
		n, ok := e.Attr("name")
		if !ok || e.Space() != nsXSD {
			continue
		}
		var table map[string]xsdComp
		switch e.Local() {
		case "complexType":
			table = g.complex
		case "simpleType":
			table = g.simple
		case "group":
			table = g.groups
		case "attributeGroup":
			table = g.attrGroups
		case "element":
			table = g.elements
		case "attribute":
			table = g.attributes
		default:
			continue
		}
		table[clark(f.ns, n)] = xsdComp{f, e}
	}
}

// occurs returns attr's occurrence count, defaulting to 1 and -1 for unbounded.
func occurs(n *tree.Node, attr string) int {
	v, ok := n.Attr(attr)
	switch {
	case !ok:
		return 1
	case v == "unbounded":
		return -1
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		fatal(fmt.Errorf("%s=%q: %w", attr, v, err))
	}
	return i
}

// multiply multiplies occurrence counts, keeping -1 (unbounded) as -1.
func multiply(a, b int) int {
	if a == -1 || b == -1 {
		return -1
	}
	return a * b
}

// flattener collects the children of one complex type.
type flattener struct {
	g    *modelGen
	t    *modelType
	rank int
}

// add records child name with rank and max, merging duplicates.
func (fl *flattener) add(name string, rank, max int, kind string, required bool) {
	if old, ok := fl.t.children[name]; ok {
		if old.rank != rank {
			rank = -1
		}
		if old.max == -1 || max == -1 {
			max = -1
		} else {
			max += old.max
		}
		if old.kind != kind {
			kind = ""
		}
		required = false
	}
	fl.t.children[name] = &modelChild{rank: rank, max: max, kind: kind, required: required}
}

// particle walks one particle; collapsed is the shared rank, or -1 in a
// plain sequence; needed is whether every enclosing particle is mandatory.
func (fl *flattener) particle(f *xsdFile, n *tree.Node, multiplier, collapsed int, needed bool) {
	if n.Space() != nsXSD {
		return
	}
	repeat := multiply(multiplier, occurs(n, "maxOccurs"))
	needed = needed && collapsed < 0 && occurs(n, "minOccurs") >= 1
	switch n.Local() {
	case "element":
		rank := fl.rank
		if collapsed >= 0 {
			rank = collapsed
		}
		var name, kind string
		if ref, ok := n.Attr("ref"); ok {
			name = f.qn(ref)
			if d, ok := fl.g.elements[name]; ok {
				kind = fl.g.elementType(d.f, d.n)
			}
		} else {
			local, _ := n.Attr("name")
			name, kind = clark(f.ns, local), fl.g.elementType(f, n)
		}
		fl.add(name, rank, repeat, kind, needed)
		if collapsed < 0 {
			fl.rank++
		}
	case "any":
		ns, ok := n.Attr("namespace")
		if !ok {
			ns = "##any"
		}
		fl.t.any[ns] = true
		if collapsed < 0 {
			fl.rank++
		}
	case "group":
		ref, _ := n.Attr("ref")
		gc, ok := fl.g.groups[f.qn(ref)]
		if !ok {
			fatal(fmt.Errorf("no group %s", ref))
		}
		for _, c := range gc.n.Elements() {
			switch c.Local() {
			case "sequence", "choice", "all":
				fl.group(gc.f, c, repeat, collapsed, needed)
			}
		}
	case "sequence", "choice", "all":
		fl.group(f, n, multiplier, collapsed, needed)
	}
}

// group flattens a sequence, choice, or all particle into the model.
func (fl *flattener) group(f *xsdFile, n *tree.Node, repeat, collapsed int, needed bool) {
	own := multiply(repeat, occurs(n, "maxOccurs"))
	needed = needed && occurs(n, "minOccurs") >= 1
	if collapsed < 0 && (n.Local() != "sequence" || own != 0 && own != 1) {
		collapsed = fl.rank
		for _, c := range n.Elements() {
			fl.particle(f, c, own, collapsed, false)
		}
		fl.rank++
		return
	}
	for _, c := range n.Elements() {
		fl.particle(f, c, own, collapsed, needed)
	}
}

// elementType returns the complex type an element declaration names, as
// a table name, when it is a type of a vocabulary with a content model
// (w:, x:), or "".
func (g *modelGen) elementType(f *xsdFile, n *tree.Node) string {
	t, ok := n.Attr("type")
	if !ok {
		for _, c := range n.Elements() {
			if c.Local() == "complexType" && modelPrefix(f.ns) != "" {
				fatal(fmt.Errorf("an anonymous complex type in namespace %s: not supported", f.ns))
			}
		}
		return ""
	}
	k := f.qn(t)
	ns, local := splitClark(k)
	if p := modelPrefix(ns); p != "" {
		if _, ok := g.complex[k]; ok {
			return p + ":" + local
		}
	}
	return ""
}

// simpleName returns the table name of a simple type reference.
func simpleName(c string) string {
	ns, local := splitClark(c)
	p, ok := modelPrefixes[ns]
	if !ok || p == "" {
		fatal(fmt.Errorf("simple type %s in a namespace without a table", c))
	}
	return p + ":" + local
}

// attrsOf collects the attributes of n into into.
func (g *modelGen) attrsOf(f *xsdFile, n *tree.Node, into map[string]modelAttr) {
	for _, c := range n.Elements() {
		switch c.Local() {
		case "attribute":
			var name string
			df, dn := f, c
			if ref, ok := c.Attr("ref"); ok {
				name = f.qn(ref)
				if d, ok := g.attributes[name]; ok {
					df, dn = d.f, d.n
				}
			} else {
				local, _ := c.Attr("name")
				name = clark("", local)
				if f.qualified {
					name = clark(f.ns, local)
				}
			}
			if use, _ := c.Attr("use"); use == "prohibited" {
				delete(into, name)
				continue
			}
			var simple string
			if t, ok := dn.Attr("type"); ok {
				simple = simpleName(df.qn(t))
			} else if inlineSimple(dn) != nil {
				// An anonymous type (xml:space's) is tabulated under the
				// attribute's own name.
				ns, local := splitClark(name)
				simple = modelPrefixes[ns] + ":" + local
			}
			use, _ := c.Attr("use")
			into[name] = modelAttr{simple: simple, required: use == "required"}
			if simple != "" {
				g.simpleRefs[simple] = true
			}
		case "attributeGroup":
			ref, _ := c.Attr("ref")
			ag, ok := g.attrGroups[f.qn(ref)]
			if !ok {
				fatal(fmt.Errorf("no attribute group %s", ref))
			}
			g.attrsOf(ag.f, ag.n, into)
		case "anyAttribute":
			into["*"] = modelAttr{}
		}
	}
}

// inlineSimple returns the anonymous simpleType child of n, or nil.
func inlineSimple(n *tree.Node) *tree.Node {
	for _, c := range n.Elements() {
		if c.Local() == "simpleType" {
			return c
		}
	}
	return nil
}

// build builds the content model for one complex type.
func (g *modelGen) build(name string, c xsdComp) {
	t := &modelType{children: map[string]*modelChild{}, any: map[string]bool{}, attrs: map[string]modelAttr{}}
	g.types[name] = t
	fl := &flattener{g: g, t: t}
	var content func(f *xsdFile, n *tree.Node)
	content = func(f *xsdFile, n *tree.Node) {
		for _, ch := range n.Elements() {
			switch ch.Local() {
			case "sequence", "choice", "group", "all":
				fl.particle(f, ch, 1, -1, true)
			case "complexContent", "simpleContent":
				els := ch.Elements()
				if len(els) == 0 {
					continue
				}
				d := els[0]
				base, _ := d.Attr("base")
				if bc, ok := g.complex[f.qn(base)]; ok && d.Local() == "extension" {
					content(bc.f, bc.n)
				}
				content(f, d)
			}
		}
		g.attrsOf(f, n, t.attrs)
	}
	content(c.f, c.n)
}

// modelTables builds the models from the zip and xml.xsd and returns the
// source of each model's table file (wml_model_table.go,
// sml_model_table.go) by file name, and the simple types their
// attributes use.
func modelTables(zr *zip.Reader) (map[string][]byte, map[string]bool) {
	g := &modelGen{
		complex: map[string]xsdComp{}, simple: map[string]xsdComp{}, groups: map[string]xsdComp{},
		attrGroups: map[string]xsdComp{}, elements: map[string]xsdComp{}, attributes: map[string]xsdComp{},
		simpleRefs: map[string]bool{},
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	for _, n := range names {
		f, err := zr.Open(n)
		if err != nil {
			fatal(err)
		}
		src, err := io.ReadAll(f)
		if err != nil {
			fatal(err)
		}
		readXSD(src, n, g)
	}
	src, err := os.ReadFile(xmlXSD)
	if err != nil {
		fatal(err)
	}
	readXSD(src, "xml.xsd", g)

	out := map[string][]byte{}
	for _, v := range modelVocabs {
		out[v.name+"_model_table.go"] = g.modelTable(v)
	}
	return out, g.simpleRefs
}

// modelTable builds the content model of every complex type of v's
// namespace and returns the source of its table file.
func (g *modelGen) modelTable(v modelVocab) []byte {
	g.types = map[string]*modelType{}
	var vtypes []string
	for k := range g.complex {
		if ns, _ := splitClark(k); ns == v.ns {
			vtypes = append(vtypes, k)
		}
	}
	sort.Strings(vtypes)
	for _, k := range vtypes {
		_, local := splitClark(k)
		g.build(v.prefix+":"+local, g.complex[k])
	}
	roots := map[string]string{}
	for k, e := range g.elements {
		if ns, local := splitClark(k); ns == v.ns {
			if t := g.elementType(e.f, e.n); t != "" {
				roots[local] = t
			}
		}
	}

	label := func(c string) (string, string) {
		ns, local := splitClark(c)
		p, ok := modelPrefixes[ns]
		if !ok {
			fatal(fmt.Errorf("%s: a namespace without a table prefix", c))
		}
		return p, local
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by gen_schema.go from the ECMA-376 transitional schemas (%s and the schemas it refers to) and the W3C xml.xsd; DO NOT EDIT.\n\npackage schema\n", v.file)
	b.WriteString(importsFor(v.prefix))
	fmt.Fprintf(&b, "\n// %sModels holds the content model of every %s complex type.%s\n", v.name, v.title, lazyNote(v.prefix))
	openTable(&b, v.prefix, v.name+"Models", "map[string]Model")
	for _, name := range slices.Sorted(maps.Keys(g.types)) {
		t := g.types[name]
		fmt.Fprintf(&b, "\t%q: {", name)
		if len(t.children) > 0 {
			b.WriteString("Children: []Particle{")
			for _, c := range slices.Sorted(maps.Keys(t.children)) {
				m := t.children[c]
				p, local := label(c)
				fmt.Fprintf(&b, "{%q, %q, %d, %d, %q, %v}, ", p, local, m.rank, m.max, m.kind, m.required)
			}
			b.WriteString("}, ")
		}
		if len(t.any) > 0 {
			fmt.Fprintf(&b, "Wildcards: %#v, ", slices.Sorted(maps.Keys(t.any)))
		}
		var attrs []string
		for _, a := range slices.Sorted(maps.Keys(t.attrs)) {
			if a == "*" {
				b.WriteString("AnyAttr: true, ")
				continue
			}
			attrs = append(attrs, a)
		}
		if len(attrs) > 0 {
			b.WriteString("Attrs: []ModelAttr{")
			for _, a := range attrs {
				p, local := label(a)
				fmt.Fprintf(&b, "{%q, %q, %q, %v}, ", p, local, t.attrs[a].simple, t.attrs[a].required)
			}
			b.WriteString("}")
		}
		b.WriteString("},\n")
	}
	closeTable(&b, v.prefix)
	fmt.Fprintf(&b, "\n// %sRoots maps the global %s elements of a complex type\n// to it.%s\n", v.name, v.title, lazyNote(v.prefix))
	openTable(&b, v.prefix, v.name+"Roots", "map[string]string")
	for _, r := range slices.Sorted(maps.Keys(roots)) {
		fmt.Fprintf(&b, "\t%q: %q,\n", r, roots[r])
	}
	closeTable(&b, v.prefix)
	out, err := format.Source(b.Bytes())
	if err != nil {
		fatal(fmt.Errorf("%s model: %w", v.name, err))
	}
	return out
}

// xmlSpaceSimple reads the anonymous type of xml:space from xml.xsd.
func xmlSpaceSimple() *genSimple {
	src, err := os.ReadFile(xmlXSD)
	if err != nil {
		fatal(err)
	}
	doc, err := tree.Parse(src)
	if err != nil {
		fatal(err)
	}
	for _, e := range doc.Root().Elements() {
		if n, _ := e.Attr("name"); e.Local() != "attribute" || n != "space" {
			continue
		}
		st := inlineSimple(e)
		if st == nil || len(st.Elements()) != 1 || st.Elements()[0].Local() != "restriction" {
			fatal(fmt.Errorf("xml.xsd: xml:space is not a restriction"))
		}
		r := st.Elements()[0]
		base, _ := r.Attr("base")
		_, local, _ := strings.Cut(base, ":")
		out := &genSimple{base: "xsd:" + local}
		for _, f := range r.Elements() {
			if v, ok := f.Attr("value"); ok && f.Local() == "enumeration" {
				out.enum = append(out.enum, v)
			}
		}
		return out
	}
	fatal(fmt.Errorf("xml.xsd: no attribute space"))
	return nil
}

// lazyPrefixes are the vocabularies whose tables are built on first use
// (sync.OnceValue) instead of at program start: SpreadsheetML, which only
// xlsx reads, so that docx and pptx do not pay for its maps (about 1.1 MB
// of startup RSS in docx). The eager tables stay variables.
var lazyPrefixes = []string{"x"}

// isLazy reports whether prefix's tables are built lazily.
func isLazy(prefix string) bool { return slices.Contains(lazyPrefixes, prefix) }

// lazyNote is the doc-comment sentence a lazy table adds.
func lazyNote(prefix string) string {
	if !isLazy(prefix) {
		return ""
	}
	return "\n// It is built on first use."
}

// openTable starts the table variable name of Go type typ: a map literal,
// or for a lazy vocabulary a sync.OnceValue that returns one.
func openTable(b *bytes.Buffer, prefix, name, typ string) {
	if isLazy(prefix) {
		fmt.Fprintf(b, "var %s = sync.OnceValue(func() %s {\n\treturn %s{\n", name, typ, typ)
		return
	}
	fmt.Fprintf(b, "var %s = %s{\n", name, typ)
}

// closeTable ends what openTable started.
func closeTable(b *bytes.Buffer, prefix string) {
	if isLazy(prefix) {
		b.WriteString("}\n})\n")
		return
	}
	b.WriteString("}\n")
}

// importsFor is the import block a table file of prefix needs.
func importsFor(prefix string) string {
	if isLazy(prefix) {
		return "\nimport \"sync\"\n"
	}
	return ""
}
