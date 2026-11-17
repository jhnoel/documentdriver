// Package doc opens a Visio package: it finds the document part through
// the package relationships, lists the pages (pages.xml) and masters
// (masters.xml) in stored order, reads the style sheets and font names
// of document.xml, and hands out a page's or master's parsed contents.
// Page contents are parsed with tree.ParseString on the part's original
// string, never Part.Tree, so a reader that visits pages one at a time
// holds one page tree at a time (docs/vsdx-design.md, Performance).
package doc

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Kind is the package kind, from the document part's content type.
type Kind uint8

// Package kinds. Stencils are read (masters only) and never edited.
const (
	Drawing  Kind = iota + 1 // .vsdx, .vsdm
	Template                 // .vstx, .vstm
	Stencil                  // .vssx, .vssm
)

// Document is an open Visio package: the parts lists, the style
// sheets and the notes about what could not be read. Masters' trees
// are parsed on first use and kept (they are small and shared by every
// page); page trees are not kept.
type Document struct {
	Pkg     *opc.Package
	Kind    Kind
	Macro   bool
	Part    string // the document part, /visio/document.xml
	Pages   []PageEntry
	Masters []MasterEntry
	Styles  map[uint32]*StyleSheet
	// FaceNames are the NameU of document.xml's FaceName elements in
	// stored order. A Font cell stores the face name itself ("Calibri",
	// "Themed"), never an index; FaceName elements carry no ID.
	FaceNames []string
	// Colors are document.xml's ColorEntry RGB values by IX (24 and up;
	// 0 to 23 are vsd.BuiltinColor and never stored).
	Colors map[int]string
	// Recalc is true when docProps/custom.xml holds RecalcDocument true
	// (read shows values_stale).
	Recalc  bool
	Notes   []Note
	masters map[uint32]*tree.Document
}

// Note is something the reader left out: the nearest address or part
// name, and why (contract section 4).
type Note struct {
	At, Reason string
}

// Open reads the package structure of p: the document part (refused as
// an error, which the caller reports as invalid_document, when the
// package has no Visio document relationship or its part is
// malformed), then pages.xml, masters.xml, document.xml's StyleSheets
// and FaceNames and the custom properties. A malformed pages or masters
// part is a note, not an error (contract section 10). Uses ctypes
// constants generated in chunk S0. Chunk D1.
func Open(p *opc.Package) (*Document, error) {
	d := &Document{Pkg: p, masters: map[uint32]*tree.Document{}}
	root, err := d.openDocument()
	if err != nil {
		return nil, err
	}
	if err := d.readPages(); err != nil {
		return nil, err
	}
	if err := d.readMasters(); err != nil {
		return nil, err
	}
	d.readStyles(root)
	d.readCustom()
	return d, nil
}

// openDocument finds and checks the document part, sets Kind, Macro
// and Part, and returns the document part's root for readStyles. S0's
// drawing content types decide Drawing and Macro; any other Visio
// document (no S0 constant for template or stencil types) is a
// template when it relates a pages part and a stencil otherwise,
// macro-enabled when it relates a vbaProject part.
func (d *Document) openDocument() (*tree.Node, error) {
	rootRels, err := d.Pkg.Rels("/")
	if err != nil {
		return nil, fmt.Errorf("vsdx: package relationships: %w", err)
	}
	rel, ok := relByType(rootRels, ctypes.RelDocument)
	if !ok {
		return nil, fmt.Errorf("vsdx: no Visio document relationship")
	}
	part, ok := relTarget(rootRels, rel)
	if !ok {
		return nil, fmt.Errorf("vsdx: document relationship %s does not resolve", rel.ID)
	}
	d.Part = part
	pt, err := d.Pkg.Part(part)
	if err != nil {
		return nil, documentPartError(part, err)
	}
	ct, err := pt.ContentType()
	if err != nil {
		return nil, documentPartError(part, err)
	}
	doc, err := partTree(d.Pkg, part)
	if err != nil {
		return nil, documentPartError(part, err)
	}
	root := doc.Root()
	if !isMain(root, vsd.ElemVisioDocument) {
		return nil, fmt.Errorf("vsdx: %s is not a Visio document", part)
	}
	switch ct {
	case ctypes.CTMsVisioDrawingMain:
		d.Kind = Drawing
	case ctypes.CTMsVisioDrawingMacroEnabledMain:
		d.Kind, d.Macro = Drawing, true
	default:
		docRels, err := d.Pkg.Rels(part)
		if err != nil {
			return nil, fmt.Errorf("vsdx: document relationships: %w", err)
		}
		if _, ok := relByType(docRels, ctypes.RelPages); ok {
			d.Kind = Template
		} else {
			d.Kind = Stencil
		}
		_, d.Macro = relByType(docRels, ctypes.RelVbaProject)
	}
	return root, nil
}

// documentPartError wraps err naming the document part, unless err
// already carries the "vsdx:" prefix (partTree's own message names
// the part), so every error names its context once. Chunk MF6.
func documentPartError(part string, err error) error {
	if strings.HasPrefix(err.Error(), "vsdx:") {
		return err
	}
	return fmt.Errorf("vsdx: document part %s: %w", part, err)
}

// partTree parses the part's original bytes with tree.ParseString,
// never Part.Tree, so the caller holds the same bytes the package held.
func partTree(p *opc.Package, part string) (*tree.Document, error) {
	pt, err := p.Part(part)
	if err != nil {
		return nil, err
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("vsdx: %s has no original bytes", part)
	}
	return tree.ParseString(s)
}

// relByType returns the first internal relationship of rels of type
// want (a ctypes relationship constant, in either vocabulary).
func relByType(rels *opc.Rels, want string) (opc.Relationship, bool) {
	if rels == nil {
		return opc.Relationship{}, false
	}
	for _, rel := range rels.All() {
		if !rel.External() && ctypes.Is(rel.Type, want) {
			return rel, true
		}
	}
	return opc.Relationship{}, false
}

// relTarget resolves rel through rels to a part name.
func relTarget(rels *opc.Rels, rel opc.Relationship) (string, bool) {
	if rels == nil {
		return "", false
	}
	part, err := rels.TargetPart(rel)
	if err != nil {
		return "", false
	}
	return part, true
}

// isMain reports whether n is an element of local name in a Visio main
// namespace (either spelling).
func isMain(n *tree.Node, local string) bool {
	return n != nil && vsd.IsMain(n.Space()) && n.Local() == local
}

// childMain returns the first element child of parent of local name in
// a Visio main namespace, or nil.
func childMain(parent *tree.Node, local string) *tree.Node {
	for _, e := range parent.Elements() {
		if isMain(e, local) {
			return e
		}
	}
	return nil
}

// uintAttr parses element n's attribute name as a uint32.
func uintAttr(n *tree.Node, name string) (uint32, bool) {
	v, ok := n.Attr(name)
	if !ok {
		return 0, false
	}
	u, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(u), true
}

// entryPart resolves the Rel child of elem through rels to the part it
// points at. An element with no Rel child, or whose relationship id
// rels does not hold or does not resolve, gets Part "" and a note at
// at; the entry itself is kept either way.
func (d *Document) entryPart(rels *opc.Rels, elem *tree.Node, at string) string {
	noPart := func() string {
		d.Notes = append(d.Notes, Note{At: at, Reason: "no part"})
		return ""
	}
	relElem := childMain(elem, vsd.ElemRel)
	if relElem == nil {
		return noPart()
	}
	id, ok := relElem.AttrNS(vsd.NSRel, "id")
	if !ok || id == "" || rels == nil {
		return noPart()
	}
	rel, ok := rels.Get(id)
	if !ok {
		return noPart()
	}
	part, ok := relTarget(rels, rel)
	if !ok {
		return noPart()
	}
	return part
}

// listRoot loads a list part of the document (pages.xml, masters.xml)
// related by want and checks its root local name, returning the root,
// the list part's own relationships and the part name. ok is false
// with a note when the list part is absent, missing or malformed;
// the document's own relationships failing to read is an error.
func (d *Document) listRoot(want, local string) (root *tree.Node, rels *opc.Rels, part string, ok bool, err error) {
	docRels, err := d.Pkg.Rels(d.Part)
	if err != nil {
		return nil, nil, "", false, fmt.Errorf("vsdx: document relationships: %w", err)
	}
	rel, found := relByType(docRels, want)
	if !found {
		return nil, nil, "", false, nil
	}
	part, found = relTarget(docRels, rel)
	if !found {
		d.Notes = append(d.Notes, Note{At: d.Part, Reason: "no part"})
		return nil, nil, "", false, nil
	}
	if !d.Pkg.Has(part) {
		d.Notes = append(d.Notes, Note{At: part, Reason: "no part"})
		return nil, nil, part, false, nil
	}
	doc, err := partTree(d.Pkg, part)
	if err != nil {
		d.Notes = append(d.Notes, Note{At: part, Reason: "unreadable: " + err.Error()})
		return nil, nil, part, false, nil
	}
	root = doc.Root()
	if !isMain(root, local) {
		d.Notes = append(d.Notes, Note{At: part, Reason: "not a " + local + " part"})
		return nil, nil, part, false, nil
	}
	rels, err = d.Pkg.Rels(part)
	if err != nil {
		d.Notes = append(d.Notes, Note{At: part, Reason: "unreadable: " + err.Error()})
		rels = nil
	}
	return root, rels, part, true, nil
}

// Page returns the page entry with stored ID id, and false when there is
// none.
func (d *Document) Page(id uint32) (*PageEntry, bool) {
	for i := range d.Pages {
		if d.Pages[i].ID == id {
			return &d.Pages[i], true
		}
	}
	return nil, false
}

// Master returns the master entry with stored ID id, and false when
// there is none.
func (d *Document) Master(id uint32) (*MasterEntry, bool) {
	for i := range d.Masters {
		if d.Masters[i].ID == id {
			return &d.Masters[i], true
		}
	}
	return nil, false
}
