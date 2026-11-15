package coverage

import (
	"fmt"
	"maps"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// A rewrite is a codec write that rewrites an element: every path in its
// scope that it does not itself write must come through unchanged for
// the path to be carried.
type rewrite struct {
	by    string
	scope string
	// except are paths in the scope that the rewrite replaces by
	// design: the other alternatives of the choice it writes.
	except string
	ctx    int
	// op builds the operation from what the views show of the object.
	op func(obj map[string]any) map[string]any
}

func setOp(id string, fields map[string]any) map[string]any {
	op := map[string]any{"op": "object.set", "at": id}
	maps.Copy(op, fields)
	return op
}

// fillChoice is the fill choice of shape and group properties: a fill
// write replaces whichever alternative is there.
const fillChoice = "{p:sp/p:spPr,p:grpSp/p:grpSpPr}/{a:noFill,a:solidFill,a:gradFill,a:blipFill,a:pattFill,a:grpFill}/**"

var rewrites = []rewrite{
	{by: "text.Set", scope: "p:sp/p:txBody/a:p/**", op: func(obj map[string]any) map[string]any {
		// An agent-like edit: text added to the first paragraph's run.
		h, _ := obj["html"].(string)
		return map[string]any{"op": "text.set", "at": "s2", "html": strings.Replace(h, "Level 1", "Level 1 edited", 1)}
	}},
	{by: "prop.Body", scope: "p:sp/p:txBody/a:bodyPr/**", except: "p:sp/p:txBody/a:bodyPr/@anchor", op: func(obj map[string]any) map[string]any {
		// The anchor set to whatever it is not.
		t, _ := obj["text"].(map[string]any)
		a := "b"
		if t != nil && t["anchor"] == "b" {
			a = "t"
		}
		return setOp("s2", map[string]any{"text": map[string]any{"anchor": a}})
	}},
	{by: "prop.Line", scope: "p:sp/p:spPr/a:ln/**", op: func(map[string]any) map[string]any {
		return setOp("s2", map[string]any{"line": map[string]any{"w": 7}})
	}},
	{by: "prop.Fill", scope: "p:sp/p:spPr/**", except: fillChoice, op: func(map[string]any) map[string]any {
		return setOp("s2", map[string]any{"fill": map[string]any{"hex": "#00FF00"}})
	}},
	{by: "prop.Fill (group)", scope: "p:grpSp/p:grpSpPr/**", except: fillChoice, op: func(map[string]any) map[string]any {
		return setOp("s7", map[string]any{"fill": map[string]any{"hex": "#00FF00"}})
	}},
	{by: "prop.Line", scope: "p:cxnSp/p:spPr/a:ln/**", op: func(map[string]any) map[string]any {
		return setOp("s5", map[string]any{"line": map[string]any{"w": 7}})
	}},
	{by: "prop.Line", scope: "p:pic/p:spPr/a:ln/**", op: func(map[string]any) map[string]any {
		return setOp("s9", map[string]any{"line": map[string]any{"w": 7}})
	}},
	{by: "prop.Geometry", scope: "p:sp/p:spPr/a:prstGeom/**", op: func(map[string]any) map[string]any {
		return setOp("s2", map[string]any{"adj": map[string]any{"adj": 20000}})
	}},
	{by: "prop.Path", scope: "p:sp/p:spPr/a:custGeom/**", except: "p:sp/p:spPr/a:custGeom/a:pathLst/a:path/a:arcTo/**", op: func(map[string]any) map[string]any {
		return setOp("s2", map[string]any{"path": "M 0 0 L 20 10 L 0 10 Z"})
	}},
	{by: "prop.Path", scope: "p:pic/p:spPr/a:custGeom/**", except: "p:pic/p:spPr/a:custGeom/a:pathLst/a:path/a:arcTo/**", op: func(map[string]any) map[string]any {
		return setOp("s9", map[string]any{"path": "M 0 0 L 20 10 L 0 10 Z"})
	}},
	{by: "prop.Geometry", scope: "p:pic/p:spPr/a:prstGeom/**", op: func(map[string]any) map[string]any {
		return setOp("s9", map[string]any{"adj": map[string]any{"adj": 20000}})
	}},
	{by: "prop.Transform", scope: "p:sp/p:spPr/a:xfrm/**", op: func(map[string]any) map[string]any {
		return setOp("s2", map[string]any{"box": []float64{80, 80, 144, 72}})
	}},
	{by: "prop.Transform", scope: "p:pic/p:spPr/a:xfrm/**", op: func(map[string]any) map[string]any {
		return setOp("s9", map[string]any{"box": []float64{40, 40, 36, 36}})
	}},
	{by: "prop.Transform", scope: "p:grpSp/p:grpSpPr/a:xfrm/**", op: func(map[string]any) map[string]any {
		return setOp("s7", map[string]any{"box": []float64{80, 300, 144, 72}})
	}},
	{by: "prop.Name", scope: "p:sp/p:nvSpPr/p:cNvPr/**", except: "**/p:cNvPr/@id", op: func(map[string]any) map[string]any {
		return setOp("s2", map[string]any{"name": "Renamed"})
	}},
	{by: "prop.Name", scope: "p:cxnSp/p:nvCxnSpPr/p:cNvPr/**", except: "**/p:cNvPr/@id", op: func(map[string]any) map[string]any {
		return setOp("s5", map[string]any{"name": "Renamed"})
	}},
	{by: "prop.Name", scope: "p:grpSp/p:nvGrpSpPr/p:cNvPr/**", except: "**/p:cNvPr/@id", op: func(map[string]any) map[string]any {
		return setOp("s7", map[string]any{"name": "Renamed"})
	}},
	{by: "prop.Name", scope: "p:pic/p:nvPicPr/p:cNvPr/**", except: "**/p:cNvPr/@id", op: func(map[string]any) map[string]any {
		return setOp("s9", map[string]any{"name": "Renamed"})
	}},
	{by: "prop.Glue", scope: "p:cxnSp/p:nvCxnSpPr/p:cNvCxnSpPr/**", op: func(map[string]any) map[string]any {
		return setOp("s5", map[string]any{"from_glue": "s6:2"})
	}},
	{by: "prop.Endpoints", scope: "p:cxnSp/p:spPr/a:xfrm/**", op: func(map[string]any) map[string]any {
		return setOp("s5", map[string]any{"from": []float64{200, 100}, "to": []float64{300, 160}})
	}},
}

// carryOutcome is whether a path came through every rewrite whose scope
// holds it, and why not.
type carryOutcome struct {
	carried bool
	why     string
}

var (
	carryOnce sync.Once
	carryRes  map[string]carryOutcome
)

// carryScan instantiates each path in a rewrite's scope (except those a
// write claim says the codecs write) and runs the rewrite.
func carryScan(t *testing.T) map[string]carryOutcome {
	u := loadUniverse(t)
	carryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "coverage-carry")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		type job struct {
			path string
			rw   []rewrite
		}
		var jobs []job
		for _, it := range u.Items {
			if !probeable(&it) || it.Node == "any" {
				continue
			}
			if c := Decide(Write, it.Path); c != nil && (c.Status == RoundTripLaw || c.Status == WriteCodec) {
				continue
			}
			var rs []rewrite
			for _, r := range rewrites {
				if (&Claim{Paths: []string{r.scope}}).Matches(it.Path) && (r.except == "" || !(&Claim{Paths: []string{r.except}}).Matches(it.Path)) {
					rs = append(rs, r)
				}
			}
			if len(rs) > 0 {
				jobs = append(jobs, job{it.Path, rs})
			}
		}
		res := make([]carryOutcome, len(jobs))
		var wg sync.WaitGroup
		next := make(chan int)
		for w := 0; w < runtime.NumCPU(); w++ {
			wg.Go(func() {
				for i := range next {
					res[i] = carryOutcome{carried: true}
					for _, r := range jobs[i].rw {
						if why := probeCarry(t, dir, jobs[i].path, r); why != "" {
							res[i] = carryOutcome{why: r.by + ": " + why}
							break
						}
					}
				}
			})
		}
		for i := range jobs {
			next <- i
		}
		close(next)
		wg.Wait()
		carryRes = map[string]carryOutcome{}
		for i, j := range jobs {
			carryRes[j.path] = res[i]
		}
	})
	return carryRes
}

// probeCarry puts the path in its sample state, runs the rewrite and
// says how the path changed ("" when it came through).
func probeCarry(t testing.TB, dir, path string, r rewrite) string {
	it := uniBy[path]
	f := newFixture(t)
	v := ""
	if it.Node == "attribute" || isSimple(it) {
		v = sample(it, true)
	}
	if err := f.setState(path, it, v, true, r.ctx, false); err != nil {
		return err.Error()
	}
	before, _, _, err := f.place(path, r.ctx)
	if err != nil {
		return err.Error()
	}
	want := valueAt(before, path, it)
	src := f.write(t, dir)
	p, err := f.project(t, dir)
	if err != nil {
		return err.Error()
	}
	root, _, _ := resolve(path)
	id := rootParts[root][r.ctx].id
	var obj map[string]any
	for _, o := range p["objects"].([]any) {
		if m := o.(map[string]any); m["id"] == id || (root == "p:cxnSp" && m["id"] == "s5") {
			obj = m
		}
	}
	dst := strings.TrimSuffix(src, ".pptx") + "-out.pptx"
	if err := applyPlan(src, dst, r.op(obj)); err != nil {
		return "refused: " + err.Error()
	}
	g, err := readFixture(dst)
	if err != nil {
		return err.Error()
	}
	after, _, _, err := g.place(path, r.ctx)
	if err != nil {
		return err.Error()
	}
	if got := valueAt(after, path, it); got != want {
		return fmt.Sprintf("%q became %q", want, got)
	}
	return ""
}

// valueAt is the state of the path in its holder: an attribute's value,
// an element's serialisation, or "(absent)".
func valueAt(holder *el, path string, it *Item) string {
	name := lastStep(path)
	if it.Node == "attribute" {
		if v, ok := holder.attr(strings.TrimPrefix(name, "@")); ok {
			return v
		}
		return "(absent)"
	}
	if holder.child(name) != nil {
		// The element itself is present; its attributes and children
		// are paths of their own.
		return "(present)"
	}
	return "(absent)"
}

func TestCarryDump(t *testing.T) {
	out := os.Getenv("COVERAGE_CARRY_OUT")
	if out == "" {
		t.Skip("COVERAGE_CARRY_OUT not set")
	}
	var lines []string
	for p, o := range carryScan(t) {
		lines = append(lines, fmt.Sprintf("%v\t%s\t%s", o.carried, p, o.why))
	}
	sort.Strings(lines)
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCarryClaimsMatchBehaviour checks the preserved-raw write claims:
// a path in a rewrite's scope is claimed carried exactly when it comes
// through the rewrite unchanged.
func TestCarryClaimsMatchBehaviour(t *testing.T) {
	res := carryScan(t)
	var bad []string
	var paths []string
	for p := range res {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		o := res[p]
		c := Decide(Write, p)
		claimed := c != nil && c.Status == PreservedRaw
		switch {
		case o.carried && !claimed:
			bad = append(bad, "undeclared carried: "+p)
		case !o.carried && claimed:
			bad = append(bad, fmt.Sprintf("declared carried by %s, not carried: %s: %s", c.By, p, o.why))
		}
	}
	// A carry claim must not reach beyond the rewrites' scopes.
	for _, it := range loadUniverse(t).Items {
		if c := Decide(Write, it.Path); c != nil && c.Status == PreservedRaw && probeable(&it) {
			if _, ok := res[it.Path]; !ok {
				bad = append(bad, fmt.Sprintf("declared carried by %s, but no rewrite covers it: %s", c.By, it.Path))
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d carry claims disagree with the rewrites:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}
