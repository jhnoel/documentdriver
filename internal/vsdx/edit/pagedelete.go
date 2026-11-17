package edit

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// DeletePage deletes a page: its pages.xml entry, its part, the part's
// Override and the pages relationship that points at it. It refuses a
// page another page uses as BackPage, and the last foreground page.
// Chunk PGD.
func (w *Workspace) DeletePage(page string) error {
	a, err := vsd.Parse(page)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindPage || a.InMaster {
		return fmt.Errorf("vsdx edit: %s: want a page address (page0)", page)
	}
	if w.pagesPart == "" {
		return fmt.Errorf("%w: %s", ErrNotFound, page)
	}
	root, err := w.pagesRoot(page)
	if err != nil {
		return err
	}
	target := findPageElem(root, a.Container, a.PageDup)
	if target == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, page)
	}
	targetBG := false
	if v, ok := target.Attr("Background"); ok {
		targetBG, _ = vsd.Bool(v)
	}
	foregroundOthers := 0
	counts := map[uint32]int{}
	for p := range root.ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		pid, ok := shapeID(p)
		if !ok {
			continue
		}
		counts[pid]++
		v, _ := p.Attr("Background")
		bg, _ := vsd.Bool(v)
		if p == target {
			continue
		}
		if bp, ok := p.Attr("BackPage"); ok {
			if u, err := strconv.ParseUint(bp, 10, 32); err == nil && uint32(u) == a.Container {
				return fmt.Errorf("vsdx edit: %s is the background of %s; delete %s first",
					page, vsd.PageID(pid, counts[pid]), vsd.PageID(pid, counts[pid]))
			}
		}
		if !bg {
			foregroundOthers++
		}
	}
	if !targetBG && foregroundOthers == 0 {
		return fmt.Errorf("vsdx edit: %s is the last foreground page", page)
	}
	var relID, partName string
	if relElem := child(target, vsd.ElemRel); relElem != nil {
		relID, _ = relElem.AttrNS(vsd.NSRel, "id")
	}
	var rels *opc.Rels
	if relID != "" {
		r, err := w.pkg.Rels(w.pagesPart)
		if err != nil {
			return fmt.Errorf("vsdx edit: pages relationships: %w", err)
		}
		rels = r
		if rel, ok := rels.Get(relID); ok {
			if part, err := rels.TargetPart(rel); err == nil {
				partName = part
			}
		}
	}
	if err := w.core.Delete(w.pagesPart, target); err != nil {
		return fmt.Errorf("vsdx edit: delete %s: %w", page, err)
	}
	if rels != nil {
		w.core.DeclarePart(rels.Name())
		rels.Remove(relID)
	}
	if partName != "" && w.pkg.Has(partName) {
		w.core.DeclarePart(partName)
		w.core.DeclarePart(opc.ContentTypesName)
		if err := w.pkg.DeletePart(partName); err != nil {
			if !errors.Is(err, opc.ErrNoPart) {
				return fmt.Errorf("vsdx edit: delete part %s: %w", partName, err)
			}
		}
	}
	seen := 0
	for i := range w.doc.Pages {
		if w.doc.Pages[i].ID != a.Container {
			continue
		}
		seen++
		if seen == a.PageDup {
			w.doc.Pages = append(w.doc.Pages[:i], w.doc.Pages[i+1:]...)
			break
		}
	}
	if w.shapes != nil && partName != "" {
		delete(w.shapes, partName)
	}
	return nil
}
