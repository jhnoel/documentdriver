package doc

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// MasterEntry is one Master of masters.xml in stored order.
type MasterEntry struct {
	ID       uint32
	Name     string
	NameU    string
	UniqueID string
	BaseID   string
	Hidden   bool
	Part     string
	// PageSheet is the Master's PageSheet in masters.xml (nil when
	// the Master stores none): the frame a master is shown in, its
	// PageWidth and PageHeight giving the page height (chunk NW).
	PageSheet *tree.Node
}

// readMasters parses masters.xml into entries, in stored order, as
// readPages does for pages (two masters sharing an ID are both kept;
// an entry whose ID does not parse is left out with a note, and a
// missing or malformed masters part is a note, not an error). A
// document with no masters relationship has no masters and no note.
// Chunk D1.
func (d *Document) readMasters() error {
	root, rels, part, ok, err := d.listRoot(ctypes.RelMasters, vsd.ElemMasters)
	if err != nil || !ok {
		return err
	}
	for _, elem := range root.Elements() {
		if !isMain(elem, vsd.ElemMaster) {
			continue
		}
		id, ok := uintAttr(elem, "ID")
		if !ok {
			d.Notes = append(d.Notes, Note{At: part, Reason: "master id does not parse"})
			continue
		}
		entry := MasterEntry{ID: id}
		entry.Name, _ = elem.Attr("Name")
		entry.NameU, _ = elem.Attr("NameU")
		entry.UniqueID, _ = elem.Attr("UniqueID")
		entry.BaseID, _ = elem.Attr("BaseID")
		if v, ok := elem.Attr("Hidden"); ok {
			entry.Hidden, _ = vsd.Bool(v)
		}
		entry.PageSheet = childMain(elem, vsd.ElemPageSheet)
		entry.Part = d.entryPart(rels, elem, vsd.MasterID(id))
		d.Masters = append(d.Masters, entry)
	}
	return nil
}
