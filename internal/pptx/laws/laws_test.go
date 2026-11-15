// Package laws holds the write laws as tests over the hand fixtures and,
// with PPTX_CORPUS set, over every PowerPoint-native corpus deck, end to
// end through plans, the read model and the views. The five laws
// (identity, inverse, untargeted bytes and spans, atomicity, sequencing)
// run through the format-neutral harness internal/laws, each property a
// table entry (transform, name, hidden: laws_test.go); the M3, M4,
// picture and text suites beside them check operations the harness does
// not model yet. The verifier's rejection of a misbehaving operation
// lives in internal/pptx/ops, where test operations can be defined.
package laws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	harness "github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/pptx/ops"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/prop"
	"github.com/jhnoel/documentdriver/internal/pptx/view"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// shown is one object as the JSON view shows it (full detail).
type shown struct {
	ID    string        `json:"id"`
	Kind  string        `json:"kind"`
	Box   []json.Number `json:"box"`
	Rot   json.Number   `json:"rot"`
	FlipH bool          `json:"flip_h"`
	FlipV bool          `json:"flip_v"`
}

// show is the transform codec's form of a shown object.
func (s shown) show() string {
	rot := string(s.Rot)
	if rot == "" {
		rot = "0"
	}
	return fmt.Sprintf("box=[%s %s %s %s] rot=%s flipH=%v flipV=%v", s.Box[0], s.Box[1], s.Box[2], s.Box[3], rot, s.FlipH, s.FlipV)
}

func (s shown) spec() map[string]any {
	rot := json.Number("0")
	if s.Rot != "" {
		rot = s.Rot
	}
	return map[string]any{"op": "object.set", "at": s.ID, "box": s.Box, "rot": rot, "flip_h": s.FlipH, "flip_v": s.FlipV}
}

type slideView struct {
	name    string
	part    string
	objects []shown
	model   map[string]*model.Object
}

// readComplete reads a slide for the laws, which hold over a complete
// derivation context: a slide whose layout, master or theme cannot be
// read is read with a note (docs/contract.md, section 10), and the laws
// count its deck as unreadable, since what a write shows back depends on
// the context that is missing. The rule for such decks has its own tests
// (internal/pptx/apply/defects_test.go).
func readComplete(d *model.Deck, part string) (*model.Slide, error) {
	s, err := d.ReadSlide(part)
	if err != nil {
		return nil, err
	}
	for _, o := range s.Omitted {
		if strings.HasPrefix(o.Reason, "unreadable: ") {
			return nil, fmt.Errorf("%s: %s %s", part, o.Prop, o.Reason)
		}
	}
	return s, nil
}

// readViews reads every slide of a deck through the read model and the
// JSON view.
func readViews(path string) ([]slideView, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		return nil, err
	}
	var out []slideView
	for _, part := range d.Slides {
		s, err := readComplete(d, part)
		if err != nil {
			return nil, err
		}
		b, err := view.JSON(s, view.Options{Detail: view.Full})
		if err != nil {
			return nil, err
		}
		b = testdeck.LocalIDs(b, s.Name)
		var v struct {
			Objects []shown `json:"objects"`
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		sv := slideView{name: s.Name, part: part, objects: v.Objects, model: map[string]*model.Object{}}
		for _, o := range s.Objects {
			sv.model[o.ID] = o
		}
		out = append(out, sv)
	}
	return out, nil
}

// writable reports, per slide and object id, whether the object can be
// targeted (every mc branch has a counterpart), and the codec's Show of
// its transform.
func writable(t *testing.T, path string, slides []slideView) map[string]map[string]string {
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	w := edit.New(p)
	c := prop.Transform(prop.Pt)
	out := map[string]map[string]string{}
	for _, s := range slides {
		out[s.name] = map[string]string{}
		for _, o := range s.objects {
			r, err := w.Resolve(s.name + "#" + o.ID)
			if err != nil {
				t.Errorf("%s %s: resolve: %v", s.name, o.ID, err)
				continue
			}
			h, err := w.Target(r)
			if err != nil {
				continue
			}
			v := c.Read(h, 0)
			if x, ok := v.Get(); ok {
				out[s.name][o.ID] = c.Show(x)
			} else {
				out[s.name][o.ID] = "absent: " + v.Reason()
			}
		}
	}
	return out
}

// private copies src into a temporary directory of its own with an old
// mtime (the harness's laws.Private).
func private(t *testing.T, src string) string { return harness.Private(t, src) }

func plan(slide string, specs ...map[string]any) []ops.Op {
	p, err := ops.ParsePlan(testdeck.Plan(slide, specs...))
	if err != nil {
		panic(err)
	}
	o, err := p.Compile()
	if err != nil {
		panic(err)
	}
	return o
}

// parts returns every part's uncompressed bytes (laws.Parts).
func parts(t *testing.T, path string) map[string][]byte {
	t.Helper()
	out, err := harness.Parts(path)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The pptx side of the law harness (internal/laws): operations are
// compiled plans ([]ops.Op), applied as one transaction.

func applyPlan(src, dst string, list []harness.Op) error {
	var flat []ops.Op
	for _, o := range list {
		flat = append(flat, o.([]ops.Op)...)
	}
	if dst == src {
		return ops.Apply(src, flat...)
	}
	return ops.ApplyTo(src, dst, flat...)
}

func inexpressible(err error) bool { return errors.Is(err, prop.ErrInexpressible) }

// failing is an operation that fails when applied: a move of an object
// no deck has.
func failing() harness.Op {
	return plan("slide1", map[string]any{"op": "object.move", "at": "s4294967295", "dx": 1})
}

// owns reports whether unit e is the target's object (any branch). For
// an id several objects share (s40~2), it owns all of them: the units
// of a part are not numbered as the views number objects.
func owns(t harness.Target, e *tree.Node) bool {
	_, id, _ := strings.Cut(t.Addr, "#")
	id, _, _ = strings.Cut(id, "~")
	for _, c := range e.Elements() {
		if strings.HasPrefix(c.Local(), "nv") {
			for _, cc := range c.Elements() {
				if cc.Local() == "cNvPr" {
					v, _ := cc.Attr("id")
					return "s"+v == id
				}
			}
			return false
		}
	}
	return false
}

func target(slide, part, id string) harness.Target {
	return harness.Target{Part: part, Addr: slide + "#" + id}
}

// slideOf returns the slide name of a target address.
func slideOf(t harness.Target) string { s, _, _ := strings.Cut(t.Addr, "#"); return s }

// idOf returns the object id (s17) of a target address.
func idOf(t harness.Target) string { _, id, _ := strings.Cut(t.Addr, "#"); return id }

// targetable returns, by slide and object id, the objects a write can
// target (every mc branch has a counterpart).
func targetable(path string, slides []slideView) (map[string]bool, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	w := edit.New(p)
	out := map[string]bool{}
	for _, s := range slides {
		for _, o := range s.objects {
			r, err := w.Resolve(s.name + "#" + o.ID)
			if err != nil {
				return nil, fmt.Errorf("%s %s: resolve: %w", s.name, o.ID, err)
			}
			if _, err := w.Target(r); err == nil {
				out[s.name+"#"+o.ID] = true
			}
		}
	}
	return out, nil
}

// placed is an object's transform as shown, and whether its box comes
// from a layout or master placeholder.
type placed struct {
	shown
	inherits bool
}

// transformSuite is the transform law suite: box, rotation and flips
// set through object.set, undone through object.move.
func transformSuite(budget int) *harness.Suite[placed] {
	return &harness.Suite[placed]{
		Name: "transform",
		Read: func(path string) ([]harness.Value[placed], error) {
			slides, err := readViews(path)
			if err != nil {
				return nil, err
			}
			ok, err := targetable(path, slides)
			if err != nil {
				return nil, err
			}
			var out []harness.Value[placed]
			for _, s := range slides {
				for _, o := range s.objects {
					// Graphic frames and ink are read-only (design:
					// Objects); objects without a box have no transform.
					if len(o.Box) != 4 || o.Kind == "other" || !ok[s.name+"#"+o.ID] {
						continue
					}
					m := s.model[o.ID]
					out = append(out, harness.Value[placed]{
						Target: target(s.name, s.part, o.ID),
						V:      placed{o, m != nil && m.Origin["box"] != model.FromSlide},
					})
				}
			}
			return out, nil
		},
		Show: func(v placed) string { return v.show() },
		Set:  func(t harness.Target, v placed) harness.Op { return plan(slideOf(t), v.spec()) },
		Vary: func(rng *rand.Rand, _ harness.Target, cur placed) placed {
			want := cur
			want.Box = []json.Number{jitter(rng, cur.Box[0]), jitter(rng, cur.Box[1]), cur.Box[2], cur.Box[3]}
			if rng.IntN(2) == 0 {
				want.Rot = json.Number(derive.FormatNum(float64(rng.IntN(36000)) / 100))
			}
			return want
		},
		Apply:   applyPlan,
		Refused: inexpressible,
		Units:   edit.Units(),
		Owns:    owns,
		Fail:    failing,
		Undo: func(rng *rand.Rand, t harness.Target) (harness.Op, harness.Op) {
			dx, dy := float64(rng.IntN(2000)-1000)/100, float64(rng.IntN(2000)-1000)/100
			mv := func(x, y float64) harness.Op {
				return plan(slideOf(t), map[string]any{"op": "object.move", "at": idOf(t), "dx": x, "dy": y})
			}
			return mv(dx, dy), mv(-dx, -dy)
		},
		// A placeholder that inherits its box gets a transform of its own
		// on the first move (design: "a move inserts a transform"); moving
		// back keeps it, now storing the inherited values, so that [d, -d]
		// equals [d] then [-d]. The box shown is unchanged; only its
		// origin becomes the slide.
		Sticky: func(v harness.Value[placed]) bool { return v.V.inherits },
		Budget: budget,
	}
}

// rawSuite is a suite over one field of the default JSON view, written
// back through object.set in the same form.
func rawSuite[V any](name string, budget int, get func(rawObj) (V, bool), show func(V) string, vary func(*rand.Rand, V) V) *harness.Suite[V] {
	return &harness.Suite[V]{
		Name: name,
		Read: func(path string) ([]harness.Value[V], error) {
			raw, err := readRaw(path)
			if err != nil {
				return nil, err
			}
			slides, err := readViews(path)
			if err != nil {
				return nil, err
			}
			ok, err := targetable(path, slides)
			if err != nil {
				return nil, err
			}
			var out []harness.Value[V]
			for _, s := range raw {
				for _, o := range s.objects {
					id := o.str("id")
					if o.str("kind") == "other" || !ok[s.name+"#"+id] {
						continue
					}
					if v, has := get(o); has {
						out = append(out, harness.Value[V]{Target: target(s.name, s.part, id), V: v})
					}
				}
			}
			return out, nil
		},
		Show: show,
		Set: func(t harness.Target, v V) harness.Op {
			return plan(slideOf(t), map[string]any{"op": "object.set", "at": idOf(t), name: v})
		},
		Vary:    func(rng *rand.Rand, _ harness.Target, cur V) V { return vary(rng, cur) },
		Apply:   applyPlan,
		Refused: inexpressible,
		Units:   edit.Units(),
		Owns:    owns,
		Fail:    failing,
		Budget:  budget,
	}
}

// nameSuite is the object name (cNvPr name).
func nameSuite(budget int) *harness.Suite[string] {
	return rawSuite("name", budget,
		func(o rawObj) (string, bool) { return o.str("name"), true },
		func(v string) string { return strconv.Quote(v) },
		func(rng *rand.Rand, cur string) string { return fmt.Sprintf("%s law %d", cur, rng.IntN(1000)) })
}

// hiddenSuite is the hidden flag (cNvPr hidden).
func hiddenSuite(budget int) *harness.Suite[bool] {
	return rawSuite("hidden", budget,
		func(o rawObj) (bool, bool) { return compact(o["hidden"]) == "true", true },
		strconv.FormatBool,
		func(_ *rand.Rand, cur bool) bool { return !cur })
}

// suites runs every pptx law suite of the harness on one deck.
func suites(t *testing.T, src string, budget int, reps map[string]*harness.Report) {
	seed := uint64(len(src))
	for _, name := range []string{"transform", "name", "hidden"} {
		if reps[name] == nil {
			reps[name] = &harness.Report{}
		}
	}
	t.Run("transform", func(t *testing.T) { transformSuite(budget).Check(t, src, seed, reps["transform"]) })
	t.Run("name", func(t *testing.T) { nameSuite(budget).Check(t, src, seed, reps["name"]) })
	t.Run("hidden", func(t *testing.T) { hiddenSuite(budget).Check(t, src, seed, reps["hidden"]) })
}

// transformExtras are the pptx-only transform laws beside the harness:
// the codec shows exactly what the view shows, object by object; and the
// inverse in memory on every object of the deck, children before their
// groups, after which the verifier accepts the result.
func transformExtras(t *testing.T, src string, c *counts) {
	path := private(t, src)
	slides, err := readViews(path)
	if err != nil {
		return
	}
	shows := writable(t, path, slides)
	rng := rand.New(rand.NewPCG(uint64(len(src)), 7))
	type cand struct {
		slide string
		o     shown
	}
	var cands []cand
	for _, s := range slides {
		c.slides++
		for _, o := range s.objects {
			c.objects++
			if len(o.Box) != 4 {
				c.noBox++
				continue
			}
			if o.Kind == "other" {
				c.readOnly++
				continue
			}
			got, ok := shows[s.name][o.ID]
			if !ok {
				c.notWritable++
				continue
			}
			if got != o.show() {
				t.Errorf("%s %s: codec shows %q, view shows %q", s.name, o.ID, got, o.show())
				continue
			}
			cands = append(cands, cand{s.name, o})
		}
	}
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	w := edit.New(p)
	codec := prop.Transform(prop.Pt)
	for _, cd := range slices.Backward(cands) {
		r, _ := w.Resolve(cd.slide + "#" + cd.o.ID)
		h, err := w.Target(r)
		if err != nil {
			t.Fatal(err)
		}
		want := randomXfrm(rng, h, codec)
		err = prop.Set(h, codec, want)
		switch {
		case err == nil:
			got, _ := codec.Read(h, 0).Get()
			if codec.Show(got) != codec.Show(want) {
				t.Errorf("%s %s: set %s, reads %s", cd.slide, cd.o.ID, codec.Show(want), codec.Show(got))
			}
			c.setOK++
		case errors.Is(err, prop.ErrInexpressible):
			c.setInexpressible++
			t.Logf("%s %s: %v", cd.slide, cd.o.ID, err)
		default:
			t.Errorf("%s %s: set: %v", cd.slide, cd.o.ID, err)
		}
	}
	if err := w.Verify(); err != nil {
		t.Errorf("verify after inverse sets: %v", err)
	}
}

type counts struct {
	slides, objects, noBox, notWritable, readOnly int
	setOK, setInexpressible                       int
}

func (c counts) String() string {
	return fmt.Sprintf("slides %d, objects %d (no box %d, not writable %d, read-only %d); in-memory inverse %d shown back, %d ErrInexpressible",
		c.slides, c.objects, c.noBox, c.notWritable, c.readOnly, c.setOK, c.setInexpressible)
}

func jitter(rng *rand.Rand, v json.Number) json.Number {
	f, _ := v.Float64()
	return json.Number(derive.FormatNum(f + float64(rng.IntN(10000)-5000)/100))
}

// randomXfrm returns a slide-space transform near the object's current
// one: moved, resized, rotated and flipped at random, on the 0.01 pt grid
// the views show.
func randomXfrm(rng *rand.Rand, h *edit.Handle, c prop.Codec[derive.Xfrm]) derive.Xfrm {
	cur, _ := c.Read(h, 0).Get()
	grid := func(emu float64) float64 { return float64(int64(emu/127)) * 127 }
	x := derive.Xfrm{
		X:     grid(cur.X + float64(rng.IntN(100000)-50000)*127),
		Y:     grid(cur.Y + float64(rng.IntN(100000)-50000)*127),
		W:     grid(cur.W * (0.5 + rng.Float64())),
		H:     grid(cur.H * (0.5 + rng.Float64())),
		Rot:   float64(rng.IntN(36000)) / 100,
		FlipH: rng.IntN(2) == 0,
		FlipV: rng.IntN(2) == 0,
	}
	return x
}

func logReports(t *testing.T, reps map[string]*harness.Report) {
	names := make([]string, 0, len(reps))
	for n := range reps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t.Logf("%s: %v", n, reps[n])
	}
}

func TestLawsFixtures(t *testing.T) {
	var c counts
	reps := map[string]*harness.Report{}
	names := []string{"minimal", "quirky", "soffice", "diagram"}
	all := testdeck.All(t)
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			suites(t, all[n], 1000, reps)
			transformExtras(t, all[n], &c)
		})
	}
	t.Log(c)
	logReports(t, reps)
	tr := reps["transform"]
	if c.setOK == 0 || tr.InverseOK == 0 || tr.UndoPlans == 0 || tr.SequencePairs == 0 || tr.AtomicStages == 0 {
		t.Fatalf("laws exercised nothing: %v; %v", c, tr)
	}
	for _, n := range []string{"name", "hidden"} {
		if reps[n].InverseOK == 0 || reps[n].IdentityPlans == 0 {
			t.Fatalf("%s laws exercised nothing: %v", n, reps[n])
		}
	}
}

func TestLawsCorpus(t *testing.T) {
	t.Parallel() // beside the other corpus suites (corpus_test.go)
	files, missing := nativeFiles(t)
	type deck struct {
		c    counts
		reps map[string]*harness.Report
	}
	var c counts
	reps := map[string]*harness.Report{}
	for _, r := range corpustest.Run(t, files, func(t *testing.T, f corpustest.File, d *deck) {
		d.reps = map[string]*harness.Report{}
		suites(t, f.Path, 3, d.reps)
		transformExtras(t, f.Path, &d.c)
	}) {
		c.add(r.Value.c)
		addReports(reps, r.Value.reps)
	}
	t.Logf("%v; %d manifest decks missing on disk", c, missing)
	logReports(t, reps)
}
