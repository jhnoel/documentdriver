package laws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The M3 laws: the property codecs (geometry, fill, line, glue,
// endpoints, name) and the operations (object.add, set, move, delete,
// duplicate, reorder), end to end through plans in the views' form.

// rawObj is one object of the JSON view (default detail), by key.
type rawObj map[string]json.RawMessage

func (o rawObj) str(k string) string {
	var s string
	_ = json.Unmarshal(o[k], &s)
	return s
}

// cand is an object a law writes to.
type cand struct {
	slide string
	o     rawObj
}

type rawSlide struct {
	name    string
	part    string
	objects []rawObj
	model   map[string]*model.Object
}

func readRaw(path string) ([]rawSlide, error) { return readRawOnly(path, "") }

// readRawOnly is readRaw limited to one slide (all when only is "").
func readRawOnly(path, only string) ([]rawSlide, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		return nil, err
	}
	var out []rawSlide
	for _, part := range d.Slides {
		if only != "" && model.ShortName(part) != only {
			continue
		}
		s, err := readComplete(d, part)
		if err != nil {
			return nil, err
		}
		b, err := view.JSON(s, view.Options{})
		b = testdeck.LocalIDs(b, s.Name)
		if err != nil {
			return nil, err
		}
		var v, full struct {
			Objects []rawObj `json:"objects"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		// The text body properties ("text") appear in --detail full only;
		// take them from it.
		fb, err := view.JSON(s, view.Options{Detail: view.Full})
		fb = testdeck.LocalIDs(fb, s.Name)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(fb, &full); err != nil {
			return nil, err
		}
		for i, o := range full.Objects {
			for _, k := range []string{"text", "custom"} {
				if t, ok := o[k]; ok && i < len(v.Objects) && v.Objects[i].str("id") == o.str("id") {
					v.Objects[i][k] = t
				}
			}
		}
		rs := rawSlide{name: s.Name, part: part, objects: v.Objects, model: map[string]*model.Object{}}
		for _, o := range s.Objects {
			rs.model[o.ID] = o
		}
		out = append(out, rs)
	}
	return out, nil
}

// writableKeys are the view keys object.set takes back in the same form.
var writableKeys = []string{"name", "descr", "title", "hidden", "tx_box", "geom", "adj", "path", "custom", "fill", "line", "from_glue", "to_glue", "from", "to", "first", "elbows", "text"}

type m3counts struct {
	decks, objects, notTargetable                  int
	paired                                         map[string]int
	identityFields                                 int
	inverseOK, inverseInexpressible, inverseBranch int
	inverseByCodec                                 map[string]int
	opPlans                                        map[string]int
	opInexpressible                                int
	dupDeleteRename, addDelete                     int
	textRefused                                    map[string]int
	textReadBack, textAtomic                       int
}

func (c *m3counts) String() string {
	keys := func(m map[string]int) string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		var s []string
		for _, k := range ks {
			s = append(s, fmt.Sprintf("%s %d", k, m[k]))
		}
		return strings.Join(s, ", ")
	}
	return fmt.Sprintf("decks %d, objects %d (not targetable %d); codec=view pairs: %s; identity: %d fields written back as shown; inverse: %d sets read back (%s), %d ErrInexpressible, %d ErrBranch; op plans: %s (+%d inexpressible); duplicate+delete = rename %d; add+delete = no-op %d; text: %d sets read back as Normalise(h), refused: %s; failed text plans left the file untouched %d",
		c.decks, c.objects, c.notTargetable, keys(c.paired), c.identityFields, c.inverseOK, keys(c.inverseByCodec), c.inverseInexpressible, c.inverseBranch, keys(c.opPlans), c.opInexpressible, c.dupDeleteRename, c.addDelete,
		c.textReadBack, keys(c.textRefused), c.textAtomic)
}

func compact(b json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		return string(b)
	}
	return out.String()
}

// codecShows returns what each codec shows for the object ("" key absent
// when the codec does not read it).
func codecShows(h *edit.Handle) map[string]string {
	out := map[string]string{}
	if v, ok := prop.Geometry().Read(h, 0).Get(); ok {
		out["geom"] = prop.Geometry().Show(v)
	}
	if v, ok := prop.Path(prop.Pt).Read(h, 0).Get(); ok {
		out["path"] = prop.Path(prop.Pt).Show(v)
	}
	if v, ok := prop.Custom(prop.Pt).Read(h, 0).Get(); ok {
		out["custom"] = prop.Custom(prop.Pt).Show(v)
	}
	if v, ok := prop.Fill().Read(h, 0).Get(); ok {
		out["fill"] = prop.Fill().Show(v)
	}
	if v, ok := prop.Line(prop.Pt).Read(h, 0).Get(); ok {
		out["line"] = prop.Line(prop.Pt).Show(v)
	}
	for _, g := range []string{"stCxn", "endCxn"} {
		c := prop.Glue(g)
		if v, ok := c.Read(h, 0).Get(); ok && v != nil {
			out[c.Name] = c.Show(v)
		}
	}
	if v, ok := prop.Endpoints(prop.Pt).Read(h, 0).Get(); ok {
		out["endpoints"] = prop.Endpoints(prop.Pt).Show(v)
	}
	if v, ok := prop.Name().Read(h, 0).Get(); ok {
		out["name"] = v
	}
	if v, ok := prop.Body(prop.Pt).Read(h, 0).Get(); ok {
		out["text"] = prop.Body(prop.Pt).Show(v)
	}
	for _, a := range []string{"descr", "title"} {
		if v, ok := prop.CNvPrText(a).Read(h, 0).Get(); ok {
			out[a] = v
		}
	}
	if v, ok := prop.Hidden().Read(h, 0).Get(); ok {
		out["hidden"] = prop.Hidden().Show(v)
	}
	if v, ok := prop.TxBox().Read(h, 0).Get(); ok {
		out["tx_box"] = prop.TxBox().Show(v)
	}
	return out
}

// viewBody shows the view's "text" object in the body codec's form (the
// compact JSON both show through show.BodyJSON).
func viewBody(raw json.RawMessage) string { return compact(raw) }

// viewShows returns the same properties as the view shows them, in the
// codecs' form.
func viewShows(o rawObj) map[string]string {
	out := map[string]string{}
	if g, ok := o["geom"]; ok {
		var s string
		_ = json.Unmarshal(g, &s)
		v := "geom=" + s
		if a, ok := o["adj"]; ok {
			v += " adj=" + compact(a)
		}
		out["geom"] = v
	}
	for _, k := range []string{"path", "custom"} {
		if v, ok := o[k]; ok {
			out[k] = compact(v)
		}
	}
	if f, ok := o["fill"]; ok {
		out["fill"] = compact(f)
	}
	if l, ok := o["line"]; ok {
		out["line"] = compact(l)
	}
	for _, k := range []string{"from_glue", "to_glue"} {
		if _, ok := o[k]; ok {
			out[k] = o.str(k)
		}
	}
	if f, ok := o["from"]; ok {
		var a, b [2]json.Number
		_ = json.Unmarshal(f, &a)
		_ = json.Unmarshal(o["to"], &b)
		v := fmt.Sprintf("from=[%s %s] to=[%s %s]", a[0], a[1], b[0], b[1])
		if fs := o.str("first"); fs != "" {
			v += " first=" + fs
		}
		out["endpoints"] = v
	}
	out["name"] = o.str("name")
	if t, ok := o["text"]; ok {
		out["text"] = viewBody(t)
	}
	out["descr"], out["title"] = o.str("descr"), o.str("title")
	flag := func(k string) string {
		if compact(o[k]) == "true" {
			return "true"
		}
		return "false"
	}
	out["hidden"] = flag("hidden")
	if o.str("kind") == "shape" {
		out["tx_box"] = flag("tx_box")
	}
	return out
}

// handles opens a workspace over path and returns a targeting function.
func handles(t *testing.T, path string) (func(slide, id string) (*edit.Handle, error), func()) {
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w := edit.New(p)
	return func(slide, id string) (*edit.Handle, error) {
		r, err := w.Resolve(slide + "#" + strings.TrimPrefix(id, "s"))
		if err != nil {
			return nil, err
		}
		return w.Target(r)
	}, func() { p.Close() }
}

// checkM3Deck runs the M3 laws on one deck. budget bounds the number of
// file-writing single-operation plans per deck.
func checkM3Deck(t *testing.T, src string, budget int, c *m3counts) {
	path := private(t, src)
	slides, err := readRaw(path)
	if err != nil {
		t.Logf("unreadable: %v", err)
		return
	}
	c.decks++
	rng := rand.New(rand.NewPCG(uint64(len(src)), 11))
	target, done := handles(t, path)
	var cands []cand
	// Pairing: each codec shows what the view shows.
	for _, s := range slides {
		for _, o := range s.objects {
			c.objects++
			id := o.str("id")
			if o.str("kind") == "other" {
				continue
			}
			h, err := target(s.name, id)
			if err != nil {
				c.notTargetable++
				continue
			}
			cs, vs := codecShows(h), viewShows(o)
			for k, v := range vs {
				if k == "name" && cs[k] == "" && v == "" {
					continue
				}
				if cs[k] != v {
					t.Errorf("%s %s %s: codec shows %q, view shows %q", s.name, id, k, cs[k], v)
					continue
				}
				c.paired[k]++
			}
			for k, v := range cs {
				if _, ok := vs[k]; !ok {
					t.Errorf("%s %s %s: codec shows %q, the view shows nothing", s.name, id, k, v)
				}
			}
			cands = append(cands, cand{s.name, o})
		}
	}
	done()

	// Identity: every writable property written back exactly as the view
	// shows it, one plan per slide, all in one transaction: bytes and mtime
	// unchanged.
	before, _ := os.ReadFile(path)
	info0, _ := os.Stat(path)
	tx, err := ops.Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, cd := range cands {
		spec := map[string]any{"op": "object.set", "at": cd.o.str("id")}
		for _, k := range writableKeys {
			if v, ok := cd.o[k]; ok {
				spec[k] = v
				c.identityFields++
			}
		}
		for _, op := range plan(cd.slide, spec) {
			if err := tx.Run(op); err != nil {
				t.Errorf("%s %s: identity write: %v", cd.slide, cd.o.str("id"), err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("identity plan: %v", err)
	}
	after, _ := os.ReadFile(path)
	info1, _ := os.Stat(path)
	if !bytes.Equal(before, after) || !info0.ModTime().Equal(info1.ModTime()) {
		t.Errorf("identity plan changed the file")
	}

	inverse(t, path, cands, rng, c)

	rng.Shuffle(len(cands), func(i, j int) { cands[i], cands[j] = cands[j], cands[i] })
	if len(cands) > budget {
		cands = cands[:budget]
	}
	for _, cd := range cands {
		singleOps(t, path, cd.slide, cd.o, c)
	}
}

// inverse sets new values through every codec that reads, one object per
// transaction to sort out the inexpressible, then all that succeeded in
// one plan, and checks that the view shows each value as set.
func inverse(t *testing.T, path string, cands []cand, rng *rand.Rand, c *m3counts) {
	schemes := []string{"accent1", "accent2", "accent3", "accent4", "accent5", "accent6"}
	dashes := []string{"dash", "sysDot", "lgDashDot", "solid"}
	type want struct {
		slide, id string
		spec      map[string]any
		expect    map[string]string // view key -> substring the view must show
	}
	var wants []want
	for _, cd := range cands {
		o := cd.o
		spec := map[string]any{"op": "object.set", "at": o.str("id")}
		exp := map[string]string{}
		kind := o.str("kind")
		if kind == "shape" || kind == "group" {
			if _, ok := o["fill"]; ok {
				s := schemes[rng.IntN(len(schemes))]
				spec["fill"] = map[string]any{"scheme": s}
				exp["fill"] = `{"scheme":"` + s + `"`
			}
		}
		if kind == "shape" || kind == "connector" || kind == "picture" {
			w := float64(rng.IntN(800)+25) / 100
			d := dashes[rng.IntN(len(dashes))]
			spec["line"] = map[string]any{"w": w, "color": map[string]any{"hex": "#12AB34"}, "dash": d, "tail": "triangle w=sm len=lg"}
			exp["line"] = fmt.Sprintf(`{"w":%s,"color":{"hex":"#12AB34"},"dash":"%s"`, derive.FormatNum(w), d)
		}
		spec["descr"] = "law alt text " + o.str("id")
		exp["descr"] = `"law alt text ` + o.str("id") + `"`
		if kind == "shape" && compact(o["tx_box"]) != "true" {
			spec["tx_box"] = true
			exp["tx_box"] = "true"
		}
		if kind == "connector" {
			spec["fill"] = "none"
			exp["fill"] = `"none"`
		}
		if g := o.str("geom"); g != "" && g != "custom" && kind == "shape" && !derive.IsConnectorPreset(g) {
			spec["geom"] = "roundRect"
			spec["adj"] = map[string]any{"adj": 12345}
			exp["geom"] = "roundRect"
		}
		if t, ok := o["text"]; ok && kind == "shape" {
			var cur struct {
				Anchor  string `json:"anchor"`
				Wrap    string `json:"wrap"`
				Autofit string `json:"autofit"`
				Vert    string `json:"vert"`
			}
			_ = json.Unmarshal(t, &cur)
			anchor, wrap, autofit, vert := "b", "none", "shape", "vert"
			if cur.Anchor == "b" {
				anchor = "ctr"
			}
			if cur.Wrap == "none" {
				wrap = "square"
			}
			if cur.Autofit == "shape" {
				autofit = "none"
			}
			if cur.Vert == "vert" {
				vert = "horz"
			}
			ins := []float64{float64(rng.IntN(20)), 1.5, 2.25, float64(rng.IntN(10))}
			spec["text"] = map[string]any{"anchor": anchor, "wrap": wrap, "insets": ins, "autofit": autofit, "vert": vert}
			exp["text"] = fmt.Sprintf(`{"anchor":"%s","wrap":"%s","insets":[%s,1.5,2.25,%s],"autofit":"%s","vert":"%s"`, anchor, wrap, derive.FormatNum(ins[0]), derive.FormatNum(ins[3]), autofit, vert)
		}
		if kind == "connector" && o["from"] != nil {
			var to [2]float64
			_ = json.Unmarshal(o["to"], &to)
			to[0], to[1] = to[0]+3, to[1]-2
			spec["to"] = to
			exp["to"] = fmt.Sprintf("[%s,%s]", derive.FormatNum(to[0]), derive.FormatNum(to[1]))
		}
		if len(spec) == 2 {
			continue
		}
		wants = append(wants, want{cd.slide, o.str("id"), spec, exp})
	}
	var ok []want
	for _, w := range wants {
		tx, err := ops.Begin(path)
		if err != nil {
			t.Fatal(err)
		}
		var runErr error
		for _, op := range plan(w.slide, w.spec) {
			if runErr = tx.Run(op); runErr != nil {
				break
			}
		}
		tx.Rollback()
		switch {
		case runErr == nil:
			ok = append(ok, w)
		case errors.Is(runErr, prop.ErrInexpressible):
			c.inverseInexpressible++
		case errors.Is(runErr, edit.ErrBranch):
			// The design: the same change in every mc branch, or none.
			c.inverseBranch++
		default:
			t.Errorf("%s %s: set %v: %v", w.slide, w.id, w.spec, runErr)
		}
	}
	if len(ok) == 0 {
		return
	}
	dst := filepath.Join(t.TempDir(), "inverse.pptx")
	tx, err := ops.Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range ok {
		for _, op := range plan(w.slide, w.spec) {
			if err := tx.Run(op); err != nil {
				t.Fatalf("%s %s: %v", w.slide, w.id, err)
			}
		}
	}
	if err := tx.CommitTo(dst); err != nil {
		t.Fatalf("inverse plan: %v", err)
	}
	slides, err := readRaw(dst)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]rawObj{}
	for _, s := range slides {
		for _, o := range s.objects {
			byID[s.name+"#"+o.str("id")] = o
		}
	}
	for _, w := range ok {
		o := byID[w.slide+"#"+w.id]
		for k, sub := range w.expect {
			got := compact(o[k])
			if k == "geom" {
				got = o.str("geom")
			}
			if !strings.Contains(got, sub) {
				t.Errorf("%s %s: %s shows %s after setting %v", w.slide, w.id, k, got, w.spec[k])
				continue
			}
			c.inverseOK++
			c.inverseByCodec[k]++
		}
	}
}

// singleOps runs single-operation plans on one object and checks that
// nothing else changed: every other part byte for byte, every other
// object's own XML on the slide.
func singleOps(t *testing.T, path, slide string, o rawObj, c *m3counts) {
	id := o.str("id")
	dir := t.TempDir()
	run := func(name string, specs []map[string]any, changed func(before, after map[string]*model.Object) map[string]bool) {
		dst := filepath.Join(dir, name+".pptx")
		os.Remove(dst)
		err := ops.ApplyTo(path, dst, plan(slide, specs...)...)
		if errors.Is(err, prop.ErrInexpressible) || errors.Is(err, edit.ErrBranch) {
			c.opInexpressible++
			return
		}
		if err != nil {
			t.Errorf("%s %s %s: %v", slide, id, name, err)
			return
		}
		c.opPlans[name]++
		checkUntargeted(t, path, dst, slide, changed)
	}
	only := func(ids ...string) func(_, _ map[string]*model.Object) map[string]bool {
		return func(_, _ map[string]*model.Object) map[string]bool {
			m := map[string]bool{}
			for _, x := range ids {
				m[x] = true
			}
			return m
		}
	}
	kind := o.str("kind")
	if kind == "shape" {
		run("set-fill", []map[string]any{{"op": "object.set", "at": id, "fill": map[string]any{"hex": "#ABCDEF"}}}, only(id))
	}
	if _, ok := o["text"]; ok && kind == "shape" {
		run("set-text-body", []map[string]any{{"op": "object.set", "at": id, "text": map[string]any{"insets": []float64{3, 3, 3, 3}, "anchor": "just"}}}, only(id))
	}
	if kind == "shape" || kind == "connector" || kind == "picture" {
		run("set-line", []map[string]any{{"op": "object.set", "at": id, "line": map[string]any{"w": 1.5, "color": map[string]any{"scheme": "accent2"}}}}, only(id))
	}
	textOps(t, path, slide, o, c)
	run("delete", []map[string]any{{"op": "object.delete", "at": id}}, only(id))
	run("duplicate", []map[string]any{{"op": "object.duplicate", "at": id, "as": "c"}}, only(id))
	run("reorder-back", []map[string]any{{"op": "object.reorder", "at": id, "to": "back"}}, only(id))
	run("add", []map[string]any{{"op": "object.add", "kind": "shape", "name": "law", "geom": "rect", "box": []float64{10, 10, 20, 20}, "fill": map[string]any{"scheme": "accent1"}}}, only())
	run("add-connector", []map[string]any{{"op": "object.add", "kind": "connector", "name": "law", "geom": "bentConnector3", "first": "v", "from": []float64{10, 10}, "to": []float64{40, 80}, "line": map[string]any{"w": 1, "color": map[string]any{"hex": "#000000"}}}}, only())

	// Duplicate then delete the original: the slide is the original
	// with the object's ids and creation ids renamed, nothing else.
	dst := filepath.Join(dir, "rename.pptx")
	err := ops.ApplyTo(path, dst, plan(slide, map[string]any{"op": "object.duplicate", "at": id, "as": "c"}, map[string]any{"op": "object.delete", "at": id})...)
	switch {
	case errors.Is(err, edit.ErrBranch):
	case err != nil:
		t.Errorf("%s %s duplicate+delete: %v", slide, id, err)
	default:
		if err := renamedOnly(t, path, dst, slide, id); err != nil {
			t.Errorf("%s %s duplicate+delete: %v", slide, id, err)
		} else {
			c.dupDeleteRename++
		}
	}

	// Add then delete in one plan: no change at all (only the id
	// counter moved, in memory).
	b0, _ := os.ReadFile(path)
	if err := ops.Apply(path, plan(slide,
		map[string]any{"op": "object.add", "as": "tmp", "kind": "shape", "name": "x", "geom": "rect", "box": []float64{1, 1, 2, 2}},
		map[string]any{"op": "object.delete", "at": "$tmp"})...); err != nil {
		t.Errorf("%s add+delete: %v", slide, err)
	}
	if b1, _ := os.ReadFile(path); !bytes.Equal(b0, b1) {
		t.Errorf("%s add+delete changed the file", slide)
	} else {
		c.addDelete++
	}
}

// checkUntargeted compares src and dst: parts other than the slide must be
// byte-identical; on the slide, every object present before that is not
// in allowed keeps its own XML (child objects cut out of groups).
func checkUntargeted(t *testing.T, src, dst, slide string, allowed func(_, _ map[string]*model.Object) map[string]bool) {
	t.Helper()
	ps, pd := parts(t, src), parts(t, dst)
	slidePart := "/ppt/slides/" + slide + ".xml"
	// Media a removed picture used is released (design: Pictures): the
	// slide's relationships may lose entries, and a media part no
	// relationship targets any more may go, nothing else.
	released := releasedMedia(t, src, dst, slidePart)
	for n, b := range ps {
		if strings.EqualFold(n, slidePart) {
			continue
		}
		if strings.EqualFold(n, opc.RelsName(slidePart)) && released != nil {
			continue
		}
		if _, present := pd[n]; !present && released[strings.ToLower(n)] {
			continue
		}
		if !bytes.Equal(b, pd[n]) {
			t.Errorf("part %s changed", n)
		}
	}
	if len(ps) != len(pd)+len(released) {
		t.Errorf("part count %d -> %d", len(ps), len(pd))
	}
	sb, _ := readRaw(src)
	sa, _ := readRaw(dst)
	for i := range sb {
		if sb[i].name != slide {
			continue
		}
		ok := allowed(sb[i].model, sa[i].model)
		// Objects sharing a target's id are addressed by order (s40~2):
		// removing or adding one renames the others.
		for id := range ok {
			if o := sb[i].model[id]; o != nil {
				for _, x := range sb[i].model {
					if x.RawID == o.RawID {
						ok[x.ID] = true
					}
				}
			}
		}
		for id, o := range sb[i].model {
			if ok[id] {
				continue
			}
			a := sa[i].model[id]
			if a == nil {
				if !ancestorIn(sb[i].model, o, ok) {
					t.Errorf("%s: untargeted %s removed", slide, id)
				}
				continue
			}
			if !bytes.Equal(own(sb[i].model, o), own(sa[i].model, a)) && !ancestorIn(sb[i].model, o, ok) {
				t.Errorf("%s: untargeted %s changed", slide, id)
			}
		}
	}
}

func ancestorIn(m map[string]*model.Object, o *model.Object, ids map[string]bool) bool {
	for p := m[o.Parent]; p != nil; p = m[p.Parent] {
		if ids[p.ID] {
			return true
		}
	}
	return false
}

// own returns an object's element bytes with its child objects cut out:
// a group's own content, which a change to its list of children (a child
// added, removed or reordered) does not change.
func own(m map[string]*model.Object, o *model.Object) []byte {
	b := o.ElementXML()
	for _, c := range m {
		if c.Parent == o.ID {
			b = bytes.Replace(b, c.ElementXML(), nil, 1)
		}
	}
	return b
}

var (
	reCNvPr = regexp.MustCompile(`cNvPr id="(\d+)"`)
	reGUID  = regexp.MustCompile(`creationId [^>]*id="(\{[0-9A-F-]+\})"`)
)

// renamedOnly checks that dst's slide equals src's with the object's
// source bytes replaced by its copy, and that the copy differs from the
// original only in its object ids (and glue to them) and creation ids.
func renamedOnly(t *testing.T, src, dst, slide, id string) error {
	sb, _ := readRaw(src)
	part := ""
	var orig []byte
	for _, s := range sb {
		if s.name == slide {
			part = s.part
			orig = s.model[id].XML()
		}
	}
	a, b := parts(t, src)[part], parts(t, dst)[part]
	i := bytes.Index(a, orig)
	if i < 0 {
		return errors.New("original not found in the slide")
	}
	tail := a[i+len(orig):]
	if !bytes.HasPrefix(b, a[:i]) || !bytes.HasSuffix(b, tail) || len(b) < i+len(tail) {
		return errors.New("bytes outside the object changed")
	}
	cp := string(b[i : len(b)-len(tail)])
	o := string(orig)
	ids, ido := reCNvPr.FindAllStringSubmatch(cp, -1), reCNvPr.FindAllStringSubmatch(o, -1)
	gs, gso := reGUID.FindAllStringSubmatch(cp, -1), reGUID.FindAllStringSubmatch(o, -1)
	if len(ids) != len(ido) || len(gs) != len(gso) {
		return errors.New("copy has different ids")
	}
	back := map[string]string{}
	for k := range ids {
		back[ids[k][1]] = ido[k][1]
	}
	for k := range gs {
		cp = strings.ReplaceAll(cp, gs[k][1], gso[k][1])
	}
	cp = regexp.MustCompile(`((?:cNvPr|stCxn|endCxn) id=")(\d+)"`).ReplaceAllStringFunc(cp, func(m string) string {
		sm := regexp.MustCompile(`^(.*id=")(\d+)"$`).FindStringSubmatch(m)
		if old, ok := back[sm[2]]; ok {
			return sm[1] + old + `"`
		}
		return m
	})
	if cp != o {
		return fmt.Errorf("copy differs from the original beyond ids:\n%s\n%s", cp, o)
	}
	return nil
}

func newM3Counts() *m3counts {
	return &m3counts{paired: map[string]int{}, inverseByCodec: map[string]int{}, opPlans: map[string]int{}, textRefused: map[string]int{}}
}

func TestM3LawsFixtures(t *testing.T) {
	c := newM3Counts()
	all := testdeck.All(t)
	for _, n := range []string{"minimal", "quirky", "soffice", "diagram"} {
		t.Run(n, func(t *testing.T) { checkM3Deck(t, all[n], 1000, c) })
	}
	t.Log(c)
	if c.inverseOK == 0 || c.dupDeleteRename == 0 || c.opPlans["add"] == 0 ||
		c.opPlans["text-set"] == 0 || c.opPlans["text-replace"] == 0 || c.opPlans["text-set-new-body"] == 0 || c.textAtomic == 0 {
		t.Fatalf("laws exercised nothing: %v", c)
	}
}

func TestM3LawsCorpus(t *testing.T) {
	t.Parallel() // beside the other corpus suites (corpus_test.go)
	files, _ := nativeFiles(t)
	c := newM3Counts()
	for _, r := range corpustest.Run(t, files, func(t *testing.T, f corpustest.File, d **m3counts) {
		*d = newM3Counts()
		checkM3Deck(t, f.Path, 2, *d)
	}) {
		c.add(r.Value)
	}
	t.Log(c)
}
