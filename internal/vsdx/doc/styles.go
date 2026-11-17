package doc

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// StyleSheet is one StyleSheet of document.xml: its ID, names, the
// parent style IDs of its three categories (absent parents are
// HasLine etc. false) and its node, whose cells and sections the
// inheritance resolver reads.
type StyleSheet struct {
	ID                        uint32
	Name, NameU               string
	LineStyle, FillStyle      uint32
	TextStyle                 uint32
	HasLine, HasFill, HasText bool
	Node                      *tree.Node
}

// readStyles reads document.xml's StyleSheets, FaceNames (NameU in
// order) and Colors (ColorEntry IX and RGB). A StyleSheet whose ID
// repeats, or a ColorEntry whose IX repeats or is below 24, is left out
// with a note. Chunk D3.
func (d *Document) readStyles(root *tree.Node) {
	d.Styles = make(map[uint32]*StyleSheet)
	d.Colors = make(map[int]string)
	d.FaceNames = nil
	if root == nil {
		return
	}
	if colors := childMain(root, "Colors"); colors != nil {
		for elem := range colors.ElementsSeq() {
			if !isMain(elem, "ColorEntry") {
				continue
			}
			ixText, ok := elem.Attr("IX")
			if !ok {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "color entry IX does not parse"})
				continue
			}
			ix, err := strconv.Atoi(ixText)
			if err != nil {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "color entry IX does not parse"})
				continue
			}
			if ix < 24 {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "color entry IX below 24"})
				continue
			}
			if _, dup := d.Colors[ix]; dup {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "duplicate color entry IX " + strconv.Itoa(ix)})
				continue
			}
			rgb, ok := elem.Attr("RGB")
			if !ok {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "color entry has no RGB"})
				continue
			}
			d.Colors[ix] = rgb
		}
	}
	if faces := childMain(root, vsd.ElemFaceNames); faces != nil {
		for elem := range faces.ElementsSeq() {
			if !isMain(elem, "FaceName") {
				continue
			}
			nameU, ok := elem.Attr("NameU")
			if !ok {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "face name has no NameU"})
				continue
			}
			d.FaceNames = append(d.FaceNames, nameU)
		}
	}
	if sheets := childMain(root, vsd.ElemStyleSheets); sheets != nil {
		for elem := range sheets.ElementsSeq() {
			if !isMain(elem, vsd.ElemStyleSheet) {
				continue
			}
			id, ok := uintAttr(elem, "ID")
			if !ok {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "style sheet id does not parse"})
				continue
			}
			if _, dup := d.Styles[id]; dup {
				d.Notes = append(d.Notes, Note{At: d.Part, Reason: "duplicate style sheet id " + strconv.FormatUint(uint64(id), 10)})
				continue
			}
			sheet := &StyleSheet{ID: id, Node: elem}
			sheet.Name, _ = elem.Attr("Name")
			sheet.NameU, _ = elem.Attr("NameU")
			if v, ok := uintAttr(elem, "LineStyle"); ok {
				sheet.LineStyle, sheet.HasLine = v, true
			}
			if v, ok := uintAttr(elem, "FillStyle"); ok {
				sheet.FillStyle, sheet.HasFill = v, true
			}
			if v, ok := uintAttr(elem, "TextStyle"); ok {
				sheet.TextStyle, sheet.HasText = v, true
			}
			d.Styles[id] = sheet
		}
	}
}

// readCustom sets Recalc from docProps/custom.xml's RecalcDocument
// property (found through the package relationships). Chunk D3.
func (d *Document) readCustom() {
	rootRels, err := d.Pkg.Rels("/")
	if err != nil {
		d.Notes = append(d.Notes, Note{At: "/", Reason: "unreadable: " + err.Error()})
		return
	}
	rel, ok := relByType(rootRels, ctypes.RelCustomProperties)
	if !ok {
		return
	}
	part, ok := relTarget(rootRels, rel)
	if !ok {
		d.Notes = append(d.Notes, Note{At: "/", Reason: "no part"})
		return
	}
	if !d.Pkg.Has(part) {
		d.Notes = append(d.Notes, Note{At: part, Reason: "no part"})
		return
	}
	doc, err := partTree(d.Pkg, part)
	if err != nil {
		d.Notes = append(d.Notes, Note{At: part, Reason: "unreadable: " + err.Error()})
		return
	}
	root := doc.Root()
	if root == nil || root.Local() != "Properties" {
		d.Notes = append(d.Notes, Note{At: part, Reason: "not a Properties part"})
		return
	}
	for prop := range root.ElementsSeq() {
		if prop.Local() != "property" {
			continue
		}
		name, ok := prop.Attr("name")
		if !ok || name != "RecalcDocument" {
			continue
		}
		for val := range prop.ElementsSeq() {
			if val.Local() != "bool" {
				continue
			}
			if b, ok := vsd.Bool(val.Text()); ok {
				d.Recalc = b
			} else {
				d.Notes = append(d.Notes, Note{At: part, Reason: "RecalcDocument does not parse"})
			}
			return
		}
		return
	}
}
