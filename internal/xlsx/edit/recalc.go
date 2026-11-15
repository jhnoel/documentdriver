package edit

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// applyRecalcPolicy is the recalculation policy (docs/design.md, xlsx:
// Recalculation policy; owner decision 7 and the formula values policy),
// run by Finish after every sheet is flushed (designer-owned):
//
//   - when the plan changed a value, a formula or a sheet's structure
//     (RecalcRequested), calcPr@fullCalcOnLoad="1" is set on the
//     workbook: an existing calcPr gains or keeps the attribute (every
//     other attribute kept); a missing one is made with Ensure in
//     x:CT_Workbook's order. Nothing is written when the input already
//     has fullCalcOnLoad="1" (or "true").
//   - when a formula cell was added, removed or moved (MarkFormulas),
//     the calculation chain is removed: the workbook relationship of type
//     ctypes.RelCalcChain, its target part (DeletePart) and the part's
//     content-type override. A workbook without one is left alone.
//   - nothing else: no <v> is removed or computed, and a plan that marks
//     nothing changes nothing here.
func (w *Workspace) applyRecalcPolicy() error {
	if !w.values && !w.formulas {
		return nil
	}
	if w.values && !w.fullCalc {
		h, err := w.WorkbookHandle()
		if err != nil {
			return err
		}
		calcPr := core.Path{SML.Name("calcPr")}
		if h.Find(calcPr) == nil {
			if err := w.insertSmall(w.wb, h.Elem(0), "x:CT_Workbook", "calcPr", `<calcPr fullCalcOnLoad="1"/>`); err != nil {
				return err
			}
		} else if err := h.SetAttr(calcPr, "fullCalcOnLoad", "1"); err != nil {
			return err
		}
	}
	if !w.formulas {
		return nil
	}
	rels, err := w.pkg.Rels(w.wb)
	if err != nil {
		return err
	}
	for _, r := range rels.ByType(ctypes.RelCalcChain) {
		target := ""
		if !r.External() {
			target, _ = rels.TargetPart(r)
		}
		w.core.DeclarePart(rels.Name())
		rels.Remove(r.ID)
		if target == "" || !w.pkg.Has(target) {
			continue
		}
		if err := w.DeletePart(target); err != nil {
			return err
		}
		ct, err := w.pkg.ContentTypes()
		if err != nil {
			return err
		}
		w.core.DeclarePart(opc.ContentTypesName)
		ct.RemoveOverride(target)
	}
	return nil
}
