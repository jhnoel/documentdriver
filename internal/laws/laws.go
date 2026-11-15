// Package laws is the format-neutral harness for the write laws every
// property and operation lands with. A format describes one law suite as
// a Suite: how to read the targets of a document and their values, how
// to show a value, the operation that sets one, how to vary one, how to
// apply operations as a plan, and its units; Check then runs the five
// laws on a document:
//
//   - identity: every target written back as read, in one plan, leaves
//     the file's bytes and mtime unchanged;
//   - inverse: a varied value set on one target reads back as set, or the
//     plan is refused (Suite.Refused);
//   - untargeted: that plan leaves every other part byte for byte, and in
//     the target's part the own bytes of every unit the target does not
//     own (nested units masked);
//   - atomicity: a failure at operation k of a plan, and at each save
//     stage before the rename (first and second occurrence), leaves the
//     file identical with no temporary file; a failure after the rename
//     is not reported;
//   - sequencing: an operation and its exact inverse in one plan write
//     nothing (or show the same, for sticky targets), and [a, b] equals
//     [a] then [b], part for part.
//
// It is imported only by tests (enforced by internal/archtest); it is
// part of the shared engine and depends on no format.
package laws

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// Target is one thing a law writes: the part that holds it and the
// format's address for it (slide1#17).
type Target struct {
	Part string
	Addr string
}

// Value is a target with its value as read.
type Value[V any] struct {
	Target Target
	V      V
}

// Op is one of the format's operations, opaque to the harness: Suite.Set,
// Undo and Fail make them and Suite.Apply runs them.
type Op any

// Suite is one law suite: a property (or operation) of a format, as table
// entry. Read, Show, Set, Vary, Apply, Refused, Owns, Units and Fail are
// required; Undo and Sticky are optional.
type Suite[V any] struct {
	// Name names the suite in failures.
	Name string
	// Read returns every target of the document at path the suite may
	// write, in document order, with its value as the format's views show
	// it.
	Read func(path string) ([]Value[V], error)
	// Show formats a value; two values that show the same are equal.
	Show func(V) string
	// Set returns the operation that sets target t to v.
	Set func(t Target, v V) Op
	// Vary returns a value near cur, different from it, for the inverse
	// law.
	Vary func(rng *rand.Rand, t Target, cur V) V
	// Apply applies ops as one plan to the document at src, writing dst
	// (in place when dst is src).
	Apply func(src, dst string, ops []Op) error
	// Refused reports whether err is a refusal the format documents (a
	// value it cannot store so that it reads back), which the inverse and
	// sequencing laws count instead of failing.
	Refused func(err error) bool
	// Units divides parts into the units whose own bytes the untargeted
	// law compares (plan.Units, the verifier's).
	Units plan.Units
	// Owns reports whether unit e of the target's part belongs to target
	// t, so that its own bytes may change.
	Owns func(t Target, e *tree.Node) bool
	// Fail returns an operation that fails when applied.
	Fail func() Op
	// Undo, when set, returns an operation on t and its exact inverse (a
	// move by d and by -d).
	Undo func(rng *rand.Rand, t Target) (Op, Op)
	// Sticky, when set, reports targets whose Undo pair restores what is
	// shown but not the bytes (a placeholder that gains a transform of
	// its own on its first move).
	Sticky func(cur Value[V]) bool
	// Budget bounds the file-writing plans per document of the inverse,
	// untargeted and sequencing laws (0: no bound).
	Budget int
}

// Report counts what Check exercised.
type Report struct {
	Docs, Unreadable, Targets          int
	IdentityPlans                      int
	InverseOK, InverseRefused          int
	AtomicPlans, AtomicStages          int
	UndoPlans, UndoSticky, UndoRefused int
	SequencePairs                      int
}

// Add sums o into r, for suites that check documents in parallel, each
// into a report of its own.
func (r *Report) Add(o Report) {
	r.Docs += o.Docs
	r.Unreadable += o.Unreadable
	r.Targets += o.Targets
	r.IdentityPlans += o.IdentityPlans
	r.InverseOK += o.InverseOK
	r.InverseRefused += o.InverseRefused
	r.AtomicPlans += o.AtomicPlans
	r.AtomicStages += o.AtomicStages
	r.UndoPlans += o.UndoPlans
	r.UndoSticky += o.UndoSticky
	r.UndoRefused += o.UndoRefused
	r.SequencePairs += o.SequencePairs
}

// String summarizes the coverage counted by a Report.
func (r Report) String() string {
	return fmt.Sprintf("docs %d (unreadable %d), targets %d; identity plans %d; inverse %d read back, %d refused (untargeted checked on each read-back); atomicity %d failing plans, %d save-stage faults; undo pairs %d (%d sticky, %d refused); sequence pairs %d",
		r.Docs, r.Unreadable, r.Targets, r.IdentityPlans, r.InverseOK, r.InverseRefused, r.AtomicPlans, r.AtomicStages, r.UndoPlans, r.UndoSticky, r.UndoRefused, r.SequencePairs)
}

// Check runs the five laws of s on a private copy of the document at src
// and adds what it exercised to rep. seed seeds the random values.
func (s *Suite[V]) Check(t *testing.T, src string, seed uint64, rep *Report) {
	t.Helper()
	path := Private(t, src)
	vals, err := s.Read(path)
	if err != nil {
		rep.Unreadable++
		t.Logf("%s: unreadable: %v", s.Name, err)
		return
	}
	rep.Docs++
	rep.Targets += len(vals)
	rng := rand.New(rand.NewPCG(seed, 7)) //nolint:gosec // G404: reproducible law values, not secrets
	s.identity(t, path, vals, rep)
	accepted := s.inverse(t, path, vals, rng, rep)
	s.atomicity(t, src, accepted, rep)
	s.sequencing(t, path, vals, accepted, rng, rep)
}

// identity writes every target back as read, in one plan.
func (s *Suite[V]) identity(t *testing.T, path string, vals []Value[V], rep *Report) {
	if len(vals) == 0 {
		return
	}
	var ops []Op
	for _, v := range vals {
		ops = append(ops, s.Set(v.Target, v.V))
	}
	before := snapshot(t, path)
	if err := s.Apply(path, path, ops); err != nil {
		t.Errorf("%s: identity plan: %v", s.Name, err)
		return
	}
	rep.IdentityPlans++
	if err := before.same(path); err != nil {
		t.Errorf("%s: identity plan: %v", s.Name, err)
	}
}

// change is a value set on a target that read back as set.
type change[V any] struct {
	t Target
	v V
}

// inverse sets a varied value on targets one plan each (up to the
// budget), checks that it reads back and that nothing untargeted changed,
// and returns the changes that were accepted.
func (s *Suite[V]) inverse(t *testing.T, path string, vals []Value[V], rng *rand.Rand, rep *Report) []change[V] {
	order := rng.Perm(len(vals))
	var accepted []change[V]
	for n, k := range order {
		if s.Budget > 0 && n >= s.Budget {
			break
		}
		cur := vals[k]
		want := s.Vary(rng, cur.Target, cur.V)
		dst := filepath.Join(t.TempDir(), "out"+filepath.Ext(path))
		err := s.Apply(path, dst, []Op{s.Set(cur.Target, want)})
		switch {
		case err != nil && s.Refused(err):
			rep.InverseRefused++
			t.Logf("%s: %s: refused: %v", s.Name, cur.Target.Addr, err)
			continue
		case err != nil:
			t.Errorf("%s: %s: set %s: %v", s.Name, cur.Target.Addr, s.Show(want), err)
			continue
		}
		after, err := s.Read(dst)
		if err != nil {
			t.Errorf("%s: %s: reading the output: %v", s.Name, cur.Target.Addr, err)
			continue
		}
		got, ok := find(after, cur.Target)
		switch {
		case !ok:
			t.Errorf("%s: %s: target gone after a set", s.Name, cur.Target.Addr)
		case s.Show(got) != s.Show(want):
			t.Errorf("%s: %s: set %s, reads back %s", s.Name, cur.Target.Addr, s.Show(want), s.Show(got))
		default:
			rep.InverseOK++
			// Only a change that wrote something can make the atomicity
			// and sequencing plans write (a varied value can store as the
			// bytes already there, after rounding).
			if SameParts(path, dst, "") != nil {
				accepted = append(accepted, change[V]{cur.Target, want})
			}
		}
		if err := Untargeted(path, dst, cur.Target.Part, s.Units, func(e *tree.Node) bool { return s.Owns(cur.Target, e) }); err != nil {
			t.Errorf("%s: %s: %v", s.Name, cur.Target.Addr, err)
		}
	}
	return accepted
}

// find returns the value for target t, or false when absent.
func find[V any](vals []Value[V], t Target) (V, bool) {
	for _, v := range vals {
		if v.Target == t {
			return v.V, true
		}
	}
	var zero V
	return zero, false
}

// atomicPlan is the plan the atomicity law fails: up to five accepted
// changes, when they apply together (changes accepted one by one can
// conflict: a group's new box under which a child's new box is no longer
// expressible), else the first alone.
func (s *Suite[V]) atomicPlan(t *testing.T, src string, accepted []change[V]) []Op {
	var ops []Op
	for _, c := range accepted[:min(5, len(accepted))] {
		ops = append(ops, s.Set(c.t, c.v))
	}
	if len(ops) > 1 {
		dst := filepath.Join(t.TempDir(), "together"+filepath.Ext(src))
		if err := s.Apply(src, dst, ops); err != nil {
			ops = ops[:1]
		}
	}
	return ops
}

// stages are the save stages before the rename, where a fault must leave
// the file untouched.
var stages = []string{opc.StageCreate, opc.StageWrite, opc.StageChmod, opc.StageSync, opc.StageClose, opc.StageRename}

// atomicity fails a writing plan at each operation and at each save
// stage, each on its own private copy.
func (s *Suite[V]) atomicity(t *testing.T, src string, accepted []change[V], rep *Report) {
	if len(accepted) == 0 {
		return
	}
	plan := s.atomicPlan(t, src, accepted)
	for k := 0; k <= len(plan); k++ {
		path := Private(t, src)
		failing := append(append(append([]Op{}, plan[:k]...), s.Fail()), plan[k:]...)
		before := snapshot(t, path)
		if err := s.Apply(path, path, failing); err == nil {
			t.Errorf("%s: a plan failing at operation %d succeeded", s.Name, k)
		}
		if err := before.same(path); err != nil {
			t.Errorf("%s: a plan failing at operation %d: %v", s.Name, k, err)
		}
		rep.AtomicPlans++
	}
	for _, stage := range stages {
		for _, nth := range []int{1, 2} {
			path := Private(t, src)
			before := snapshot(t, path)
			seen := 0
			restore := opc.SetFaultHookFor(path, func(st, _ string) error {
				if st == stage {
					seen++
					if seen == nth {
						return errors.New("injected at " + st)
					}
				}
				return nil
			})
			err := s.Apply(path, path, plan)
			restore()
			if seen < nth {
				continue
			}
			rep.AtomicStages++
			if err == nil {
				t.Errorf("%s: commit succeeded with a fault at %s #%d", s.Name, stage, nth)
			}
			if err := before.same(path); err != nil {
				t.Errorf("%s: fault at %s #%d: %v", s.Name, stage, nth, err)
			}
		}
	}
	path := Private(t, src)
	before := snapshot(t, path)
	restore := opc.SetFaultHookFor(path, func(st, _ string) error {
		if st == opc.StageDirSync {
			return errors.New("injected after the rename")
		}
		return nil
	})
	err := s.Apply(path, path, plan)
	restore()
	if err != nil {
		t.Errorf("%s: commit reported %v after the rename", s.Name, err)
	}
	if before.same(path) == nil {
		t.Errorf("%s: the plan wrote nothing", s.Name)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 1 {
		t.Errorf("%s: after a fault past the rename the directory holds %d entries (%v)", s.Name, len(entries), err)
	}
}

// sequencing checks undo pairs and [a, b] against [a] then [b].
func (s *Suite[V]) sequencing(t *testing.T, path string, vals []Value[V], accepted []change[V], rng *rand.Rand, rep *Report) {
	if s.Undo != nil {
		for k, v := range vals {
			if s.Budget > 0 && k >= s.Budget {
				break
			}
			op, inv := s.Undo(rng, v.Target)
			before := snapshot(t, path)
			err := s.Apply(path, path, []Op{op, inv})
			switch {
			case err != nil && s.Refused(err):
				rep.UndoRefused++
				continue
			case err != nil:
				t.Errorf("%s: %s: undo pair: %v", s.Name, v.Target.Addr, err)
				continue
			}
			rep.UndoPlans++
			if s.Sticky == nil || !s.Sticky(v) {
				if err := before.same(path); err != nil {
					t.Errorf("%s: %s: undo pair: %v", s.Name, v.Target.Addr, err)
				}
				continue
			}
			rep.UndoSticky++
			after, err := s.Read(path)
			if err != nil {
				t.Fatalf("%s: reading after an undo pair: %v", s.Name, err)
			}
			if got, ok := find(after, v.Target); !ok || s.Show(got) != s.Show(v.V) {
				t.Errorf("%s: %s: undo pair shows %s, was %s", s.Name, v.Target.Addr, s.Show(got), s.Show(v.V))
			}
			before.restore(t, path)
		}
	}
	for k := 0; k+1 < len(accepted); k += 2 {
		if s.Budget > 0 && k >= s.Budget {
			break
		}
		a := s.Set(accepted[k].t, accepted[k].v)
		b := s.Set(accepted[k+1].t, accepted[k+1].v)
		dir := t.TempDir()
		one, two := filepath.Join(dir, "one"+filepath.Ext(path)), filepath.Join(dir, "two"+filepath.Ext(path))
		errAB := s.Apply(path, one, []Op{a, b})
		errA := s.Apply(path, two, []Op{a})
		if errAB != nil || errA != nil {
			if s.Refused(errAB) || s.Refused(errA) {
				continue
			}
			t.Errorf("%s: sequencing: %v / %v", s.Name, errAB, errA)
			continue
		}
		if err := s.Apply(two, two, []Op{b}); err != nil {
			t.Errorf("%s: sequencing [b]: %v", s.Name, err)
			continue
		}
		if err := SameParts(one, two, ""); err != nil {
			t.Errorf("%s: sequencing: [a, b] and [a] then [b]: %v", s.Name, err)
		}
		rep.SequencePairs++
	}
}

// Private copies the document at src into a new temporary directory of
// its own, with an mtime two days old (so that a rewrite shows), and
// returns the copy's path.
func Private(t testing.TB, src string) string {
	t.Helper()
	b, err := os.ReadFile(src) //nolint:gosec // G304: a test document chosen by the caller
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "doc"+filepath.Ext(src))
	if err := os.WriteFile(dst, b, 0o600); err != nil { //nolint:gosec // G703: dst is in the test's own temporary directory
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(dst, old, old); err != nil {
		t.Fatal(err)
	}
	return dst
}

// state is a file's bytes and mtime.
type state struct {
	b   []byte
	mod time.Time
}

// snapshot captures the file's bytes and modification time for later comparison.
func snapshot(t testing.TB, path string) state {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return state{b, info.ModTime()}
}

// same fails unless the file at path still has s's bytes and mtime and
// its directory holds nothing else (no temporary file).
func (s state) same(path string) error {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the test's own copy
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	switch {
	case !bytes.Equal(b, s.b):
		return errors.New("the file changed")
	case !info.ModTime().Equal(s.mod):
		return errors.New("the file's mtime changed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return fmt.Errorf("the directory holds %d entries", len(entries))
	}
	return nil
}

// restore writes s back to path.
func (s state) restore(t testing.TB, path string) {
	t.Helper()
	if err := os.WriteFile(path, s.b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, s.mod, s.mod); err != nil {
		t.Fatal(err)
	}
}

// Parts returns every part of the package at path, uncompressed, by
// name.
func Parts(path string) (map[string][]byte, error) {
	p, err := opc.Open(path)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	out := map[string][]byte{}
	for _, n := range p.Names() {
		pt, err := p.Part(n)
		if err != nil {
			return nil, err
		}
		b, err := pt.Bytes()
		if err != nil {
			return nil, err
		}
		out[n] = b
	}
	return out, nil
}

// SameParts fails unless the packages at a and b have the same part
// names and every part but except ("" for none; compared without case)
// byte for byte.
func SameParts(a, b, except string) error {
	pa, err := Parts(a)
	if err != nil {
		return err
	}
	pb, err := Parts(b)
	if err != nil {
		return err
	}
	names := slices.Sorted(maps.Keys(pa))
	var errs []error
	for _, n := range names {
		y, ok := pb[n]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("part %s is gone", n))
		case except != "" && strings.EqualFold(strings.TrimPrefix(n, "/"), strings.TrimPrefix(except, "/")):
		case !bytes.Equal(pa[n], y):
			errs = append(errs, fmt.Errorf("part %s changed", n))
		}
	}
	if len(pa) != len(pb) {
		errs = append(errs, fmt.Errorf("part count %d -> %d", len(pa), len(pb)))
	}
	return errors.Join(errs...)
}

// Untargeted fails unless the package at dst differs from src only in
// part, and in part only in the own bytes of units for which owns
// reports true (in src): every other unit, and the document outside its
// units, keeps its own bytes (nested units masked), in the same order. A
// part without units may change as a whole.
func Untargeted(src, dst, part string, units plan.Units, owns func(e *tree.Node) bool) error {
	if err := SameParts(src, dst, part); err != nil {
		return err
	}
	pa, err := Parts(src)
	if err != nil {
		return err
	}
	pb, err := Parts(dst)
	if err != nil {
		return err
	}
	var a, b []byte
	for n := range pa {
		if strings.EqualFold(strings.TrimPrefix(n, "/"), strings.TrimPrefix(part, "/")) {
			a, b = pa[n], pb[n]
		}
	}
	if a == nil {
		return fmt.Errorf("no part %s", part)
	}
	da, err := tree.Parse(a)
	if err != nil {
		return err
	}
	db, err := tree.Parse(b)
	if err != nil {
		return fmt.Errorf("%s does not parse after the plan: %w", part, err)
	}
	if !units.HasUnits(da) {
		return nil // a part without units changes as a whole
	}
	ua, ub := unitList(da, units), unitList(db, units)
	if len(ua) != len(ub) {
		return fmt.Errorf("%s: %d units became %d", part, len(ua), len(ub))
	}
	var errs []error
	for k := range ua {
		if k > 0 && owns(ua[k]) {
			continue
		}
		if !bytes.Equal(ownBytes(ua[k], units), ownBytes(ub[k], units)) {
			what := "the document outside its units"
			if k > 0 {
				what = units.Describe(ua[k])
			}
			errs = append(errs, fmt.Errorf("%s: untargeted %s changed", part, what))
		}
	}
	return errors.Join(errs...)
}

// unitList returns the document node followed by its units in document
// order.
func unitList(doc *tree.Document, units plan.Units) []*tree.Node {
	out := []*tree.Node{doc.Node()}
	doc.Root().Walk(func(n *tree.Node) bool {
		if n.Kind() == tree.ElementNode && units.IsUnit(n) {
			out = append(out, n)
		}
		return true
	})
	return out
}

// ownBytes serialises n with each nested unit replaced by a marker.
func ownBytes(n *tree.Node, units plan.Units) []byte {
	var b bytes.Buffer
	var walk func(n *tree.Node, top bool)
	walk = func(n *tree.Node, top bool) {
		if n.Kind() != tree.ElementNode && n.Kind() != tree.DocumentNode {
			b.Write(n.Raw())
			return
		}
		if !top && n.Kind() == tree.ElementNode && units.IsUnit(n) {
			b.WriteString("\x00unit\x00")
			return
		}
		if n.Kind() == tree.ElementNode {
			b.Write(n.StartTag())
		}
		for c := range n.ChildrenSeq() {
			walk(c, false)
		}
		if n.Kind() == tree.ElementNode {
			b.Write(n.EndTag())
		}
	}
	walk(n, true)
	return b.Bytes()
}
