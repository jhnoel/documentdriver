package apply

import (
	"fmt"
	"maps"
	"slices"

	"github.com/jhnoel/documentdriver/internal/docx/view"
)

// sameAffected is the law that the markup a result reports comes from
// the filtered render: the changed paragraphs' markup, and that of
// pseudo-random address sets of the output, equal what a render of the
// whole output (view.AffectedMarkupFull) gives.
func sameAffected(r *Result) error {
	if r.Output == nil {
		return nil
	}
	full, err := view.AffectedMarkupFull(r.Output, r.Changed, viewOpt)
	if err != nil {
		return fmt.Errorf("full render: %w", err)
	}
	if r.Markup != nil && !maps.Equal(r.Markup, full) {
		return fmt.Errorf("markup of the changed paragraphs %v, the full render gives %v", r.Markup, full)
	}
	var all []string
	for _, id := range r.Output.ParaIDs().IDs {
		all = append(all, id.ID)
	}
	slices.Sort(all)
	for _, p := range view.StoryParagraphs(r.Output) {
		all = append(all, p.ID)
	}
	if len(all) == 0 {
		return nil
	}
	x := uint64(len(r.Snapshot))*0x9E3779B97F4A7C15 | 1
	sets := [][]string{all}
	for range 4 {
		var set []string
		for range 1 + x%4 {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
			set = append(set, all[x%uint64(len(all))])
		}
		sets = append(sets, set)
	}
	for _, set := range sets {
		got, err := view.AffectedMarkup(r.Output, set, viewOpt)
		if err != nil {
			return fmt.Errorf("affected markup: %w", err)
		}
		want, err := view.AffectedMarkupFull(r.Output, set, viewOpt)
		if err != nil {
			return fmt.Errorf("full render: %w", err)
		}
		if !maps.Equal(got, want) {
			return fmt.Errorf("affected markup of %v differs from the full render's", set)
		}
	}
	return nil
}
