package coverage

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The universe the probes run against: the committed file.
var (
	uniOnce sync.Once
	uni     *Universe
	uniBy   map[string]*Item
	uniKids map[string][]*Item // items by parent path, in schema order
	uniOrd  map[string]int     // item path to its index among its siblings
)

func loadUniverse(t testing.TB) *Universe {
	t.Helper()
	uniOnce.Do(func() {
		u, err := LoadUniverse(filepath.Join(repoRoot(), "eval", "coverage", UniverseFile))
		if err != nil {
			panic(err)
		}
		uni = u
		uniBy = u.ByPath()
		uniKids = map[string][]*Item{}
		uniOrd = map[string]int{}
		for i := range u.Items {
			it := &u.Items[i]
			p := parentPath(it.Path)
			if p == "" {
				continue
			}
			uniOrd[it.Path] = len(uniKids[p])
			uniKids[p] = append(uniKids[p], it)
		}
	})
	return uni
}

func repoRoot() string { return filepath.Join("..", "..", "..") }

// parentPath drops the last step of a path ("" for a one-step path).
func parentPath(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

func lastStep(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Hosts are where a factored type's content is probed: a position of the
// type that the read model reads, when there is one.
var Hosts = map[string]string{
	"(a:CT_SchemeColor)":            "p:sp/p:spPr/a:solidFill/a:schemeClr",
	"(a:CT_SRgbColor)":              "p:sp/p:spPr/a:solidFill/a:srgbClr",
	"(a:CT_ScRgbColor)":             "p:sp/p:spPr/a:solidFill/a:scrgbClr",
	"(a:CT_HslColor)":               "p:sp/p:spPr/a:solidFill/a:hslClr",
	"(a:CT_SystemColor)":            "p:sp/p:spPr/a:solidFill/a:sysClr",
	"(a:CT_PresetColor)":            "p:sp/p:spPr/a:solidFill/a:prstClr",
	"(a:CT_GradientFillProperties)": "p:sp/p:spPr/a:gradFill",
	"(a:CT_PatternFillProperties)":  "p:sp/p:spPr/a:pattFill",
	"(a:CT_EffectList)":             "p:sp/p:spPr/a:effectLst",
	"(a:CT_EffectContainer)":        "p:sp/p:spPr/a:effectDag",
	"(a:CT_Scene3D)":                "p:sp/p:spPr/a:scene3d",
	"(a:CT_Shape3D)":                "p:sp/p:spPr/a:sp3d",
	"(a:CT_TextListStyle)":          "p:sp/p:txBody/a:lstStyle",
	"(a:CT_Blip)":                   "p:pic/p:blipFill/a:blip",
}

// rootParts says where each root of the universe lives in the probe deck:
// the part and the steps from its document element to the root element,
// "name#k" naming the k-th (0-based) child of that name. A root may have
// several contexts; the read probe tries each and keeps the strongest
// outcome. A shape is probed as the plain shape s2, as the body
// placeholder s4 (inheritance) and as the group child s8 (a:grpFill).
type loc struct {
	part  string
	steps []string
	// setup prepares the deck for the context, before the probe's
	// states are set.
	setup func(fixture)
	// id is the object's short id, for plans; writeOnly contexts are
	// plan targets the read probe does not use.
	id        string
	writeOnly bool
}

// withoutSlideClrMap removes the slide's colour map override, so that
// the layout's is used.
func withoutSlideClrMap(f fixture) {
	s := f[partSlide]
	if c := s.child("p:clrMapOvr"); c != nil {
		s.removeChild(c)
	}
}

// withStyle gives the plain shape s2 a style whose fill and line
// references select the theme's first entries, so that the format
// scheme is read.
func withStyle(f fixture) {
	sp := f[partSlide].child("p:cSld").child("p:spTree").child("p:sp")
	sp.kids = append(sp.kids[:2], append([]*el{parseEl(`<p:style ` + nsDecl + `><a:lnRef idx="1"><a:schemeClr val="accent1"/></a:lnRef><a:fillRef idx="1"><a:schemeClr val="accent1"/></a:fillRef><a:effectRef idx="0"><a:schemeClr val="accent1"/></a:effectRef><a:fontRef idx="minor"><a:schemeClr val="lt1"/></a:fontRef></p:style>`)}, sp.kids[2:]...)...)
}

var rootParts = map[string][]loc{
	"p:sp": {
		{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:sp#0"}, id: "s2"},
		{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:sp#2"}, id: "s4"},
		{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:grpSp#0", "p:sp#0"}, id: "s8"},
		{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:sp#3"}, id: "s6", writeOnly: true},
	},
	"p:cxnSp":                              {{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:cxnSp"}, id: "s5"}},
	"p:grpSp":                              {{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:grpSp"}, id: "s7"}},
	"p:pic":                                {{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:pic"}, id: "s9"}},
	"p:graphicFrame":                       {{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:graphicFrame"}, id: "s10"}},
	"p:contentPart":                        {{part: partSlide, steps: []string{"p:cSld", "p:spTree", "p:contentPart"}, id: "s11"}},
	"p:sld":                                {{part: partSlide}},
	"p:sldLayout/p:clrMapOvr":              {{part: partLayout, steps: []string{"p:clrMapOvr"}, setup: withoutSlideClrMap}},
	"p:sldMaster/p:clrMap":                 {{part: partMaster, steps: []string{"p:clrMap"}}},
	"p:sldMaster/p:txStyles":               {{part: partMaster, steps: []string{"p:txStyles"}}},
	"p:presentation/p:sldSz":               {{part: partPres, steps: []string{"p:sldSz"}}},
	"p:presentation/p:defaultTextStyle":    {{part: partPres, steps: []string{"p:defaultTextStyle"}}},
	"a:theme/a:themeElements/a:clrScheme":  {{part: partTheme, steps: []string{"a:themeElements", "a:clrScheme"}}},
	"a:theme/a:themeElements/a:fontScheme": {{part: partTheme, steps: []string{"a:themeElements", "a:fontScheme"}}},
	"a:theme/a:themeElements/a:fmtScheme":  {{part: partTheme, steps: []string{"a:themeElements", "a:fmtScheme"}, setup: withStyle}},
}

// contexts returns how many read contexts the root of path has.
func contexts(path string) int {
	root, _, err := resolve(path)
	if err != nil {
		return 1
	}
	n := 0
	for _, l := range rootParts[root] {
		if !l.writeOnly {
			n++
		}
	}
	return n
}

// hostPath rewrites a path under a pseudo-root to the same path at the
// pseudo-root's host, repeatedly, and returns it with the universe path
// of each step: the concrete steps to walk and the item that governs each.
type step struct {
	name string // element name or @attr
	item *Item  // the universe item of this step
}

// resolve splits a universe path into the root it starts from and the
// steps below it, rewriting pseudo-roots to their hosts.
func resolve(path string) (root string, steps []step, err error) {
	if strings.HasPrefix(path, "(") {
		i := strings.Index(path, ")")
		pseudo, rest := path[:i+1], strings.TrimPrefix(path[i+1:], "/")
		host, ok := Hosts[pseudo]
		if !ok {
			return "", nil, fmt.Errorf("no host for %s", pseudo)
		}
		root, steps, err = resolve(host)
		if err != nil || rest == "" {
			return root, steps, err
		}
		cur := pseudo
		for s := range strings.SplitSeq(rest, "/") {
			cur += "/" + s
			it := uniBy[cur]
			if it == nil {
				return "", nil, fmt.Errorf("%s: no item %s", path, cur)
			}
			steps = append(steps, step{s, it})
			if it.Factored != "" {
				cur = it.Factored
			}
		}
		return root, steps, nil
	}
	// The longest root that prefixes the path.
	for r := range rootParts {
		if (path == r || strings.HasPrefix(path, r+"/")) && len(r) > len(root) {
			root = r
		}
	}
	if root == "" {
		return "", nil, fmt.Errorf("%s: no root", path)
	}
	cur := root
	rest := strings.TrimPrefix(strings.TrimPrefix(path, root), "/")
	if rest == "" {
		return root, nil, nil
	}
	for s := range strings.SplitSeq(rest, "/") {
		cur += "/" + s
		it := uniBy[cur]
		if it == nil {
			return "", nil, fmt.Errorf("%s: no item %s", path, cur)
		}
		steps = append(steps, step{s, it})
		if it.Factored != "" {
			cur = it.Factored
		}
	}
	return root, steps, nil
}

// rootEl finds a root's element in context ctx of the fixture.
func (f fixture) rootEl(root string, ctx int) (*el, error) {
	locs, ok := rootParts[root]
	if !ok || ctx >= len(locs) {
		return nil, fmt.Errorf("unknown root %s context %d", root, ctx)
	}
	rp := locs[ctx]
	cur := f[rp.part]
	for _, s := range rp.steps {
		name, k := s, 0
		if n, idx, ok := strings.Cut(s, "#"); ok {
			name = n
			if _, err := fmt.Sscanf(idx, "%d", &k); err != nil {
				return nil, err
			}
		}
		var c *el
		for _, kid := range cur.kids {
			if kid.name == name {
				if k == 0 {
					c = kid
					break
				}
				k--
			}
		}
		if c == nil {
			return nil, fmt.Errorf("root %s: no %s", root, s)
		}
		cur = c
	}
	return cur, nil
}

// kidsOf returns the universe children of the element an item stands for
// (its pseudo-root's children when factored).
func kidsOf(it *Item) []*Item {
	p := it.Path
	if it.Factored != "" {
		p = it.Factored
	}
	return uniKids[p]
}

// newEl builds a minimal instance of an element item: its required
// attributes with sample values and its required child elements.
func newEl(it *Item, depth int, alt bool) *el {
	e := &el{name: lastStep(it.Path)}
	if depth > 6 {
		return e
	}
	for _, k := range kidsOf(it) {
		if k.Min < 1 {
			continue
		}
		switch k.Node {
		case "attribute":
			e.setAttr(strings.TrimPrefix(lastStep(k.Path), "@"), sample(k, alt))
		case "element":
			if k.Type != "" && isSimple(k) {
				c := &el{name: lastStep(k.Path)}
				c.text = sample(k, alt)
				e.kids = append(e.kids, c)
			} else {
				e.kids = append(e.kids, newEl(k, depth+1, alt))
			}
		}
	}
	if isSimple(it) {
		e.text = sample(it, alt)
	}
	return e
}

// sample is the first or (alt) second sample value of an item.
func sample(it *Item, alt bool) string {
	a, b := samples(it)
	if alt {
		return b
	}
	return a
}

// isSimple reports whether an element item has simple content.
func isSimple(it *Item) bool {
	if it.Node != "element" || it.Factored != "" {
		return false
	}
	if it.Enum != "" || strings.HasPrefix(it.Type, "xsd:") || strings.HasPrefix(it.Type, "s:") {
		return true
	}
	_, ok := uni.Simple[it.Type]
	return ok
}

// ensure returns parent's child for item it, creating it in schema order
// (and removing the other alternatives of its choice) when missing.
func ensure(parent *el, parentItemPath string, it *Item) *el {
	name := lastStep(it.Path)
	if c := parent.child(name); c != nil {
		return c
	}
	c := newEl(it, 0, false)
	insert(parent, parentItemPath, it, c)
	return c
}

// insert places c (the instance of item it) among parent's children in
// schema order, removing alternatives of the same choice.
func insert(parent *el, parentItemPath string, it *Item, c *el) {
	sibs := uniKids[parentItemPath]
	ord := func(name string) (int, *Item) {
		for i, s := range sibs {
			if lastStep(s.Path) == name {
				return i, s
			}
		}
		return -1, nil
	}
	if it.Choice != "" {
		var keep []*el
		for _, k := range parent.kids {
			if _, s := ord(k.name); s != nil && s.Choice == it.Choice && k.name != c.name {
				continue
			}
			keep = append(keep, k)
		}
		parent.kids = keep
	}
	mine, _ := ord(c.name)
	pos := len(parent.kids)
	for i, k := range parent.kids {
		if o, _ := ord(k.name); o > mine {
			pos = i
			break
		}
	}
	parent.kids = append(parent.kids[:pos], append([]*el{c}, parent.kids[pos:]...)...)
}

// place walks steps from the root element, creating missing elements,
// and returns the element holding the last step and that step. For an
// attribute the holder is its element; for an element, its parent.
func (f fixture) place(path string, ctx int) (*el, step, string, error) {
	root, steps, err := resolve(path)
	if err != nil {
		return nil, step{}, "", err
	}
	cur, err := f.rootEl(root, ctx)
	if err != nil {
		return nil, step{}, "", err
	}
	if len(steps) == 0 {
		// A root below its part's document element: its holder is its
		// parent.
		locs := rootParts[root]
		if ctx >= len(locs) || len(locs[ctx].steps) == 0 {
			return nil, step{}, "", fmt.Errorf("%s: a document root", path)
		}
		l := locs[ctx]
		cur = f[l.part]
		for _, s := range l.steps[:len(l.steps)-1] {
			if cur = cur.child(s); cur == nil {
				return nil, step{}, "", fmt.Errorf("%s: no %s", path, s)
			}
		}
		return cur, step{lastStep(path), uniBy[path]}, "", nil
	}
	parentItem := root
	for _, s := range steps[:len(steps)-1] {
		cur = ensure(cur, parentItem, s.item)
		parentItem = s.item.Path
		if s.item.Factored != "" {
			parentItem = s.item.Factored
		}
	}
	return cur, steps[len(steps)-1], parentItem, nil
}

// setState puts the item at path in state v: an attribute with value v
// ("" removes it), an element present (v != "") with sample content, or
// absent. Missing ancestors are created.
func (f fixture) setState(path string, it *Item, v string, present bool, ctx int, alt bool) error {
	holder, last, parentItem, err := f.place(path, ctx)
	if err != nil {
		return err
	}
	switch it.Node {
	case "attribute":
		name := strings.TrimPrefix(last.name, "@")
		if !present {
			holder.removeAttr(name)
		} else {
			holder.setAttr(name, v)
		}
	case "element":
		existing := holder.child(last.name)
		if !present {
			if existing != nil {
				holder.removeChild(existing)
			}
			return nil
		}
		if existing == nil || alt {
			if existing != nil {
				holder.removeChild(existing)
			}
			existing = newEl(it, 0, alt)
			insert(holder, parentItem, it, existing)
		}
		if v != "" && isSimple(it) {
			existing.text = v
		}
	default:
		return fmt.Errorf("%s: cannot probe %s", path, it.Node)
	}
	return nil
}

// samples returns two distinct valid values for an attribute or simple
// element item.
func samples(it *Item) (string, string) {
	if v, ok := pathSamples[it.Path]; ok {
		return v[0], v[1]
	}
	if it.Enum != "" {
		vals := uni.Enums[it.Enum]
		if len(vals) == 1 {
			return vals[0], vals[0]
		}
		return vals[0], vals[1]
	}
	if v, ok := typeSamples[it.Type]; ok {
		return v[0], v[1]
	}
	st, ok := uni.Simple[it.Type]
	base := it.Type
	if ok {
		base = st.Base
	}
	switch base {
	case "xsd:boolean":
		return "0", "1"
	case "xsd:unsignedInt", "xsd:unsignedShort", "xsd:unsignedByte", "xsd:unsignedLong":
		return bounded(st, "1", "2")
	case "xsd:int", "xsd:long", "xsd:integer", "xsd:short", "xsd:byte", "xsd:nonNegativeInteger", "xsd:positiveInteger":
		return bounded(st, "254000", "508000")
	case "xsd:double", "xsd:float", "xsd:decimal":
		return "1.5", "2.5"
	case "xsd:hexBinary":
		return "FF0000", "00FF00"
	case "xsd:dateTime":
		return "2020-01-01T00:00:00Z", "2021-01-01T00:00:00Z"
	}
	return "A", "B"
}

// pathSamples are sample values for items whose type says too little:
// an extension's uri means something only when it names an extension
// the reader knows (the SVG blip, and PowerPoint's useLocalDpi, which is
// not read).
var pathSamples = map[string][2]string{
	"(a:CT_Blip)/a:extLst/a:ext/@uri": {"{96DAC541-7B7A-43D3-8B79-37D633B846F1}", "{28A0092B-C50C-407E-A947-70E740481C1C}"},
}

// typeSamples are sample values for types whose built-in base does not
// say enough to pick valid ones.
var typeSamples = map[string][2]string{
	"a:ST_GeomGuideFormula":     {"val 10000", "val 20000"},
	"a:ST_GeomGuideName":        {"adj", "adj2"},
	"a:ST_TextTypeface":         {"Arial", "Courier New"},
	"s:ST_Lang":                 {"en-US", "de-DE"},
	"r:ST_RelationshipId":       {"rId2", "rId99"},
	"s:ST_Guid":                 {"{00000000-0000-0000-0000-000000000001}", "{00000000-0000-0000-0000-000000000002}"},
	"s:ST_Panose":               {"020B0604020202020204", "020F0502020204030204"},
	"s:ST_HexColorRGB":          {"FF0000", "00FF00"},
	"a:ST_HexColorRGB":          {"FF0000", "00FF00"},
	"a:ST_TextBulletStartAtNum": {"1", "5"},
	// Bullet sizes in the percentage-string form the schema gives.
	"a:ST_TextBulletSizePercent": {"75%", "50%"},
	// Style matrix index 1 and 2 select equal entries in the probe
	// theme; 0 selects none.
	"a:ST_StyleMatrixColumnIndex": {"1", "0"},
	// Percentages beyond 100% clamp colours to the same white.
	"a:ST_Percentage": {"25000", "50000"},
}

// bounded clamps two integer samples into a simple type's bounds.
func bounded(st SimpleType, a, b string) (string, string) {
	lo, hi := new(big.Int), new(big.Int)
	hasLo, hasHi := false, false
	if st.Min != "" {
		_, hasLo = lo.SetString(st.Min, 10)
	}
	if st.Max != "" {
		_, hasHi = hi.SetString(st.Max, 10)
	}
	clamp := func(s string) *big.Int {
		v, _ := new(big.Int).SetString(s, 10)
		if hasLo && v.Cmp(lo) < 0 {
			v.Set(lo)
		}
		if hasHi && v.Cmp(hi) > 0 {
			v.Set(hi)
		}
		return v
	}
	x, y := clamp(a), clamp(b)
	if x.Cmp(y) == 0 {
		switch {
		case hasLo && hasHi:
			x, y = lo, hi
		case hasHi:
			x = new(big.Int).Sub(hi, big.NewInt(1))
		default:
			y = new(big.Int).Add(x, big.NewInt(1))
		}
	}
	return x.String(), y.String()
}

// projection is what the views show of the probe slide: the full-detail
// JSON view, decoded.
type projection map[string]any

// projections caches what the views show of a fixture, by the fixture's
// bytes: many probe states are the same deck.
var projections sync.Map // [32]byte -> projection

func (f fixture) key() [32]byte {
	h := sha256.New()
	for _, n := range []string{partSlide, partLayout, partMaster, partPres, partTheme} {
		h.Write([]byte(n))
		h.Write(f[n].xml())
	}
	var k [32]byte
	copy(k[:], h.Sum(nil))
	return k
}

func (f fixture) project(t testing.TB, dir string) (projection, error) {
	k := f.key()
	if p, ok := projections.Load(k); ok {
		return p.(projection), nil
	}
	p, err := f.projectUncached(t, dir)
	if err == nil {
		projections.Store(k, p)
	}
	return p, err
}

func (f fixture) projectUncached(t testing.TB, dir string) (projection, error) {
	path := f.write(t, dir)
	defer os.Remove(path)
	pkg, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	d, err := model.OpenDeck(pkg)
	if err != nil {
		return nil, err
	}
	s, err := d.ReadSlide("/" + partSlide)
	if err != nil {
		return nil, err
	}
	b, err := view.JSON(s, view.Options{Detail: view.Full})
	b = testdeck.LocalIDs(b, s.Name)
	if err != nil {
		return nil, err
	}
	var p projection
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return p, nil
}

// strip returns v without the keys named.
func strip(v any, keys ...string) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			drop := false
			for _, d := range keys {
				if k == d {
					drop = true
				}
			}
			if !drop {
				out[k] = strip(e, keys...)
			}
		}
		return out
	case projection:
		return strip(map[string]any(x), keys...)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = strip(e, keys...)
		}
		return out
	}
	return v
}

// Probe outcomes, from the strongest.
const (
	seenModelled = "read-modelled"       // a value the views show changed
	seenOmitted  = "omitted-with-reason" // only the omissions changed
	seenRaw      = "preserved-raw"       // only raw XML shown changed
	seenNothing  = ""                    // the views did not change
)

// classify compares two projections.
func classify(a, b projection) string {
	switch {
	case !reflect.DeepEqual(strip(a, "xml", "omitted", "notes"), strip(b, "xml", "omitted", "notes")):
		return seenModelled
	case !reflect.DeepEqual(strip(a, "xml"), strip(b, "xml")):
		return seenOmitted
	case !reflect.DeepEqual(a, b):
		return seenRaw
	}
	return seenNothing
}

// hints are content a path needs around it to have an effect: by path
// suffix, a path relative to the item's parent element that is set in
// every state of the probe. A custom path's extent scales its points, so
// it shows only when the path has one.
//
// A hint is "relative/path" or "relative/path=value"; "../" steps up from
// the item's parent element. A custom geometry's guides show only when a
// path point refers to them by name ("adj" is the sample guide name).
var hints = map[string]string{
	"/a:path/@w":                     "a:lnTo/a:pt/@x",
	"/a:path/@h":                     "a:lnTo/a:pt/@y",
	"/a:path/@fill":                  "a:lnTo/a:pt/@x",
	"/a:path/@stroke":                "a:lnTo/a:pt/@x",
	"/a:custGeom/a:gdLst/a:gd/@fmla": "../../a:pathLst/a:path/a:lnTo/a:pt/@x=adj",
	"/a:custGeom/a:gdLst/a:gd/@name": "../../a:pathLst/a:path/a:lnTo/a:pt/@x=adj",
	"/a:custGeom/a:avLst/a:gd/@fmla": "../../a:pathLst/a:path/a:lnTo/a:pt/@x=adj",
	"/a:custGeom/a:avLst/a:gd/@name": "../../a:pathLst/a:path/a:lnTo/a:pt/@x=adj",
}

// hintPath resolves a hint against an item's path: the path it sets and
// the value ("" for the sample).
func hintPath(path, hint string) (string, string) {
	rel, val, _ := strings.Cut(hint, "=")
	base := parentPath(path)
	for strings.HasPrefix(rel, "../") {
		base, rel = parentPath(base), strings.TrimPrefix(rel, "../")
	}
	return base + "/" + rel, val
}

// rank orders probe outcomes, strongest first.
var rank = map[string]int{seenModelled: 3, seenOmitted: 2, seenRaw: 1, seenNothing: 0}

func stronger(a, b string) string {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// probeRead puts path in two states in every context of its root and
// classifies what the views show, keeping the strongest outcome. For an
// attribute or simple element the states are two sample values (va, vb
// when given); for an element, absent and present with either sample
// content (three states, compared pairwise).
func probeRead(t testing.TB, dir, path string, va, vb string) (string, error) {
	it := uniBy[path]
	if it == nil {
		return "", fmt.Errorf("no item %s", path)
	}
	best := seenNothing
	for ctx := 0; ctx < contexts(path); ctx++ {
		var states []fixture
		mk := func(v string, present, alt bool) error {
			f := newFixture(t)
			root, _, _ := resolve(path)
			if setup := rootParts[root][ctx].setup; setup != nil {
				setup(f)
			}
			for suffix, extra := range hints {
				if strings.HasSuffix(path, suffix) {
					xp, xv := hintPath(path, extra)
					if xv == "" {
						xv = sample(uniBy[xp], false)
					}
					if err := f.setState(xp, uniBy[xp], xv, true, ctx, false); err != nil {
						return err
					}
				}
			}
			if err := f.setState(path, it, v, present, ctx, alt); err != nil {
				return err
			}
			states = append(states, f)
			return nil
		}
		var err error
		switch {
		case it.Node == "attribute" || isSimple(it):
			a, b := va, vb
			if a == "" && b == "" {
				a, b = samples(it)
			}
			if a == b {
				// A single-valued enumeration: absent against present.
				err = mk("", false, false)
			} else {
				err = mk(a, true, false)
			}
			if err == nil {
				err = mk(b, true, false)
			}
		case it.Node == "element":
			if err = mk("", false, false); err == nil {
				if err = mk("", true, false); err == nil {
					err = mk("", true, true)
				}
			}
		default:
			return "", fmt.Errorf("%s: not probed", path)
		}
		if err != nil {
			return "", err
		}
		var ps []projection
		for i, f := range states {
			p, err := f.project(t, dir)
			if err != nil {
				return "", fmt.Errorf("context %d state %d: %w", ctx, i, err)
			}
			ps = append(ps, p)
		}
		for i := range ps {
			for j := i + 1; j < len(ps); j++ {
				best = stronger(best, classify(ps[i], ps[j]))
			}
		}
		if best == seenModelled {
			break
		}
	}
	return best, nil
}
