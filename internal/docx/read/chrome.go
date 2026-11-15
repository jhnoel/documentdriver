package read

import (
	"slices"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// chromePartNames returns every header and footer part the body's sections
// reference, in section order (headers default, first, even, then
// footers), each once. Each of the six chains carries forward to later
// sections until a section names its own; a reference that does not
// resolve to a part clears its chain.
func chromePartNames(pkg *model.Package, x *model.XML) []string {
	body := x.Body()
	if body == nil {
		return nil
	}
	rels := pkg.RelTargets()
	main := pkg.DocumentPartName()
	var chains [2][3]string
	var set [2][3]bool
	tags := [2]string{"headerReference", "footerReference"}
	kinds := [3]string{"default", "first", "even"}
	var parts []string
	for c := range body.ChildrenSeq() {
		var sect *tree.Node
		switch {
		case model.IsW(c, "p"), model.IsW(c, "tbl"):
			sect = model.FindSectPr(c)
		case model.IsW(c, "sectPr"):
			sect = c
		}
		if sect == nil {
			continue
		}
		for si, tag := range tags {
			for ki, kind := range kinds {
				for r := range sect.ChildrenSeq() {
					if !model.IsW(r, tag) {
						continue
					}
					t, ok := model.Attr(r, "type")
					if !ok {
						t = "default"
					}
					if t != kind {
						continue
					}
					set[si][ki] = false
					if id, ok := model.Attr(r, "id"); ok {
						if target, ok := rels[id]; ok {
							if part := model.ResolveRelTarget(main, target); pkg.Has(part) {
								chains[si][ki], set[si][ki] = part, true
							}
						}
					}
					break
				}
				if set[si][ki] && !slices.Contains(parts, chains[si][ki]) {
					parts = append(parts, chains[si][ki])
				}
			}
		}
	}
	return parts
}
