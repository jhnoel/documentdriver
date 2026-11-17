package doc

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// PageEntry is one Page of pages.xml in stored order: its ID, names,
// background flags, its PageSheet node (in the pages.xml tree) and the
// part its Rel points at ("" with a note when the Rel does not resolve).
type PageEntry struct {
	ID         uint32
	Name       string
	NameU      string
	Background bool
	BackPage   uint32
	HasBack    bool
	PageSheet  *tree.Node
	Part       string
}

// readPages parses pages.xml (found through the document part's pages
// relationship) into entries, in stored order. Two pages sharing an ID
// are both kept (Page returns the first; the second is addressed with
// its occurrence, page5~2); an entry whose ID does not parse is left
// out with a note. A missing or malformed pages part is a note, not an
// error. Chunk D1.
func (d *Document) readPages() error {
	root, rels, part, ok, err := d.listRoot(ctypes.RelPages, vsd.ElemPages)
	if err != nil || !ok {
		return err
	}
	seen := map[uint32]int{}
	for _, elem := range root.Elements() {
		if !isMain(elem, vsd.ElemPage) {
			continue
		}
		id, ok := uintAttr(elem, "ID")
		if !ok {
			d.Notes = append(d.Notes, Note{At: part, Reason: "page id does not parse"})
			continue
		}
		seen[id]++
		entry := PageEntry{ID: id}
		entry.Name, _ = elem.Attr("Name")
		entry.NameU, _ = elem.Attr("NameU")
		if v, ok := elem.Attr("Background"); ok {
			entry.Background, _ = vsd.Bool(v)
		}
		if v, ok := elem.Attr("BackPage"); ok {
			if u, err := strconv.ParseUint(v, 10, 32); err == nil {
				entry.BackPage, entry.HasBack = uint32(u), true
			} else {
				d.Notes = append(d.Notes, Note{At: vsd.PageID(id, seen[id]), Reason: "back page does not parse"})
			}
		}
		entry.PageSheet = childMain(elem, vsd.ElemPageSheet)
		entry.Part = d.entryPart(rels, elem, vsd.PageID(id, seen[id]))
		d.Pages = append(d.Pages, entry)
	}
	return nil
}
