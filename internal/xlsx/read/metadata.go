package read

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Metadata is what read needs of xl/metadata.xml: for each cellMetadata
// block (a cell's cm is 1-based into them), whether it marks a dynamic
// array.
//
// The chain ([MS-XLSX] §2.6.x; brief "Format facts"):
// cellMetadata/bk[cm-1]/rc@t is a 1-based index into
// metadataTypes/metadataType; when that type's name is "XLDAPR",
// rc@v is a 0-based index into the futureMetadata element whose
// name is "XLDAPR", whose bk[v]/extLst/ext/xda:dynamicArrayProperties
// has fDynamic ("1" or "true" is dynamic). The xda namespace is
// http://schemas.microsoft.com/office/spreadsheetml/2017/dynamicarray.
type Metadata struct {
	// dynamic[i] is the answer for cm i+1: 1 dynamic, 0 not dynamic
	// (a CSE array or another kind of metadata), -1 the chain does not
	// resolve.
	dynamic []int8
}

// parseMetadata reads a parsed metadata part. It fails only when root
// is not <metadata> in a main namespace; any broken link of the chain
// makes that block -1.
func parseMetadata(doc *tree.Document) (*Metadata, error) {
	root := doc.Root()
	if root == nil || !sml.IsMain(root.Space()) || root.Local() != "metadata" {
		return nil, fmt.Errorf("metadata: the part has no metadata root")
	}
	var types []string
	if mt := sml.MainChild(root, "metadataTypes"); mt != nil {
		for n := range mt.ElementsSeq() {
			if sml.IsMain(n.Space()) && n.Local() == "metadataType" {
				name, _ := n.Attr("name")
				types = append(types, name)
			}
		}
	}
	var future []int8
	for n := range root.ElementsSeq() {
		name, _ := n.Attr("name")
		if !sml.IsMain(n.Space()) || n.Local() != "futureMetadata" || name != "XLDAPR" {
			continue
		}
		for bk := range n.ElementsSeq() {
			if sml.IsMain(bk.Space()) && bk.Local() == "bk" {
				future = append(future, dynamicBlock(bk))
			}
		}
		break
	}
	m := &Metadata{}
	if cells := sml.MainChild(root, "cellMetadata"); cells != nil {
		for bk := range cells.ElementsSeq() {
			if sml.IsMain(bk.Space()) && bk.Local() == "bk" {
				m.dynamic = append(m.dynamic, metadataBlock(bk, types, future))
			}
		}
	}
	return m, nil
}

// ArrayKind is the kind of the t="array" formula whose anchor has cell
// metadata cm:
//
//	cm == 0                      model.CSE (no metadata: a legacy array)
//	block resolves, fDynamic     model.Dynamic
//	block resolves, not dynamic  model.CSE
//	m nil, cm out of range, or   model.UnknownArray (the caller notes it,
//	  the chain broken             NoteArray)
//
// Brief: dynamic vs CSE comes from the metadata, never from
// formula.IsDynamicArray.
func (m *Metadata) ArrayKind(cm int) model.ArrayKind {
	if cm == 0 {
		return model.CSE
	}
	if m == nil || cm < 1 || cm > len(m.dynamic) || m.dynamic[cm-1] < 0 {
		return model.UnknownArray
	}
	if m.dynamic[cm-1] == 1 {
		return model.Dynamic
	}
	return model.CSE
}

// Metadata returns the workbook's metadata part, parsed on first use
// (the workbook's RelSheetMetadata target). It is nil when there is
// none, or when it is unreadable (a NotePart note).
func (b *Book) Metadata() *Metadata {
	if b.metaDone {
		return b.meta
	}
	b.metaDone = true
	part := b.target(ctypes.RelSheetMetadata)
	if part == "" {
		return nil
	}
	pt, err := b.pkg.Part(part)
	if err == nil {
		var doc *tree.Document
		doc, err = pt.Tree()
		if err == nil {
			b.meta, err = parseMetadata(doc)
		}
	}
	if err != nil {
		b.recordLimit(err)
		b.Notes.Add(model.NotePart, part, "the metadata part cannot be read; array kinds may be unknown")
	}
	return b.meta
}

// dynamicBlock resolves one XLDAPR futureMetadata block, or returns -1.
func dynamicBlock(bk *tree.Node) int8 {
	const nsDynamic = "http://schemas.microsoft.com/office/spreadsheetml/2017/dynamicarray"
	lst := sml.MainChild(bk, "extLst")
	if lst == nil {
		return -1
	}
	for ext := range lst.ElementsSeq() {
		if !sml.IsMain(ext.Space()) || ext.Local() != "ext" {
			continue
		}
		if pr := ext.Child(nsDynamic, "dynamicArrayProperties"); pr != nil {
			v, _ := pr.Attr("fDynamic")
			switch v {
			case "1", "true":
				return 1
			case "", "0", "false":
				return 0
			default:
				return -1
			}
		}
	}
	return -1
}

// metadataBlock follows each rc link; any broken link makes it unknown.
func metadataBlock(bk *tree.Node, types []string, future []int8) int8 {
	result := int8(-1)
	for rc := range bk.ElementsSeq() {
		if !sml.IsMain(rc.Space()) || rc.Local() != "rc" {
			continue
		}
		text, _ := rc.Attr("t")
		t, ok := sml.ParseIndex(text)
		if !ok || t < 1 || t > len(types) || types[t-1] == "" {
			return -1
		}
		text, _ = rc.Attr("v")
		v, ok := sml.ParseIndex(text)
		if !ok {
			return -1
		}
		kind := int8(0)
		if types[t-1] == "XLDAPR" {
			if v >= len(future) || future[v] < 0 {
				return -1
			}
			kind = future[v]
		}
		result = max(result, kind)
	}
	return result
}
