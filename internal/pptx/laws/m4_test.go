package laws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The M4 laws: custom geometry paths (identity through the views'
// form, rebuild from the shown values, inverse, untargeted bytes,
// atomicity) and the group operations (create then ungroup is the
// identity in slide space; ungroup and fit leave every child where it
// was, or refuse).

type m4counts struct {
	decks, custom int
	// Rebuild: each a:path of an own custGeom with literal points, built
	// again from the values the EMU view shows.
	rebuildPaths, rebuildSame, freshSame int
	rebuildSkip, rebuildDiff             map[string]int
	inverseOK, inverseInexpressible      int
	inverseRefused                       map[string]int
	switchOK                             int
	untargeted, atomic                   int
	untargetedBudget                     int // per deck, for ungroup and fit
	// Groups.
	pairs, pairsIdentity, pairsBytes int
	pairRefused                      map[string]int
	pairDiff                         map[string]int
	groups                           int
	ungroupOK, ungroupExact          int
	ungroupRefused                   map[string]int
	fitOK, fitChanged, fitExact      int
	fitRefused                       map[string]int
	maxMoved                         float64
}

func newM4Counts() *m4counts {
	return &m4counts{rebuildSkip: map[string]int{}, rebuildDiff: map[string]int{}, inverseRefused: map[string]int{},
		pairRefused: map[string]int{}, pairDiff: map[string]int{}, ungroupRefused: map[string]int{}, fitRefused: map[string]int{}}
}

func keys(m map[string]int) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var s []string
	for _, k := range ks {
		s = append(s, fmt.Sprintf("%s %d", k, m[k]))
	}
	return "{" + strings.Join(s, ", ") + "}"
}

func (c *m4counts) String() string {
	return fmt.Sprintf("decks %d, custom geometries %d; rebuild: %d paths, %d byte-identical through the write path, %d identical as fresh XML, differ %s, skipped %s; inverse: %d paths read back as set, %d ErrInexpressible, refused %s; preset->custom->preset switches %d; untargeted plans %d; atomic %d; "+
		"create+ungroup: %d pairs, %d shown-identical, %d byte-identical, differ %s, refused %s; ungroup: %d groups, %d ok (%d shown-identical), refused %s; fit: %d ok (%d changed the frame, %d shown-identical), refused %s; largest child move %.4f pt",
		c.decks, c.custom, c.rebuildPaths, c.rebuildSame, c.freshSame, keys(c.rebuildDiff), keys(c.rebuildSkip), c.inverseOK, c.inverseInexpressible, keys(c.inverseRefused), c.switchOK, c.untargeted, c.atomic,
		c.pairs, c.pairsIdentity, c.pairsBytes, keys(c.pairDiff), keys(c.pairRefused), c.groups, c.ungroupOK, c.ungroupExact, keys(c.ungroupRefused), c.fitOK, c.fitChanged, c.fitExact, keys(c.fitRefused), c.maxMoved)
}

// compilePlan is plan without the panic.
func compilePlan(slide string, specs ...map[string]any) ([]ops.Op, error) {
	p, err := ops.ParsePlan(testdeck.Plan(slide, specs...))
	if err != nil {
		return nil, err
	}
	return p.Compile()
}

// refusal names an error's kind for the tallies.
func refusal(err error) string {
	switch {
	case errors.Is(err, prop.ErrInexpressible):
		return "ErrInexpressible"
	case errors.Is(err, edit.ErrBranch):
		return "ErrBranch"
	case errors.Is(err, ops.ErrReadOnlyObject):
		return "ErrReadOnlyObject"
	case errors.Is(err, prop.ErrNotDerived):
		return "ErrNotDerived"
	}
	return "other"
}

// geomKeys are the view keys that place an object in slide space.
var geomKeys = []string{"box", "rot", "flip_h", "flip_v", "from", "to", "elbows", "path"}

// placement returns what the view shows of an object's place.
func placement(o rawObj) map[string]string {
	out := map[string]string{}
	for _, k := range geomKeys {
		if v, ok := o[k]; ok {
			out[k] = compact(v)
		}
	}
	return out
}

// numbers extracts the numbers of a compact JSON value, for tolerant
// comparison.
func numbers(s string) []float64 {
	var out []float64
	f := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r == '.' || r == '-' || r == 'e')
	})
	for _, x := range f {
		if v, err := strconv.ParseFloat(x, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// moved compares two placements: identical shown values, or the largest
// difference between them (in points; +Inf when the shapes differ).
func moved(a, b map[string]string) (bool, float64) {
	same := true
	d := 0.0
	for _, k := range geomKeys {
		x, y := a[k], b[k]
		if x == y {
			continue
		}
		same = false
		if k == "rot" && (x == "" || y == "") {
			// rot 0 is not shown; compare as numbers.
			if x == "" {
				x = "0"
			}
			if y == "" {
				y = "0"
			}
		}
		nx, ny := numbers(x), numbers(y)
		if len(nx) != len(ny) {
			return false, math.Inf(1)
		}
		for i := range nx {
			v := math.Abs(nx[i] - ny[i])
			if k == "rot" {
				v = 0 // angles are not lengths; any difference is reported by same
			}
			d = math.Max(d, v)
		}
	}
	return same, d
}

func bySlideID(slides []rawSlide) map[string]rawObj {
	out := map[string]rawObj{}
	for _, s := range slides {
		for _, o := range s.objects {
			out[s.name+"#"+o.str("id")] = o
		}
	}
	return out
}

// checkM4Deck runs the M4 laws on one deck. budget bounds the plans
// that write files per deck and law.
func checkM4Deck(t *testing.T, src string, budget int, c *m4counts) {
	path := private(t, src)
	slides, err := readRaw(path)
	if err != nil {
		t.Logf("unreadable: %v", err)
		return
	}
	c.decks++
	c.untargetedBudget = budget
	rng := rand.New(rand.NewPCG(uint64(len(src)), 4))
	pathLaws(t, path, slides, rng, budget, c)
	groupLaws(t, path, slides, budget, c)
}

func pathLaws(t *testing.T, path string, slides []rawSlide, rng *rand.Rand, budget int, c *m4counts) {
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w := edit.New(p)
	type cand struct {
		slide string
		o     rawObj
	}
	var cands, presets []cand
	for _, s := range slides {
		for _, o := range s.objects {
			if o.str("kind") == "shape" && o.str("geom") != "" && o.str("geom") != "custom" && !derive.IsConnectorPreset(o.str("geom")) && o["box"] != nil {
				presets = append(presets, cand{s.name, o})
			}
			if o.str("geom") != "custom" {
				continue
			}
			c.custom++
			if o["path"] != nil && o.str("kind") != "connector" {
				cands = append(cands, cand{s.name, o})
			}
			// Rebuild every own literal path from the EMU values shown.
			ref, err := w.Resolve(s.name + "#" + strings.TrimPrefix(o.str("id"), "s"))
			if err != nil {
				continue
			}
			h, err := w.Peek(ref)
			if err != nil {
				c.rebuildSkip["not targetable"]++
				continue
			}
			cgEl := h.Find(edit.Path{edit.P("spPr"), edit.A("custGeom")})
			if cgEl == nil {
				c.rebuildSkip["inherited"]++
				continue
			}
			cg := derive.ReadCustomGeom(cgEl)
			arc := false
			for _, cp := range cg.Paths {
				for _, cmd := range cp.Cmds {
					arc = arc || cmd.Op == "arcTo"
				}
			}
			switch {
			case arc:
				c.rebuildSkip["arcTo"]++
				continue
			case cg.PathUsesGuides():
				c.rebuildSkip["formula guides"]++
				continue
			}
			v, ok := prop.Path(prop.EMU).Read(h, 0).Get()
			if !ok {
				c.rebuildSkip["path not shown"]++
				continue
			}
			m, sw, sh, err := prop.LocalFrame(h)
			if err != nil {
				c.rebuildSkip["no box"]++
				continue
			}
			kx, ky := derive.AxisScales(m)
			pl := h.Find(edit.Path{edit.P("spPr"), edit.A("custGeom"), edit.A("pathLst")})
			olds := pl.Elements()
			for k, item := range v {
				c.rebuildPaths++
				orig := olds[k].Bytes()
				b, err := prop.BuildPath(olds[k], pl.Prefix(), item, sw, sh, kx, ky, prop.EMU)
				switch {
				case err != nil:
					c.rebuildDiff["refused: "+refusal(err)]++
				case bytes.Equal(b, orig):
					c.rebuildSame++
				default:
					c.rebuildDiff[rebuildReason(orig, b)]++
					if len(c.rebuildDiff) < 3 {
						t.Logf("%s %s path %d rebuilt differently:\n%s\n%s", s.name, o.str("id"), k, orig, b)
					}
				}
				if fb, err := prop.BuildPath(nil, pl.Prefix(), item, sw, sh, kx, ky, prop.EMU); err == nil && bytes.Equal(fb, orig) {
					c.freshSame++
				}
			}
		}
	}
	p.Close()

	// Inverse: a jittered path set through a plan reads back as set.
	rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	if len(cands) > budget {
		cands = cands[:budget]
	}
	for _, cd := range cands {
		var cur any
		_ = json.Unmarshal(cd.o["path"], &cur)
		want := jitterPath(cur, rng)
		dst := filepath.Join(t.TempDir(), "path.pptx")
		opsList, err := compilePlan(cd.slide, map[string]any{"op": "object.set", "at": cd.o.str("id"), "path": want})
		if err != nil {
			t.Errorf("%s %s: compile %v: %v", cd.slide, cd.o.str("id"), want, err)
			continue
		}
		if err := ops.ApplyTo(path, dst, opsList...); err != nil {
			switch r := refusal(err); r {
			case "ErrInexpressible":
				c.inverseInexpressible++
			default:
				c.inverseRefused[r]++
				if r == "other" {
					t.Errorf("%s %s: set path: %v", cd.slide, cd.o.str("id"), err)
				}
			}
			continue
		}
		after, err := readRawOnly(dst, cd.slide)
		if err != nil {
			t.Fatal(err)
		}
		got := compact(bySlideID(after)[cd.slide+"#"+cd.o.str("id")]["path"])
		wb, _ := json.Marshal(want)
		if got != compact(wb) {
			t.Errorf("%s %s: path shows %s after setting %s", cd.slide, cd.o.str("id"), got, wb)
			continue
		}
		c.inverseOK++
		id := cd.o.str("id")
		checkUntargeted(t, path, dst, cd.slide, func(_, _ map[string]*model.Object) map[string]bool { return map[string]bool{id: true} })
		c.untargeted++
	}
	// A preset becomes a custom geometry and back.
	rng.Shuffle(len(presets), func(i, j int) { presets[i], presets[j] = presets[j], presets[i] })
	if len(presets) > budget {
		presets = presets[:budget]
	}
	for _, cd := range presets {
		var box [4]float64
		_ = json.Unmarshal(cd.o["box"], &box)
		d := fmt.Sprintf("M 0 0 L %s 0 L %s %s Z", derive.FormatNum(box[2]), derive.FormatNum(box[2]/2), derive.FormatNum(box[3]))
		id := cd.o.str("id")
		mid := filepath.Join(t.TempDir(), "switch.pptx")
		if err := ops.ApplyTo(path, mid, plan(cd.slide, map[string]any{"op": "object.set", "at": id, "path": d})...); err != nil {
			c.inverseRefused["switch: "+refusal(err)]++
			continue
		}
		s1, _ := readRawOnly(mid, cd.slide)
		o1 := bySlideID(s1)[cd.slide+"#"+id]
		if o1.str("geom") != "custom" || o1.str("path") != d {
			t.Errorf("%s %s: after the switch geom %s path %s, want custom %s", cd.slide, id, o1.str("geom"), o1.str("path"), d)
			continue
		}
		back := filepath.Join(t.TempDir(), "back.pptx")
		if err := ops.ApplyTo(mid, back, plan(cd.slide, map[string]any{"op": "object.set", "at": id, "geom": cd.o.str("geom")})...); err != nil {
			t.Errorf("%s %s: switch back: %v", cd.slide, id, err)
			continue
		}
		s2, _ := readRawOnly(back, cd.slide)
		if g := bySlideID(s2)[cd.slide+"#"+id].str("geom"); g != cd.o.str("geom") {
			t.Errorf("%s %s: switched back to %s, want %s", cd.slide, id, g, cd.o.str("geom"))
			continue
		}
		c.switchOK++
	}
	// Atomicity: a path set followed by a failing operation writes nothing.
	if len(cands) > 0 {
		cd := cands[0]
		b0, _ := os.ReadFile(path)
		i0, _ := os.Stat(path)
		err := ops.Apply(path, plan(cd.slide,
			map[string]any{"op": "object.set", "at": cd.o.str("id"), "path": "M 0 0 L 10 10"},
			map[string]any{"op": "object.set", "at": "s999999", "name": "x"})...)
		b1, _ := os.ReadFile(path)
		i1, _ := os.Stat(path)
		if err == nil || !bytes.Equal(b0, b1) || !i0.ModTime().Equal(i1.ModTime()) {
			t.Errorf("%s: a failed path plan changed the file (err %v)", cd.slide, err)
		} else {
			c.atomic++
		}
	}
}

// slidePart returns one part's bytes.
func slidePart(t *testing.T, path, part string) []byte {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pt, err := p.Part(part)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pt.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rebuildReason classifies how a rebuilt path differs from the original.
func rebuildReason(orig, b []byte) string {
	strip := func(x []byte) []byte {
		return bytes.Join(bytes.Fields(x), nil)
	}
	if bytes.Equal(strip(orig), strip(b)) {
		return "whitespace inside the path"
	}
	o, n := string(orig), string(b)
	if i := strings.Index(o, ">"); i > 0 {
		if j := strings.Index(n, ">"); j > 0 && o[i:] == n[j:] {
			return "start tag only"
		}
	}
	return "points"
}

// jitterPath moves every coordinate of a shown path value by up to 5 pt,
// keeping its commands and modes.
func jitterPath(v any, rng *rand.Rand) any {
	j := func(d string) string {
		f := strings.Fields(d)
		for i, s := range f {
			if x, err := strconv.ParseFloat(s, 64); err == nil {
				f[i] = derive.FormatNum(x + float64(rng.IntN(1001)-500)/100)
			}
		}
		return strings.Join(f, " ")
	}
	switch x := v.(type) {
	case string:
		return j(x)
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			m := map[string]any{}
			maps.Copy(m, it.(map[string]any))
			m["d"] = j(m["d"].(string))
			out[i] = m
		}
		return out
	}
	return v
}

func groupLaws(t *testing.T, path string, slides []rawSlide, budget int, c *m4counts) {
	before := bySlideID(slides)
	// create then ungroup: siblings next to each other in one container.
	pairs := 0
	for _, s := range slides {
		var prev rawObj
		for _, o := range s.objects {
			mo := s.model[o.str("id")]
			if prev != nil && pairs < budget && s.model[prev.str("id")].Parent == mo.Parent && o.str("kind") != "other" && prev.str("kind") != "other" {
				pairs++
				createUngroup(t, path, s, prev, o, before, c)
			}
			prev = o
		}
	}
	// Ungroup and fit every group.
	for _, s := range slides {
		for _, o := range s.objects {
			if o.str("kind") != "group" {
				continue
			}
			c.groups++
			id := o.str("id")
			ungroupOne(t, path, s, id, before, c)
			fitOne(t, path, s, id, before, c)
		}
	}
}

func descendants(s rawSlide, id string) []string {
	var out []string
	for _, o := range s.objects {
		for p := s.model[o.str("id")]; p != nil && p.Parent != ""; p = s.model[p.Parent] {
			if p.Parent == id {
				out = append(out, o.str("id"))
				break
			}
		}
	}
	return out
}

// samePlace compares the places of ids before and after, recording the
// largest move.
func samePlace(t *testing.T, what, slide string, ids []string, before, after map[string]rawObj, c *m4counts) (exact bool, ok bool) {
	exact, ok = true, true
	for _, id := range ids {
		a, b := before[slide+"#"+id], after[slide+"#"+id]
		if b == nil {
			t.Errorf("%s %s: %s is gone", what, slide, id)
			return false, false
		}
		same, d := moved(placement(a), placement(b))
		if !same {
			exact = false
			c.maxMoved = math.Max(c.maxMoved, d)
			if d > 0.011 {
				t.Errorf("%s %s: %s moved: %v -> %v", what, slide, id, placement(a), placement(b))
				ok = false
			}
		}
	}
	return exact, ok
}

func createUngroup(t *testing.T, path string, s rawSlide, a, b rawObj, before map[string]rawObj, c *m4counts) {
	dst := filepath.Join(t.TempDir(), "cu.pptx")
	err := ops.ApplyTo(path, dst, plan(s.name,
		map[string]any{"op": "group.create", "at": []string{a.str("id"), b.str("id")}, "name": "G", "as": "g"},
		map[string]any{"op": "group.ungroup", "at": "$g"})...)
	if err != nil {
		c.pairRefused[refusal(err)]++
		t.Logf("%s %s+%s: create+ungroup refused: %v", s.name, a.str("id"), b.str("id"), err)
		if refusal(err) == "other" {
			t.Errorf("%s %s+%s: create+ungroup: %v", s.name, a.str("id"), b.str("id"), err)
		}
		return
	}
	c.pairs++
	after, err := readRawOnly(dst, s.name)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range s.objects {
		ids = append(ids, o.str("id"))
	}
	exact, _ := samePlace(t, "create+ungroup", s.name, ids, before, bySlideID(after), c)
	if exact {
		c.pairsIdentity++
	}
	pb, pa := slidePart(t, path, s.part), slidePart(t, dst, s.part)
	if bytes.Equal(pb, pa) {
		c.pairsBytes++
	} else {
		x, y := pb, pa
		switch {
		case bytes.Equal(bytes.Join(bytes.Fields(x), nil), bytes.Join(bytes.Fields(y), nil)):
			c.pairDiff["whitespace between objects"]++
		case len(x) == len(y):
			c.pairDiff["object order"]++
		default:
			c.pairDiff["content"]++
			t.Errorf("%s %s+%s: create+ungroup changed content", s.name, a.str("id"), b.str("id"))
		}
	}
}

func ungroupOne(t *testing.T, path string, s rawSlide, id string, before map[string]rawObj, c *m4counts) {
	dst := filepath.Join(t.TempDir(), "ungroup.pptx")
	err := ops.ApplyTo(path, dst, plan(s.name, map[string]any{"op": "group.ungroup", "at": id})...)
	if err != nil {
		c.ungroupRefused[refusal(err)]++
		if r := refusal(err); r == "other" || r == "ErrNotDerived" {
			t.Logf("%s %s: ungroup: %v", s.name, id, err)
		}
		return
	}
	after, err := readRawOnly(dst, s.name)
	if err != nil {
		t.Fatal(err)
	}
	exact, ok := samePlace(t, "ungroup", s.name, descendants(s, id), before, bySlideID(after), c)
	if !ok {
		return
	}
	c.ungroupOK++
	if exact {
		c.ungroupExact++
	}
	kids := map[string]bool{id: true}
	for _, o := range s.objects {
		if s.model[o.str("id")].Parent == id {
			kids[o.str("id")] = true
		}
	}
	if c.untargetedBudget > 0 {
		c.untargetedBudget--
		c.untargeted++
		checkUntargeted(t, path, dst, s.name, func(_, _ map[string]*model.Object) map[string]bool { return kids })
	}
}

func fitOne(t *testing.T, path string, s rawSlide, id string, before map[string]rawObj, c *m4counts) {
	dst := filepath.Join(t.TempDir(), "fit.pptx")
	err := ops.ApplyTo(path, dst, plan(s.name, map[string]any{"op": "group.fit", "at": id})...)
	if err != nil {
		c.fitRefused[refusal(err)]++
		if r := refusal(err); r == "other" {
			t.Errorf("%s %s: fit: %v", s.name, id, err)
		}
		return
	}
	after, err := readRawOnly(dst, s.name)
	if err != nil {
		t.Fatal(err)
	}
	exact, ok := samePlace(t, "fit", s.name, descendants(s, id), before, bySlideID(after), c)
	if !ok {
		return
	}
	c.fitOK++
	if exact {
		c.fitExact++
	}
	if pb, pa := slidePart(t, path, s.part), slidePart(t, dst, s.part); !bytes.Equal(pb, pa) {
		c.fitChanged++
	}
	if c.untargetedBudget > 0 {
		c.untargetedBudget--
		c.untargeted++
		checkUntargeted(t, path, dst, s.name, func(_, _ map[string]*model.Object) map[string]bool { return map[string]bool{id: true} })
	}
}

func TestM4LawsFixtures(t *testing.T) {
	c := newM4Counts()
	all := testdeck.All(t)
	for _, n := range []string{"minimal", "quirky", "soffice", "diagram"} {
		t.Run(n, func(t *testing.T) { checkM4Deck(t, all[n], 1000, c) })
	}
	t.Log(c)
	if c.inverseOK == 0 || c.pairs == 0 || c.ungroupOK == 0 || c.fitOK == 0 || c.switchOK == 0 {
		t.Fatalf("laws exercised nothing: %v", c)
	}
}

func TestM4LawsCorpus(t *testing.T) {
	t.Parallel() // beside the other corpus suites (corpus_test.go)
	files, _ := nativeFiles(t)
	c := newM4Counts()
	for _, r := range corpustest.Run(t, files, func(t *testing.T, f corpustest.File, d **m4counts) {
		*d = newM4Counts()
		checkM4Deck(t, f.Path, 3, *d)
	}) {
		c.add(r.Value)
	}
	t.Log(c)
}
