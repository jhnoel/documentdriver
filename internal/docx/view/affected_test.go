package view

import (
	"maps"
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
)

// AffectedIDSets returns address sets to compare AffectedMarkup on: every
// body and story paragraph address with one that does not exist, then
// n pseudo-random subsets of one to four of them drawn from seed.
func affectedIDSets(d *model.Document, n int, seed uint64) [][]string {
	var all []string
	for _, id := range d.ParaIDs().IDs {
		all = append(all, id.ID)
	}
	slices.Sort(all)
	for _, p := range StoryParagraphs(d) {
		all = append(all, p.ID)
	}
	all = append(all, "7FFFFFFF")
	sets := [][]string{all}
	x := seed | 1
	next := func() uint64 { // xorshift64
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		return x
	}
	for range n {
		var set []string
		for range 1 + next()%4 {
			set = append(set, all[next()%uint64(len(all))])
		}
		sets = append(sets, set)
	}
	return sets
}

// TestAffectedMarkupFiltered is the law that the filtered AffectedMarkup
// is the full render's: on every fixture (and the corpus under
// DOCX_CORPUS), for every address at once and random address sets.
func TestAffectedMarkupFiltered(t *testing.T) {
	opt := Options{Equation: math.OMMLToLaTeX}
	i := uint64(0)
	for path, d := range fixtures(t) {
		i++
		for _, set := range affectedIDSets(d, 12, i*0x9E3779B97F4A7C15) {
			got, err := AffectedMarkup(d, set, opt)
			want, werr := AffectedMarkupFull(d, set, opt)
			if (err == nil) != (werr == nil) {
				t.Fatalf("%s: error %v, full render %v", path, err, werr)
			}
			if !maps.Equal(got, want) {
				for _, id := range set {
					if got[id] != want[id] {
						t.Errorf("%s: %s:\nfiltered %q\nfull     %q", path, id, got[id], want[id])
						break
					}
				}
				t.Fatalf("%s: filtered markup differs for %v", path, set)
			}
		}
	}
}
