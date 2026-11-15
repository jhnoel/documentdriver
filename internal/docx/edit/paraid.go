package edit

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Paragraph identity. docxdriver resolves w14:paraIds when a paragraph is
// first addressed and again when it writes the package, each time over
// the document as it then stands: a paragraph without a valid id gets one
// seeded by its story position at that moment. An operation that inserts
// or removes paragraphs before it (a table operation, insert_paragraph)
// shifts that position, so the id the plan addressed is not the id
// written. Here the ids are resolved once, when the workspace opens, over
// the input document, and bound to the paragraph nodes: the id a plan
// reads is the id it writes, whatever runs first (listed as a known
// difference).

// resolveParaIDs resolves the main document part's paragraph ids.
func (w *Workspace) resolveParaIDs() error {
	doc, err := w.Tree(w.doc)
	if err != nil {
		return err
	}
	x := &model.XML{Doc: doc}
	idx := x.ResolveParagraphIDs(strings.TrimPrefix(w.doc, "/"), w.opt.Source)
	if idx.AllocationErr != nil {
		return idx.AllocationErr
	}
	w.ids, w.repaired = idx.IDs, idx.Repaired
	return nil
}

// ParaID returns the identity of paragraph p of the main document part as
// resolved when the workspace opened (its valid w14:paraId, or the repair
// it will be written with), and false for a paragraph that did not exist
// then.
func (w *Workspace) ParaID(p *tree.Node) (model.ParaID, bool) {
	id, ok := w.ids[p]
	return id, ok
}

// paraIDAttr returns the qualified name of p's stored paraId attribute
// (whatever its prefix, as the read model finds it), or "".
func paraIDAttr(p *tree.Node) string {
	for _, a := range p.Attrs() {
		if _, local, ok := strings.Cut(a.Name, ":"); (ok && local == "paraId" && !strings.HasPrefix(a.Name, "xmlns:")) || a.Name == "paraId" {
			return a.Name
		}
	}
	return ""
}

// Finish prepares the working copy for commit. Unless the save is a no-op
// (no part changed), it writes every paragraph id that is missing from
// the main document part (a paragraph with no paraId attribute at all),
// as resolved at open, through the write-set kinds for repaired identity
// attributes and root namespaces, untargeted paragraphs included: left
// unstored, those ids would be re-derived from the changed file's bytes
// on the next read and every address an agent holds would break. It is
// the one stated exception to leaving untargeted content alone. Stored
// ids that are malformed or duplicated are never rewritten (duplicates
// are addressed by position). Finish also gives every paragraph the plan
// created without a unique valid id of its own (a copy) a fresh one. Call
// it once, after the last operation and before Verify and commit.
func (w *Workspace) Finish() error {
	changed, err := w.pkg.ChangedParts()
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return nil
	}
	doc, err := w.Tree(w.doc)
	if err != nil {
		return err
	}
	all := (&model.XML{Doc: doc}).AllParagraphs()
	var missing []*tree.Node
	for _, p := range all {
		if w.repaired[p] && paraIDAttr(p) == "" {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		if err := w.core.EnsureRootNamespace(w.doc, W14.Prefix, W14.URIs[0], true); err != nil {
			return err
		}
		for _, p := range missing {
			if err := w.core.Repair(w.doc, p, W14.Prefix+":paraId", w.ids[p].ID); err != nil {
				return fmt.Errorf("edit: writing the paragraph id %s: %w", w.ids[p].ID, err)
			}
		}
	}
	return w.fixCreatedParaIDs(all)
}
