package edit

import (
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Join is one paragraph join a plan made: Absorbed's identity is gone and
// Survivor's remains (ids as resolved at open, or bound since).
type Join struct {
	Survivor, Absorbed string
}

// HasContent reports whether paragraph p has live content: an element
// other than its properties, a pending deletion, a body-level range
// marker or an emptied insertion.
func HasContent(p *tree.Node) bool {
	for _, c := range p.Children() {
		if !model.IsElement(c) || model.IsW(c, "pPr") || model.IsRevDel(c) ||
			slices.ContainsFunc(model.BodyMilestones, func(l string) bool { return model.IsW(c, l) }) {
			continue
		}
		if model.IsRevIns(c) && len(c.Elements()) == 0 {
			continue
		}
		return true
	}
	return false
}

// IdentityMoves reports whether joining paragraph absorbed into survivor
// (absorbed's mark removed, its content moved to the front of survivor)
// moves absorbed's identity onto survivor: when absorbed has live content
// and survivor has none, the joined paragraph is absorbed's text and
// keeps its id. It is evaluated before the content moves. This is the
// one rule: the settled final and original views and every operation
// that joins paragraphs decide by it.
func IdentityMoves(absorbed, survivor *tree.Node) bool {
	return HasContent(absorbed) && !HasContent(survivor)
}

// JoinParagraphs records that paragraph absorbed of the main document is
// being joined into survivor through region h. Call it before moving the
// content. When the identity moves (IdentityMoves), survivor takes
// absorbed's w14:paraId and w14:textId, both its binding and its stored
// attributes (an unstored id is written at Finish); otherwise survivor
// keeps its own. Either way the id that is gone stays recorded: the
// registry refuses it from then on, naming the survivor (Registry.Joined),
// and Joins lists the join for the plan's result. It returns the
// surviving id.
func (w *Workspace) JoinParagraphs(h *core.Handle, survivor, absorbed *tree.Node) (string, error) {
	sid, aid := w.ids[survivor], w.ids[absorbed]
	keep, gone := sid.ID, aid.ID
	if IdentityMoves(absorbed, survivor) {
		keep, gone = aid.ID, sid.ID
		for _, local := range []string{"paraId", "textId"} {
			from, to := storedAttr(absorbed, local), storedAttr(survivor, local)
			switch {
			case from != "":
				v, _ := absorbed.Attr(from)
				if to == "" {
					to = W14.Prefix + ":" + local
					if err := w.core.EnsureRootNamespace(w.doc, W14.Prefix, W14.URIs[0], true); err != nil {
						return "", err
					}
				}
				if err := h.SetNodeAttr(survivor, to, v); err != nil {
					return "", err
				}
			case to != "":
				if err := h.RemoveNodeAttr(survivor, to); err != nil {
					return "", err
				}
			}
		}
		if _, ok := w.ids[absorbed]; ok {
			w.ids[survivor] = aid
			w.repaired[survivor] = w.repaired[absorbed]
		}
	}
	if gone != "" && keep != "" {
		w.joins = append(w.joins, Join{Survivor: keep, Absorbed: gone})
		w.reg.join(gone, keep, survivor)
	}
	return keep, nil
}

// storedAttr returns the qualified name of p's attribute with local name
// local (any prefix), or "".
func storedAttr(p *tree.Node, local string) string {
	for _, a := range p.Attrs() {
		if strings.HasPrefix(a.Name, "xmlns:") {
			continue
		}
		if a.Name == local || strings.HasSuffix(a.Name, ":"+local) {
			return a.Name
		}
	}
	return ""
}

// Joins returns the paragraph joins made so far, in order.
func (w *Workspace) Joins() []Join { return slices.Clone(w.joins) }
