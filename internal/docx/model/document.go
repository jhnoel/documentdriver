package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// ErrStrict is docxdriver's refusal of ISO Strict documents. The read
// model reads Strict (docxdriver refuses it at open, a limitation not
// copied); operations that cannot yet write Strict refuse with it
// (IsStrict).
var ErrStrict = errors.New("this document is saved as Strict Open XML, which docxdriver does not support; " +
	"save it as a standard Word Document (.docx) and try again")

// Document is the read model of one docx: the package, its parsed main
// document, the input hash, and the derived facts every view (read, find,
// the HTML projection) shares, each computed once on first use.
type Document struct {
	Pkg    *Package
	XML    *XML
	Source [32]byte // SHA-256 of the input bytes

	ids       *ParaIDIndex
	numbering *Numbering
	markers   map[View]map[*tree.Node]string
	lists     map[View]map[*tree.Node]ListInfo
}

// OpenDocument opens the package in b and parses its main document. Errors
// are worded as docxdriver's rejections.
func OpenDocument(b []byte) (*Document, error) {
	pkg, err := Open(b)
	if err != nil {
		return nil, err
	}
	return NewDocument(pkg, sha256.Sum256(b))
}

// NewDocument returns the read model of an opened package whose input
// bytes hash to source (an edit workspace's package, after edits: its
// views read the working trees).
func NewDocument(pkg *Package, source [32]byte) (*Document, error) {
	if !pkg.Has(pkg.DocumentPartName()) {
		return nil, ErrNoDocument
	}
	x, err := pkg.XML(pkg.DocumentPartName())
	if err != nil {
		return nil, err
	}
	return &Document{Pkg: pkg, XML: x, Source: source}, nil
}

// SourceString is "sha256:<hex>" of the input bytes.
func (d *Document) SourceString() string { return "sha256:" + hex.EncodeToString(d.Source[:]) }

// ParaIDs is the main document's paragraph identity resolution, seeded by
// the input hash: the stable address of every paragraph.
func (d *Document) ParaIDs() *ParaIDIndex {
	if d.ids == nil {
		d.ids = d.XML.ResolveParagraphIDs(d.Pkg.DocumentPartName(), d.Source)
		for _, p := range d.XML.AllParagraphs() {
			if d.ids.Unstable[p] {
				v, _ := Attr(p, "paraId")
				d.Pkg.Note(d.ids.IDs[p].ID, "the stored w14:paraId "+strconv.Quote(v)+
					" is not eight hexadecimal digits, so this address is provisional and changes when the file is saved")
			}
		}
	}
	return d.ids
}

// Scope is the body, or the document element when there is none.
func (d *Document) Scope() *tree.Node {
	if b := d.XML.Body(); b != nil {
		return b
	}
	return d.XML.Root()
}

// Numbering is the package's numbering and paragraph-style tables.
func (d *Document) Numbering() *Numbering {
	if d.numbering == nil {
		d.numbering = LoadNumbering(d.Pkg)
	}
	return d.numbering
}

// ListMarkers is the rendered list label of every numbered body paragraph
// as view v counts them.
func (d *Document) ListMarkers(v View) map[*tree.Node]string {
	if d.markers == nil {
		d.markers = map[View]map[*tree.Node]string{}
	}
	m, ok := d.markers[v]
	if !ok {
		m = d.Numbering().ListMarkers(d.Scope(), v)
		d.markers[v] = m
	}
	return m
}

// Lists is the list membership of every numbered body paragraph as view v
// counts them.
func (d *Document) Lists(v View) map[*tree.Node]ListInfo {
	if d.lists == nil {
		d.lists = map[View]map[*tree.Node]ListInfo{}
	}
	m, ok := d.lists[v]
	if !ok {
		m = d.Numbering().ListInfo(d.Scope(), v)
		d.lists[v] = m
	}
	return m
}

// IsStrict reports whether the main document is ISO Strict
// WordprocessingML.
func (d *Document) IsStrict() bool {
	r := d.XML.Root()
	return r != nil && r.Space() == NSStrictW
}
