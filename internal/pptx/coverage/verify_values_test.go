package coverage

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestReadValuesMatchBehaviour checks enumerated attributes value by
// value: for a read-modelled attribute, each value must show differently
// from another value exactly when the read claim covers it.
func TestReadValuesMatchBehaviour(t *testing.T) {
	u := loadUniverse(t)
	dir, err := os.MkdirTemp("", "coverage-values")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	type job struct{ path, v, other string }
	var jobs []job
	for _, it := range u.Items {
		if it.Node != "attribute" || it.Enum == "" || !probeable(&it) {
			continue
		}
		if c := Decide(Read, it.Path); c == nil || c.Status != ReadModelled {
			continue
		}
		vals := u.Enums[it.Enum]
		if len(vals) < 2 {
			continue
		}
		for i, v := range vals {
			other := vals[0]
			if i == 0 {
				other = vals[1]
			}
			jobs = append(jobs, job{it.Path, v, other})
		}
	}
	res := make([]string, len(jobs))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Go(func() {
			for i := range next {
				seen, err := probeRead(t, dir, jobs[i].path, jobs[i].other, jobs[i].v)
				if err != nil {
					seen = "error: " + err.Error()
				}
				res[i] = seen
			}
		})
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	var bad []string
	for i, j := range jobs {
		c := Decide(Read, j.path)
		shown := res[i] == seenModelled
		switch {
		case c.Covers(j.v) && !shown:
			bad = append(bad, fmt.Sprintf("%s=%s (%s): claimed, not shown apart from %s (%s)", j.path, j.v, c.By, j.other, res[i]))
		case !c.Covers(j.v) && shown:
			bad = append(bad, fmt.Sprintf("%s=%s (%s): shown, not claimed", j.path, j.v, c.By))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d enumerated values disagree with the probes:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	t.Logf("%d values probed", len(jobs))
}

// TestFacetsMatchBehaviour checks each facet claim value by value
// against the read model.
func TestFacetsMatchBehaviour(t *testing.T) {
	u := loadUniverse(t)
	dir, err := os.MkdirTemp("", "coverage-facets")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	var bad []string
	checked := 0
	for _, c := range Claims {
		if c.Facet == "" || c.Has == nil {
			continue
		}
		probe := facetProbes[c.Facet]
		if probe == nil {
			bad = append(bad, "no probe for facet "+c.Facet)
			continue
		}
		for _, it := range u.Items {
			if !c.Matches(it.Path) || it.Enum == "" {
				continue
			}
			for _, v := range u.Enums[it.Enum] {
				checked++
				has, err := probe(t, dir, it.Path, v)
				if err != nil {
					bad = append(bad, fmt.Sprintf("%s %s=%s: %v", c.Facet, it.Path, v, err))
					continue
				}
				if has != c.Has(v) {
					bad = append(bad, fmt.Sprintf("%s %s=%s: claimed %v, read model says %v", c.Facet, it.Path, v, c.Has(v), has))
				}
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d facet values disagree with the read model:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	if checked == 0 {
		t.Error("no facet value checked")
	}
	t.Logf("%d facet values checked", checked)
}

// facetProbes say, for a facet, whether the object at the path's root
// shows it with the path set to v.
var facetProbes = map[string]func(t testing.TB, dir, path, v string) (bool, error){
	"sites":     objectHasKey("sites"),
	"endpoints": objectHasKey("from"),
}

// objectHasKey sets the path to v and reports whether the first object of
// the path's root shows the key in the full view.
func objectHasKey(key string) func(t testing.TB, dir, path, v string) (bool, error) {
	return func(t testing.TB, dir, path, v string) (bool, error) {
		f := newFixture(t)
		it := uniBy[path]
		if err := f.setState(path, it, v, true, 0, false); err != nil {
			return false, err
		}
		p, err := f.project(t, dir)
		if err != nil {
			return false, err
		}
		root, _, _ := resolve(path)
		id := rootParts[root][0].id
		for _, o := range p["objects"].([]any) {
			if m := o.(map[string]any); m["id"] == id {
				_, ok := m[key]
				return ok, nil
			}
		}
		return false, fmt.Errorf("no object %s", id)
	}
}

// TestValidationMatchesBehaviour checks the "validated" facet: every
// settable enumerated attribute is claimed validated exactly when a plan
// giving a value outside its enumeration fails.
func TestValidationMatchesBehaviour(t *testing.T) {
	res := writeScan(t)
	dir, err := os.MkdirTemp("", "coverage-invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ts := writeTmpls()
	var bad []string
	n := 0
	for p := range res {
		it := uniBy[p]
		if it.Enum == "" {
			continue
		}
		tp := tmplFor(ts, p)
		bogus := *tp
		bogus.values = []string{"notInTheSchema"}
		out := probeWrite(t, dir, p, &bogus)
		refused := len(out.passed) == 0
		claimed := false
		for _, f := range Facets(p) {
			if f.Facet == "validated" {
				claimed = true
			}
		}
		n++
		if refused != claimed {
			bad = append(bad, fmt.Sprintf("%s: claimed validated %v, out-of-schema value refused %v", p, claimed, refused))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d validation claims disagree with the plans:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	if n == 0 {
		t.Error("no enumerated write path checked")
	}
}
