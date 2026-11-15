package coverage

import (
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// scanResult is what the read probe saw for one item.
type scanResult struct {
	Path string
	Seen string
	Err  string
}

var (
	scanOnce sync.Once
	scanRes  map[string]scanResult
)

// scanRead probes every probeable item of the universe once per test
// binary, in parallel.
func scanRead(t *testing.T) map[string]scanResult {
	t.Helper()
	u := loadUniverse(t)
	scanOnce.Do(func() {
		dir, err := os.MkdirTemp("", "coverage-scan")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		var paths []string
		for _, it := range u.Items {
			if probeable(&it) {
				paths = append(paths, it.Path)
			}
		}
		res := make([]scanResult, len(paths))
		var wg sync.WaitGroup
		next := make(chan int)
		for w := 0; w < runtime.NumCPU(); w++ {
			wg.Go(func() {
				for i := range next {
					seen, err := probeRead(t, dir, paths[i], "", "")
					r := scanResult{Path: paths[i], Seen: seen}
					if err != nil {
						r.Err = err.Error()
					}
					res[i] = r
				}
			})
		}
		for i := range paths {
			next <- i
		}
		close(next)
		wg.Wait()
		scanRes = map[string]scanResult{}
		for _, r := range res {
			scanRes[r.Path] = r
		}
	})
	return scanRes
}

// probeable reports whether the read scan probes an item: every element
// and attribute below a root, except object elements below a group or
// shape tree (each is probed as its own root), xsd:any, and the roots
// themselves.
func probeable(it *Item) bool {
	if it.Node == "any" || it.Object || !strings.Contains(it.Path, "/") || strings.HasPrefix(it.Path, "(") && !strings.Contains(it.Path, ")/") {
		return false
	}
	if locs, root := rootParts[it.Path]; root {
		// A root below its part's document element is probed by
		// removing it.
		return len(locs[0].steps) > 0 && locs[0].part != partSlide
	}
	return true
}

// effective is the read scan with each element raised to the strongest
// outcome of its descendants: an element is read when anything below it
// is. A factored element's content is probed at the factored type's host,
// so it does not raise the element at other positions.
func effective(t *testing.T) map[string]string {
	res := scanRead(t)
	u := loadUniverse(t)
	eff := map[string]string{}
	for p, r := range res {
		eff[p] = r.Seen
	}
	// Pseudo-roots collect their content's outcome, which raises their
	// host position below.
	for pseudo := range Hosts {
		eff[pseudo] = seenNothing
	}
	for i := len(u.Items) - 1; i >= 0; i-- {
		it := &u.Items[i]
		par := parentPath(it.Path)
		if par == "" {
			continue
		}
		if _, ok := eff[par]; !ok {
			continue
		}
		eff[par] = stronger(eff[par], eff[it.Path])
	}
	for pseudo, host := range Hosts {
		if _, ok := eff[host]; ok {
			eff[host] = stronger(eff[host], eff[pseudo])
		}
		delete(eff, pseudo)
	}
	return eff
}

func TestScanDump(t *testing.T) {
	out := os.Getenv("COVERAGE_SCAN_OUT")
	if out == "" {
		t.Skip("COVERAGE_SCAN_OUT not set")
	}
	res := scanRead(t)
	eff := effective(t)
	var lines []string
	for _, r := range res {
		lines = append(lines, eff[r.Path]+"\t"+r.Path+"\t"+r.Seen+"\t"+r.Err)
	}
	sort.Strings(lines)
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
