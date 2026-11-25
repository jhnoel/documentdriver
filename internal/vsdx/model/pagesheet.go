package model

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// sheetShape builds the resolver shape of a PageSheet node (a page's
// or a master's): its indexed sheet and its LineStyle, FillStyle and
// TextStyle attributes as stored, for the style-chain lookup of its
// cells. A nil node gives an empty shape with no sheet. Chunk NW
// (extracted from pageFacts, for pages and masters alike).
func sheetShape(node *tree.Node) (*inherit.Shape, *sheet.Sheet) {
	shape := &inherit.Shape{}
	if node == nil {
		return shape, nil
	}
	s, _ := sheet.Index(node)
	shape.Sheet = s
	for name, value := range node.AttrsSeq() {
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
			shape.Styles[i], shape.HasStyle[i] = uint32(u), true
		}
	}
	return shape, s
}

// masterFacts fills p's page height (and size, when both width and
// height are derived) from master m's PageSheet, in the frame the
// contract draft gives a master: its own page sheet, origin top-left,
// y down. It reports whether PageHeight was derived; without it p
// keeps H zero and the caller leaves box, from and to out, with a
// note naming the master. Chunk NW.
func masterFacts(p *Page, r *inherit.Resolver, m *doc.MasterEntry) bool {
	if p == nil || m == nil {
		return false
	}
	pageShape, _ := sheetShape(m.PageSheet)
	wCell, _, wOK := r.Cell(pageShape, sheet.ShapeCell(vsd.CellPageWidth))
	hCell, _, hOK := r.Cell(pageShape, sheet.ShapeCell(vsd.CellPageHeight))
	w, wNum := vsd.Number(wCell.V)
	h, hNum := vsd.Number(hCell.V)
	hasHeight := hOK && hNum
	if hasHeight {
		p.H = h
	}
	if wOK && hasHeight && wNum {
		p.W = w
		p.HasSize = true
		switch {
		case h > w:
			p.Orientation = "portrait"
		case w > h:
			p.Orientation = "landscape"
		default:
			p.Orientation = ""
		}
	}
	if !hasHeight {
		p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "master has no PageHeight"})
	}
	return hasHeight
}

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
		if d != nil && !hasPageID(d, e.BackPage) {
			p.BackPage = "#" + strconv.FormatUint(uint64(e.BackPage), 10)
			p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "background page " + p.BackPage + " is not in the drawing"})
		}
	} else {
		p.BackPage = ""
	}
	pageShape, pageSheet := sheetShape(e.PageSheet)
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
		if drawingScale, _, ok := r.Cell(pageShape, sheet.ShapeCell("DrawingScale")); ok && scaled(pageScale, drawingScale) {
			p.Notes = append(p.Notes, doc.Note{At: p.ID, Reason: "drawing scale " + scaleText(pageScale, drawingScale) + ": lengths are stored drawing units"})
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

// scaleText returns the drawing scale of two scale cells as
// "<PageScale> = <DrawingScale>", each as its stored value (geom.FormatNum)
// and unit abbreviation as stored (IN_F and FT_I as in and ft), which
// chunk CC6 derives the page note and the scaled-page refusal from.
// It reports "" when either cell is not a number.
func scaleText(pageScale, drawingScale sheet.Cell) string {
	ps := scalePair(pageScale)
	ds := scalePair(drawingScale)
	if ps == "" || ds == "" {
		return ""
	}
	return ps + " = " + ds
}

// scaled reports whether two scale cells differ, as numbers or in
// unit (IN and IN_F are the same unit, FT and FT_I likewise): a page
// whose PageScale and DrawingScale differ is a scaled page, whose
// geometry v1 does not edit. A cell that is not a number is never
// scaled.
func scaled(pageScale, drawingScale sheet.Cell) bool {
	ps, psOK := vsd.Number(pageScale.V)
	ds, dsOK := vsd.Number(drawingScale.V)
	if !psOK || !dsOK {
		return false
	}
	return ps != ds || scaleUnit(pageScale.U) != scaleUnit(drawingScale.U)
}

// scalePair returns one scale cell as "<number> <unit>" (the number
// alone when the unit is empty), or "" when the cell is not a number.
func scalePair(cell sheet.Cell) string {
	n, ok := vsd.Number(cell.V)
	if !ok {
		return ""
	}
	if u := scaleUnit(cell.U); u != "" {
		return geom.FormatNum(n) + " " + u
	}
	return geom.FormatNum(n)
}

// scaleUnit returns Visio's abbreviation for a stored unit U: IN,
// IN_F to in, FT, FT_I to ft, MM to mm, CM to cm, M to m, and any
// other unit as stored.
func scaleUnit(u string) string {
	switch u {
	case "IN", "IN_F":
		return "in"
	case "FT", "FT_I":
		return "ft"
	case "MM":
		return "mm"
	case "CM":
		return "cm"
	case "M":
		return "m"
	}
	return u
}

// layerMembers returns the layer addresses of a LayerMember value
// ("0;2"); an index with no layer row is left out with a note. Chunk M6.
func layerMembers(p *Page, v string) []string {
	if p == nil || v == "" {
		return nil
	}
	var out []string
	for tok := range strings.SplitSeq(v, ";") {
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

// hasPageID reports whether d has a page with stored ID id.
func hasPageID(d *doc.Document, id uint32) bool {
	for i := range d.Pages {
		if d.Pages[i].ID == id {
			return true
		}
	}
	return false
}
