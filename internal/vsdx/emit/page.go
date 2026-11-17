package emit

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Page is a new page: stored ID, name, size in inches and print
// orientation (1 portrait, 2 landscape; 0 writes none), and the
// relationship id of its part in pages.xml.rels.
type Page struct {
	ID          uint32
	Name        string
	Width       float64
	Height      float64
	Orientation int
	RelID       string
}

// PageXML returns the Page element of pages.xml: ID, NameU and Name,
// ViewScale -1, ViewCenterX/Y at the page centre, a PageSheet
// (LineStyle, FillStyle, TextStyle 0) with PageWidth, PageHeight,
// PageScale and DrawingScale 1 (U IN), DrawingSizeType 1,
// DrawingScaleType 0 and, when Orientation is set,
// PrintPageOrientation; then Rel r:id. Chunk K6.
func PageXML(p Page) ([]byte, error) {
	if p.Orientation < 0 || p.Orientation > 2 {
		return nil, fmt.Errorf("vsdx emit: print orientation %d is not 0 (none), 1 (portrait) or 2 (landscape)", p.Orientation)
	}
	if math.IsNaN(p.Width) || math.IsInf(p.Width, 0) || math.IsNaN(p.Height) || math.IsInf(p.Height, 0) {
		return nil, fmt.Errorf("vsdx emit: page size (%v, %v) inches is not finite", p.Width, p.Height)
	}
	name := xmlscan.EscapeAttr(p.Name, '\'')
	rel := xmlscan.EscapeAttr(p.RelID, '\'')

	var b strings.Builder
	b.Grow(384 + 2*len(name) + len(rel))
	b.WriteString("<" + vsd.ElemPage + " ID='" + strconv.FormatUint(uint64(p.ID), 10) +
		"' NameU='" + name + "' Name='" + name +
		"' ViewScale='-1' ViewCenterX='" + vsd.FormatInches(p.Width/2) +
		"' ViewCenterY='" + vsd.FormatInches(p.Height/2) + "'>")
	b.WriteString("<" + vsd.ElemPageSheet + " LineStyle='0' FillStyle='0' TextStyle='0'>")
	b.WriteString(pageCell(vsd.CellPageWidth, vsd.FormatInches(p.Width), ""))
	b.WriteString(pageCell(vsd.CellPageHeight, vsd.FormatInches(p.Height), ""))
	b.WriteString(pageCell("PageScale", "1", "IN"))
	b.WriteString(pageCell("DrawingScale", "1", "IN"))
	b.WriteString(pageCell("DrawingSizeType", "1", ""))
	b.WriteString(pageCell("DrawingScaleType", "0", ""))
	if p.Orientation != 0 {
		b.WriteString(pageCell(vsd.CellOrientation, strconv.Itoa(p.Orientation), ""))
	}
	b.WriteString("</" + vsd.ElemPageSheet + "><" + vsd.ElemRel + " r:id='" + rel + "'/></" + vsd.ElemPage + ">")
	return []byte(b.String()), nil
}

// pageCell returns one PageSheet Cell element: Name n with value v,
// carrying U='IN' when unit is "IN".
func pageCell(n, v, unit string) string {
	if unit == "IN" {
		return "<" + vsd.ElemCell + " N='" + n + "' V='" + v + "' U='IN'/>"
	}
	return "<" + vsd.ElemCell + " N='" + n + "' V='" + v + "'/>"
}

// PageContentsXML returns an empty page part: PageContents in the main
// namespace with xml:space="preserve" and an empty Shapes element, as
// the corpus's pages hold. Chunk K6.
func PageContentsXML() []byte {
	return []byte("<?xml version='1.0' encoding='utf-8' ?>\n<" + vsd.ElemPageContents +
		" xmlns='" + vsd.NSMain2012 + "' xmlns:r='" + vsd.NSRel +
		"' xml:space='preserve'><" + vsd.ElemShapes + "/></" + vsd.ElemPageContents + ">\n")
}
