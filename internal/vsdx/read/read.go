// Package read builds the vsdx read, find and xml results
// (docs/vsdx-contract-draft.md, sections 4 and 8) from the read model.
// Results are structs marshalled by cli.Marshal; lists are never nil.
package read

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Options are the read options of the command line.
type Options struct {
	// Units is "pt" (default) or "in".
	Units string
	// Detail is "", "sites" or "full".
	Detail string
	// IDs keeps only these objects and their descendants.
	IDs []string
	// Region keeps objects whose box intersects x, y, w, h (view units).
	Region *[4]float64
}

// Summary is read without --page or --master.
type Summary struct {
	Base    string        `json:"base"`
	Pages   []PageEntry   `json:"pages"`
	Masters []MasterEntry `json:"masters"`
	// ValuesStale is set when the document asks Visio to recalculate
	// on open (RecalcDocument): cached values may be out of date.
	ValuesStale bool   `json:"values_stale,omitempty"`
	Notes       []Note `json:"notes"`
}

// PageEntry is one page of the summary, in pages.xml order.
type PageEntry struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Background       bool          `json:"background,omitempty"`
	BackPage         string        `json:"background_page,omitempty"`
	Size             []json.Number `json:"size,omitempty"`
	Orientation      string        `json:"orientation,omitempty"`
	PrintOrientation string        `json:"print_orientation,omitempty"`
	Objects          int           `json:"objects"`
}

// MasterEntry is one master of the summary, in masters.xml order.
type MasterEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	NameU   string `json:"name_u,omitempty"`
	Hidden  bool   `json:"hidden,omitempty"`
	Objects int    `json:"objects"`
}

// Note is contract section 4's {at, reason}.
type Note struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// Document reads the summary of the package in input. Page object
// counts come from a streaming tree.Scan of each page (no tree kept).
// Chunk R1.
func Document(input []byte, base string) (*Summary, error) {
	pkg, err := opc.OpenBytes(input, opc.DefaultLimits())
	if err != nil {
		return nil, fmt.Errorf("vsdx: %w", err)
	}
	defer pkg.Close()
	d, err := doc.Open(pkg)
	if err != nil {
		return nil, err // doc.Open's errors already carry one "vsdx:" prefix
	}
	r := inherit.New(d)
	sum := &Summary{Base: base, Pages: []PageEntry{}, Masters: []MasterEntry{}, Notes: []Note{}}
	sum.ValuesStale = d.Recalc
	for _, n := range d.Notes {
		sum.Notes = append(sum.Notes, Note{At: n.At, Reason: n.Reason})
	}
	for i := range d.Pages {
		e := &d.Pages[i]
		p := model.PageSheet(d, r, e)
		entry := PageEntry{ID: p.ID, Name: p.Name, Background: p.Background, BackPage: p.BackPage, Orientation: p.Orientation, PrintOrientation: p.PrintOrientation}
		if p.HasSize {
			entry.Size = []json.Number{json.Number(geom.FormatNum(p.W * vsd.PointsPerInch)), json.Number(geom.FormatNum(p.H * vsd.PointsPerInch))}
		}
		if e.Part != "" {
			n, err := countObjects(pkg, e.Part)
			if err != nil {
				sum.Notes = append(sum.Notes, Note{At: p.ID, Reason: "unreadable: " + err.Error()})
			} else {
				entry.Objects = n
			}
		}
		for _, n := range p.Notes {
			sum.Notes = append(sum.Notes, Note{At: n.At, Reason: n.Reason})
		}
		sum.Pages = append(sum.Pages, entry)
	}
	for i := range d.Masters {
		m := &d.Masters[i]
		entry := MasterEntry{ID: vsd.MasterID(m.ID), Name: m.Name, Hidden: m.Hidden}
		if m.NameU != "" && m.NameU != m.Name {
			entry.NameU = m.NameU
		}
		if m.Part != "" {
			n, err := countObjects(pkg, m.Part)
			if err != nil {
				sum.Notes = append(sum.Notes, Note{At: entry.ID, Reason: "unreadable: " + err.Error()})
			} else {
				entry.Objects = n
			}
		}
		sum.Masters = append(sum.Masters, entry)
	}
	for _, n := range r.Notes() {
		sum.Notes = append(sum.Notes, Note{At: n.At, Reason: n.Reason})
	}
	return sum, nil
}

// wrapPart names part in err's context without adding a second
// "vsdx:" when err already carries one: the part context alone is
// wrapped as "<part>: <err>" then. Chunk MF6.
func wrapPart(part string, err error) error {
	if strings.HasPrefix(err.Error(), "vsdx:") {
		return fmt.Errorf("%s: %w", part, err)
	}
	return fmt.Errorf("vsdx: %s: %w", part, err)
}

// countObjects counts the Shape elements of the part at part in pkg
// with a streaming tree.ScanString, dropping the part's cached bytes
// after (Part.Forget) so one page is held at a time. Chunk R1.
func countObjects(pkg *opc.Package, part string) (int, error) {
	pt, err := pkg.Part(part)
	if err != nil {
		return 0, wrapPart(part, err)
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		return 0, wrapPart(part, err)
	}
	if !ok {
		return 0, fmt.Errorf("vsdx: %s has no original bytes", part)
	}
	c := &shapeCounter{}
	err = tree.ScanString(s, c)
	pt.Forget()
	if err != nil {
		return 0, wrapPart(part, err)
	}
	return c.count, nil
}

// shapeCounter counts Shape elements in a tree.Scan visitor, resolving
// namespaces through a tree.NSStack (Scan reports nodes without their
// ancestors' declarations). Chunk R1.
type shapeCounter struct {
	ns tree.NSStack
	// count is the Shape elements in a Visio main namespace seen so far.
	count int
}

// Start records the namespace declarations of element n and counts it
// when it is a Shape in a Visio main namespace.
func (c *shapeCounter) Start(n *tree.Node) error {
	c.ns.Push(n)
	if n.Local() == vsd.ElemShape && vsd.IsMain(c.ns.Space(n)) {
		c.count++
	}
	return nil
}

// End drops the namespace declarations of the innermost element.
func (c *shapeCounter) End(*tree.Node) error {
	c.ns.Pop()
	return nil
}

// Leaf ignores the non-element node n.
func (c *shapeCounter) Leaf(*tree.Node) error { return nil }
