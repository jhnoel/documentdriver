package edit

import (
	"encoding/binary"
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/plan"
)

// The workspace is the one allocator of every id an operation writes:
// revision ids (per part, header and footer stories included), paragraph
// ids (content-derived), comment ids and comment paragraph ids (unique
// across every part), durable ids, bookmark ids, numbering ids,
// relationship ids and part names. Each follows docxdriver 0.5.2's rule,
// named on the method, over the working trees as they stand, so ids
// allocated one after another within a plan never collide. Nothing here
// reads a clock or randomness: given the same input, options and
// operations, the ids are the same.

// allocState is the allocator state that cannot be recomputed from the
// trees: revision ids stay reserved after the revision they named is
// settled away.
type allocState struct {
	revNext map[string]uint64          // part key -> next revision id
	revSeen map[string]map[string]bool // part key -> revision ids in use or used
	revDone map[*tree.Node]bool        // created revision elements already numbered
}

// revisionTags are the WordprocessingML elements whose w:id is a revision
// id; they share one sequence per part. The last four, move range
// markers, pair a start with its end by id and are never renumbered.
var revisionTags = []string{
	"ins", "del", "cellIns", "cellDel", "cellMerge", "moveFrom", "moveTo", "rPrChange",
	"pPrChange", "tblPrChange", "trPrChange", "tcPrChange", "sectPrChange", "tblGridChange",
	"numberingChange", "tblPrExChange",
	"moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd",
}

const revisionRangeMarkers = 16

// isRevision reports whether n is a revision element, including range markers when withMarkers.
func isRevision(n *tree.Node, withMarkers bool) bool {
	tags := revisionTags
	if !withMarkers {
		tags = tags[:revisionRangeMarkers]
	}
	return model.IsElement(n) && slices.ContainsFunc(tags, func(t string) bool { return model.IsW(n, t) })
}

// idAttr returns the qualified name of n's id attribute (w:id), or "".
func idAttr(n *tree.Node) string {
	for _, a := range n.Attrs() {
		if a.Name == "id" || strings.HasSuffix(a.Name, ":id") && !strings.HasPrefix(a.Name, "xmlns:") {
			return a.Name
		}
	}
	return ""
}

// storyScope is the element revision ids are counted under: the body, or
// the part root.
func storyScope(doc *tree.Document) *tree.Node {
	x := &model.XML{Doc: doc}
	if b := x.Body(); b != nil {
		return b
	}
	return doc.Root()
}

// RevisionID returns a fresh revision id for part (docxdriver's
// next_revision_id): one more than the largest revision id (every
// revision element, move range markers included) the part held when it
// was first asked, then counting up; every call returns a new id.
func (w *Workspace) RevisionID(part string) (string, error) {
	if err := w.initRevisions(part); err != nil {
		return "", err
	}
	k := plan.PartKey(part)
	for {
		id := w.alloc.revNext[k]
		w.alloc.revNext[k]++
		s := strconv.FormatUint(id, 10)
		if !w.alloc.revSeen[k][s] {
			w.alloc.revSeen[k][s] = true
			return s, nil
		}
	}
}

// initRevisions reads part's revision ids once: the next id from the
// working tree, the ids in use from the part as it was stored (so ids of
// revisions settled away stay reserved).
func (w *Workspace) initRevisions(part string) error {
	k := plan.PartKey(part)
	if w.alloc.revNext == nil {
		w.alloc = allocState{revNext: map[string]uint64{}, revSeen: map[string]map[string]bool{}, revDone: map[*tree.Node]bool{}}
	}
	if _, ok := w.alloc.revNext[k]; ok {
		return nil
	}
	seen := map[string]bool{}
	pt, err := w.pkg.Part(part)
	if err != nil {
		return err
	}
	if orig, ok, err := pt.OriginalString(); err != nil {
		return err
	} else if ok {
		od, err := tree.ParseString(orig)
		if err != nil {
			return fmt.Errorf("edit: %s: %w", part, err)
		}
		od.Node().Walk(func(n *tree.Node) bool {
			if isRevision(n, false) {
				if v, ok := model.Attr(n, "id"); ok {
					seen[v] = true
				}
			}
			return true
		})
	}
	doc, err := w.Tree(part)
	if err != nil {
		return err
	}
	next, found := uint64(0), false
	if s := storyScope(doc); s != nil {
		s.Walk(func(n *tree.Node) bool {
			if isRevision(n, true) {
				if v, ok := model.Attr(n, "id"); ok {
					if id, err := strconv.ParseUint(v, 10, 64); err == nil {
						next, found = max(next, id), true
					}
				}
			}
			return true
		})
	}
	if found {
		next++
	} else {
		next = 1
	}
	w.alloc.revNext[k], w.alloc.revSeen[k] = next, seen
	return nil
}

// RenumberRevisions gives each revision element the plan created in part
// (move range markers excepted) whose id another revision already holds
// a fresh one (docxdriver's renumber_allocated_revision_ids): a split
// clones a revision wrapper with its id, and only the first holder keeps
// it. Elements are taken in creation order, each once over the life of
// the workspace; call it after every operation. Revision ids are told
// apart by value, not by the created-node record, so it never decides
// which revisions belong to the plan.
func (w *Workspace) RenumberRevisions(part string) error {
	if err := w.initRevisions(part); err != nil {
		return err
	}
	k := plan.PartKey(part)
	roots, err := w.core.CreatedIn(part)
	if err != nil {
		return err
	}
	for _, r := range roots {
		var els []*tree.Node
		r.Walk(func(n *tree.Node) bool {
			if isRevision(n, false) && !w.alloc.revDone[n] {
				els = append(els, n)
			}
			return true
		})
		for _, n := range els {
			w.alloc.revDone[n] = true
			q := idAttr(n)
			if q == "" {
				continue
			}
			v, _ := n.Attr(q)
			if !w.alloc.revSeen[k][v] {
				w.alloc.revSeen[k][v] = true
				continue
			}
			fresh, err := w.RevisionID(part)
			if err != nil {
				return err
			}
			if err := w.core.SetCreatedAttr(n, q, fresh); err != nil {
				return err
			}
		}
	}
	return nil
}

// paraIDSeed is the seed of an inserted paragraph's id: source | op
// index | slot, the integers little-endian. docxdriver also mixes in the
// sha256 of the serde-canonical plan; the plan's bytes are left out here
// (an intended difference: inserted ids are compared by document order).
func (w *Workspace) paraIDSeed(op int, slot uint64) []byte {
	b := make([]byte, 0, 50)
	b = append(b, w.opt.Source[:]...)
	b = append(b, '|')
	b = binary.LittleEndian.AppendUint64(b, uint64(max(op, 0))) // op indices are never negative
	b = append(b, '|')
	return binary.LittleEndian.AppendUint64(b, slot)
}

// paraIDAllocator returns an allocator over part with every valid
// paragraph id in it reserved, and, for the main document part, every id
// resolved at open (repairs not yet written included).
func (w *Workspace) paraIDAllocator(part string) (*model.ParaIDAllocator, error) {
	doc, err := w.Tree(part)
	if err != nil {
		return nil, err
	}
	a := model.NewParaIDAllocator()
	for _, p := range (&model.XML{Doc: doc}).AllParagraphs() {
		if v, ok := model.Attr(p, "paraId"); ok {
			if n, ok := model.ParaIDValue(v); ok {
				a.Reserve(n)
			}
		}
	}
	if plan.PartKey(part) == plan.PartKey(w.doc) {
		for _, id := range w.ids {
			if n, ok := model.ParaIDValue(id.ID); ok {
				a.Reserve(n)
			}
		}
	}
	return a, nil
}

// ParaIDs returns n fresh paragraph ids for paragraphs operation op
// inserts into part, content-derived as docxdriver's are: slot s is
// seeded by source | op | s (see paraIDSeed) and probes
// forward past every id in use. insert_paragraph takes slot 0 (first 0,
// n 1); a new table's cell paragraphs take slots 1..n in row-major order
// (first 1).
func (w *Workspace) ParaIDs(part string, op int, first uint64, n int) ([]string, error) {
	a, err := w.paraIDAllocator(part)
	if err != nil {
		return nil, err
	}
	out := make([]string, n)
	for i := range n {
		v, err := a.Allocate(w.paraIDSeed(op, first+uint64(i)))
		if err != nil {
			return nil, err
		}
		out[i] = model.FormatParaID(v)
	}
	return out, nil
}

// fixCreatedParaIDs gives every paragraph the plan created in the main
// document part (all, in document order) a fresh id when it has no valid
// id of its own or one an earlier paragraph holds: a copied paragraph
// keeps no duplicate of its original's identity. Pre-existing paragraphs
// are reserved first, so the original keeps its id wherever the copy
// lands. The seed is source | "created" | k for the k-th such
// paragraph.
func (w *Workspace) fixCreatedParaIDs(all []*tree.Node) error {
	a := model.NewParaIDAllocator()
	reserve := func(p *tree.Node) bool {
		v, ok := model.Attr(p, "paraId")
		if !ok {
			return false
		}
		n, ok := model.ParaIDValue(v)
		return ok && a.Reserve(n)
	}
	var created []*tree.Node
	for _, p := range all {
		if w.core.Created(p) {
			created = append(created, p)
			continue
		}
		reserve(p)
	}
	k := uint64(0)
	for _, p := range created {
		if reserve(p) {
			continue
		}
		seed := slices.Concat(w.opt.Source[:], []byte("|created|"), binary.LittleEndian.AppendUint64(nil, k))
		k++
		v, err := a.Allocate(seed)
		if err != nil {
			return err
		}
		q := paraIDAttr(p)
		if q == "" {
			if err := w.core.EnsureRootNamespace(w.doc, W14.Prefix, W14.URIs[0], true); err != nil {
				return err
			}
			q = W14.Prefix + ":paraId"
		}
		if err := w.core.SetCreatedAttr(p, q, model.FormatParaID(v)); err != nil {
			return err
		}
	}
	return nil
}

// relatedParts returns the parts the main document part relates to by
// relationship type rel (internal targets that exist), in relationship
// order.
func (w *Workspace) relatedParts(rel string) ([]string, error) {
	rels, err := w.pkg.Rels(w.doc)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rels.All() {
		if r.External() || !ctypes.Is(r.Type, rel) {
			continue
		}
		name, err := rels.TargetPart(r)
		if err != nil || !w.pkg.Has(name) || slices.Contains(out, name) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// StoryParts returns the main document part followed by its headers,
// footers, footnotes, endnotes and comments parts, in that order of kind
// and relationship order within each (docxdriver's story_parts).
func (w *Workspace) StoryParts() ([]string, error) {
	out := []string{w.doc}
	for _, rel := range []string{ctypes.RelHeader, ctypes.RelFooter, ctypes.RelFootnotes, ctypes.RelEndnotes, ctypes.RelComments} {
		parts, err := w.relatedParts(rel)
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// BookmarkID returns a fresh bookmark id, unique among the w:bookmarkStart
// ids of every story (docxdriver's next_bookmark_id): one more than the
// largest, or the smallest unused from 0 when that would pass the 32-bit
// signed range.
func (w *Workspace) BookmarkID() (string, error) {
	parts, err := w.StoryParts()
	if err != nil {
		return "", err
	}
	used := map[int64]bool{}
	var top int64 = -1
	for _, part := range parts {
		doc, err := w.Tree(part)
		if err != nil {
			return "", err
		}
		doc.Node().Walk(func(n *tree.Node) bool {
			if model.IsW(n, "bookmarkStart") {
				if v, err := strconv.ParseInt(model.AttrOr(n, "id"), 10, 64); err == nil {
					used[v] = true
					top = max(top, v)
				}
			}
			return true
		})
	}
	if top+1 <= math.MaxInt32 {
		return strconv.FormatInt(top+1, 10), nil
	}
	for id := int64(0); ; id++ {
		if !used[id] {
			return strconv.FormatInt(id, 10), nil
		}
	}
}

// nextChildID returns one more than the largest decimal value of attribute
// local on the children of part's root that keep reports true, or zero
// when there is none.
func (w *Workspace) nextChildID(part string, keep func(*tree.Node) bool, local string) (uint64, error) {
	doc, err := w.Tree(part)
	if err != nil {
		return 0, err
	}
	var next uint64
	if r := doc.Root(); r != nil {
		for _, c := range r.Elements() {
			if !keep(c) {
				continue
			}
			if v, err := strconv.ParseUint(model.AttrOr(c, local), 10, 64); err == nil && v+1 > next {
				next = v + 1
			}
		}
	}
	return next, nil
}

// NumberingPart returns the main document's numbering part, and false
// when it has none.
func (w *Workspace) NumberingPart() (string, bool, error) {
	parts, err := w.relatedParts(ctypes.RelNumbering)
	if err != nil || len(parts) == 0 {
		return "", false, err
	}
	return parts[0], true, nil
}

// NumID returns a fresh w:numId for the numbering part: one more than the
// largest numId of its w:num definitions as the working tree stands
// (docxdriver's max_num + 1), 1 for a part with none. The numbering part
// is an ordinary working tree; nothing is cached across operations.
func (w *Workspace) NumID(numbering string) (string, error) {
	n, err := w.nextChildID(numbering, func(e *tree.Node) bool { return model.IsW(e, "num") }, "numId")
	return strconv.FormatUint(max(n, 1), 10), err
}

// AbstractNumID returns a fresh w:abstractNumId for the numbering part:
// one more than the largest of its w:abstractNum definitions, 1 for none
// (docxdriver's max_abstract + 1).
func (w *Workspace) AbstractNumID(numbering string) (string, error) {
	n, err := w.nextChildID(numbering, func(e *tree.Node) bool { return model.IsW(e, "abstractNum") }, "abstractNumId")
	return strconv.FormatUint(max(n, 1), 10), err
}

// RelID returns the id the next relationship from source will get:
// rId and one more than the largest rIdN in use (docxdriver's max_rid + 1,
// opc.Rels.NextID).
func (w *Workspace) RelID(source string) (string, error) {
	rels, err := w.pkg.Rels(source)
	if err != nil {
		return "", err
	}
	return rels.NextID(), nil
}

// PartName returns the first part name dir/<stem><n><ext>, n from 1, that
// the package does not hold (docxdriver's chrome part naming:
// word/header3.xml after header1 and header2).
func (w *Workspace) PartName(dir, stem, ext string) string {
	for n := 1; ; n++ {
		name := "/" + path.Join(strings.TrimPrefix(dir, "/"), stem+strconv.Itoa(n)+ext)
		if !w.pkg.Has(name) {
			return name
		}
	}
}
