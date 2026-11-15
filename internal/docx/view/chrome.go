package view

import (
	"slices"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// slotKinds is the canonical order of a scope's header or footer slots.
var slotKinds = []string{"default", "first", "even"}

// ChromeSlot is one header or footer a section uses.
type ChromeSlot struct {
	Scope string // "hdr" or "ftr"
	Kind  string // default, first or even
	Part  string
	// Explicit is set when the section's w:sectPr names the slot itself
	// rather than inheriting it from an earlier section.
	Explicit bool
}

// ChromeSection is one section's title-page flag and header and footer
// slots, in canonical order (headers default, first, even, then footers).
type ChromeSection struct {
	N         int // 1-based
	TitlePage bool
	Slots     []ChromeSlot
}

// ChromeMap walks the body's sections and resolves their header and footer
// relationships (docxdriver's html/chrome.rs chrome_map). Each of the six
// slot chains carries forward to later sections until a section names its
// own; a reference that does not resolve to a part clears its chain.
func ChromeMap(pkg *model.Package, x *model.XML) []ChromeSection {
	body := x.Body()
	if body == nil {
		return nil
	}
	rels := pkg.RelTargets()
	main := pkg.DocumentPartName()
	var chains [2][3]string
	var set [2][3]bool
	scopes := [2][2]string{{"hdr", "headerReference"}, {"ftr", "footerReference"}}
	var out []ChromeSection
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
		s := ChromeSection{N: len(out) + 1, TitlePage: onOff(model.ChildW(sect, "titlePg"))}
		for si, sc := range scopes {
			for ki, kind := range slotKinds {
				explicit := false
				for r := range sect.ChildrenSeq() {
					if !model.IsW(r, sc[1]) {
						continue
					}
					t, ok := model.Attr(r, "type")
					if !ok {
						t = "default"
					}
					if t != kind {
						continue
					}
					explicit = true
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
				if set[si][ki] {
					s.Slots = append(s.Slots, ChromeSlot{Scope: sc[0], Kind: kind, Part: chains[si][ki], Explicit: explicit})
				}
			}
		}
		out = append(out, s)
	}
	return out
}

// onOff reports whether an on/off element is present and not turned off.
func onOff(n *tree.Node) bool {
	if n == nil {
		return false
	}
	v, ok := model.Attr(n, "val")
	return !ok || !offValues[v]
}

// ChromePartNames is every header and footer part the sections use, each
// once, in section and slot order.
func ChromePartNames(pkg *model.Package, x *model.XML) []string {
	var parts []string
	for _, s := range ChromeMap(pkg, x) {
		for _, slot := range s.Slots {
			if !slices.Contains(parts, slot.Part) {
				parts = append(parts, slot.Part)
			}
		}
	}
	return parts
}

// SectPrNodes returns every w:sectPr of the body's sections in order: those
// body blocks carry, then a final body-level one.
func SectPrNodes(x *model.XML) []*tree.Node {
	body := x.Body()
	if body == nil {
		return nil
	}
	var out []*tree.Node
	for c := range body.ChildrenSeq() {
		switch {
		case model.IsW(c, "p"), model.IsW(c, "tbl"):
			if s := model.FindSectPr(c); s != nil {
				out = append(out, s)
			}
		case model.IsW(c, "sectPr"):
			out = append(out, c)
		}
	}
	return out
}
