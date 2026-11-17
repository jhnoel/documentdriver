package edit

import (
	"errors"
	"fmt"
	"path"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// AddPage adds a page: its entry (emit.PageXML) in pages.xml after the
// page after (appended when ""), its part (emit.PageContentsXML) at the
// next free visio/pages/page<n>.xml with one Override and one
// relationship. It returns the page's address. Chunk E2.
func (w *Workspace) AddPage(entry func(relID string) ([]byte, error), after string) (string, error) {
	if entry == nil {
		return "", errors.New("vsdx edit: a page entry function is required")
	}
	if w.pagesPart == "" {
		return "", fmt.Errorf("%w: %s", ErrNotFound, "pages")
	}
	if err := w.pkg.RegistersTypes(); err != nil {
		return "", fmt.Errorf("vsdx edit: %w", err)
	}
	var afterAddr vsd.Address
	hasAfter := after != ""
	if hasAfter {
		a, err := vsd.Parse(after)
		if err != nil {
			return "", fmt.Errorf("vsdx edit: %w", err)
		}
		if a.Kind != vsd.KindPage || a.InMaster {
			return "", fmt.Errorf("vsdx edit: %s: want a page address (page0)", after)
		}
		afterAddr = a
	}
	root, err := w.pagesRoot(w.pagesPart)
	if err != nil {
		return "", err
	}
	var largest uint32
	seenAny := false
	var afterNode *tree.Node
	afterSeen := 0
	for p := range root.ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		pid, ok := shapeID(p)
		if !ok {
			continue
		}
		if !seenAny || pid > largest {
			largest, seenAny = pid, true
		}
		if hasAfter && pid == afterAddr.Container {
			afterSeen++
			if afterSeen == afterAddr.PageDup {
				afterNode = p
			}
		}
	}
	if hasAfter && afterNode == nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, after)
	}
	var nextID uint32
	if seenAny {
		if largest == ^uint32(0) {
			return "", errors.New("vsdx edit: page ID space exhausted")
		}
		nextID = largest + 1
	}
	rels, err := w.pkg.Rels(w.pagesPart)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: pages relationships: %w", err)
	}
	relID := rels.NextID()
	partName := nextPagePart(w.pkg, path.Dir(w.pagesPart))
	frag, err := entry(relID)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: page entry: %w", err)
	}
	nodes, err := w.core.Parse(root, frag, nil)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: page entry: %w", err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemPage) {
		return "", errors.New("vsdx edit: page entry is not a single Page element")
	}
	pageNode := nodes[0]
	relElem := child(pageNode, vsd.ElemRel)
	if relElem == nil {
		return "", errors.New("vsdx edit: page entry has no Rel")
	}
	gotRel, ok := relElem.AttrNS(vsd.NSRel, "id")
	if !ok || gotRel != relID {
		return "", fmt.Errorf("vsdx edit: page entry relates %q, want %q", gotRel, relID)
	}
	var pos core.Position
	if hasAfter {
		pos = core.After(afterNode, nil)
	} else {
		pos, err = w.core.SlotEnd(root, "v:Pages", vsdxNS.Name(vsd.ElemPage), true, nil, nil)
		if err != nil {
			return "", err
		}
	}
	name, _ := pageNode.Attr("Name")
	nameU, _ := pageNode.Attr("NameU")
	w.core.DeclarePart(partName)
	w.core.DeclarePart(opc.ContentTypesName)
	w.core.DeclarePart(rels.Name())
	if _, err := w.pkg.AddPart(partName, emit.PageContentsXML(), ctypes.CTMsVisioPage); err != nil {
		return "", fmt.Errorf("vsdx edit: %w", err)
	}
	gotID, err := rels.Add(ctypes.RelPage, path.Base(partName), false)
	if err != nil {
		return "", fmt.Errorf("vsdx edit: %w", err)
	}
	if gotID != relID {
		return "", fmt.Errorf("vsdx edit: relationship %q, want %q", gotID, relID)
	}
	if err := w.core.Insert(w.pagesPart, pos, pageNode, []core.NodeAttr{{Node: pageNode, Name: "ID", Value: strconv.FormatUint(uint64(nextID), 10)}}); err != nil {
		return "", err
	}
	w.doc.Pages = append(w.doc.Pages, doc.PageEntry{ID: nextID, Name: name, NameU: nameU, PageSheet: child(pageNode, vsd.ElemPageSheet), Part: partName})
	return vsd.PageID(nextID, 1), nil
}

// RenamePage sets a page's Name and NameU; a name another page has
// (ignoring case) is refused. Chunk E2.
func (w *Workspace) RenamePage(page, name string) error {
	if name == "" {
		return errors.New("vsdx edit: a page name cannot be empty")
	}
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
	for p := range root.ElementsSeq() {
		if !isElem(p, vsd.ElemPage) || p == target {
			continue
		}
		if n, _ := p.Attr("Name"); ascii.EqualFold(n, name) {
			return fmt.Errorf("vsdx edit: page name %q is already used", name)
		}
		if n, _ := p.Attr("NameU"); ascii.EqualFold(n, name) {
			return fmt.Errorf("vsdx edit: page name %q is already used", name)
		}
	}
	h := w.core.Target(w.pagesPart, page, []*tree.Node{target})
	if err := h.SetAttr(core.Path{}, "NameU", name); err != nil {
		return err
	}
	if err := h.SetAttr(core.Path{}, "Name", name); err != nil {
		return err
	}
	seen := 0
	for i := range w.doc.Pages {
		if w.doc.Pages[i].ID != a.Container {
			continue
		}
		seen++
		if seen == a.PageDup {
			w.doc.Pages[i].Name, w.doc.Pages[i].NameU = name, name
			break
		}
	}
	return nil
}

// findPageElem returns the dup-th Page with stored ID id in root (the
// root's Page children, in stored order), or nil.
func findPageElem(root *tree.Node, id uint32, dup int) *tree.Node {
	seen := 0
	for p := range root.ElementsSeq() {
		if !isElem(p, vsd.ElemPage) {
			continue
		}
		pid, ok := shapeID(p)
		if !ok || pid != id {
			continue
		}
		seen++
		if seen == dup {
			return p
		}
	}
	return nil
}

// pagesRoot returns the root of the pages.xml working tree, reporting
// target unreadable when the part is missing, malformed or not a Pages
// part.
func (w *Workspace) pagesRoot(target string) (*tree.Node, error) {
	d, err := w.core.Tree(w.pagesPart)
	if err != nil {
		return nil, &Unreadable{Target: target, Detail: err.Error()}
	}
	root := d.Root()
	if !isElem(root, vsd.ElemPages) {
		return nil, &Unreadable{Target: target, Detail: "it is not a " + vsd.ElemPages + " part"}
	}
	return root, nil
}

// nextPagePart returns the next free part name dir/page<n>.xml (n from
// 1) that the package does not hold.
func nextPagePart(pkg *opc.Package, dir string) string {
	for n := 1; ; n++ {
		cand := path.Join(dir, fmt.Sprintf("page%d.xml", n))
		if !pkg.Has(cand) {
			return cand
		}
	}
}
