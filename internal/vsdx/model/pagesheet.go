package model

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// pageFacts fills the page's size (PageWidth, PageHeight), Orientation
// (from the size), PrintOrientation (PrintPageOrientation of the page
// sheet through its style chain), the drawing-scale note and Layers
// (Layer rows: Name, IX, Visible, Print, Lock) from its PageSheet.
// Chunk M6.
func pageFacts(p *Page, d *doc.Document, r *inherit.Resolver, e *doc.PageEntry) {
	if p == nil || e == nil {
		return
	}
	dup := 1
	if d != nil {
		dup = pageDup(d, e)
	}
	p.ID = vsd.PageID(e.ID, dup)
	p.Name = e.Name
	p.NameU = e.NameU
	p.Background = e.Background
	if e.HasBack {
		p.BackPage = vsd.PageID(e.BackPage, 1)
	} else {
		p.BackPage = ""
	}
	var pageSheet *sheet.Sheet
	pageShape := &inherit.Shape{}
	if e.PageSheet != nil {
		pageSheet, _ = sheet.Index(e.PageSheet)
		pageShape.Sheet = pageSheet
		for name, value := range e.PageSheet.AttrsSeq() {
			var i int
			switch name {
			case "LineStyle":
				i = 0
			case "FillStyle":
				i = 1
			case "TextStyle":
				i = 2
			default:
				continue
			}
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				pageShape.Styles[i], pageShape.HasStyle[i] = uint32(u), true
			}
		}
	}
	wCell, _, wOK := r.Cell(pageShape, sheet.ShapeCell(vsd.CellPageWidth))
	hCell, _, hOK := r.Cell(pageShape, sheet.ShapeCell(vsd.CellPageHeight))
	w, wNum := vsd.Number(wCell.V)
	h, hNum := vsd.Number(hCell.V)
	if wOK && hOK && wNum && hNum {
		p.W, p.H = w, h
		p.HasSize = true
		switch {
		case h > w:
			p.Orientation = "portrait"
		case w > h:
			p.Orientation = "landscape"
		default:
			p.Orientation = ""
		}
	} else {
		p.HasSize = false
		p.Orientation = ""
		p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "page size not derived"})
	}
	if cell, _, ok := r.Cell(pageShape, sheet.ShapeCell(vsd.CellOrientation)); ok {
		if f, ok := vsd.Number(cell.V); ok {
			switch f {
			case 1:
				p.PrintOrientation = "portrait"
			case 2:
				p.PrintOrientation = "landscape"
			}
		}
	}
	if pageScale, _, ok := r.Cell(pageShape, sheet.ShapeCell("PageScale")); ok {
		if drawingScale, _, ok := r.Cell(pageShape, sheet.ShapeCell("DrawingScale")); ok {
			if ps, ok := vsd.Number(pageScale.V); ok {
				if ds, ok := vsd.Number(drawingScale.V); ok && ps != ds {
					p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "drawing scale 1 in = 10 ft: lengths are stored drawing units"})
				}
			}
		}
	}
	if pageSheet != nil {
		for _, key := range pageSheet.Rows(vsd.SectionLayer) {
			if pageSheet.Deleted(key) {
				continue
			}
			nameCell, ok := pageSheet.Cell(key.WithCell("Name"))
			if !ok {
				continue
			}
			var visible, printLayer, lock bool
			if c, ok := pageSheet.Cell(key.WithCell("Visible")); ok {
				visible, _ = vsd.Bool(c.V)
			}
			if c, ok := pageSheet.Cell(key.WithCell("Print")); ok {
				printLayer, _ = vsd.Bool(c.V)
			}
			if c, ok := pageSheet.Cell(key.WithCell("Lock")); ok {
				lock, _ = vsd.Bool(c.V)
			}
			p.Layers = append(p.Layers, Layer{
				ID:      vsd.LayerID(p.ID, nameCell.V),
				Name:    nameCell.V,
				Index:   key.RowIX,
				Visible: visible,
				Print:   printLayer,
				Lock:    lock,
			})
		}
	}
}

// layerMembers returns the layer addresses of a LayerMember value
// ("0;2"); an index with no layer row is left out with a note. Chunk M6.
func layerMembers(p *Page, v string) []string {
	if p == nil || v == "" {
		return nil
	}
	var out []string
	for _, tok := range strings.Split(v, ";") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		ix, err := strconv.Atoi(tok)
		if err != nil {
			p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "layer index " + tok + " has no layer row"})
			continue
		}
		found := false
		for _, layer := range p.Layers {
			if layer.Index == ix {
				out = append(out, layer.ID)
				found = true
				break
			}
		}
		if !found {
			p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "layer index " + strconv.Itoa(ix) + " has no layer row"})
		}
	}
	return out
}

// PageSheet returns a Page holding only the page-sheet facts of e (ID,
// names, background, size, orientations, layers, notes), without its
// shapes: the summary's page entries (read.Document, chunk R1). Chunk M6.
func PageSheet(d *doc.Document, r *inherit.Resolver, e *doc.PageEntry) *Page {
	p := &Page{}
	pageFacts(p, d, r, e)
	return p
}
